package main

// UND-04 wiring slice 1 (rulings A320, A329, A330, A333, A347, A356; REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-AUD-001, REQ-SEC-004, REQ-OPS-003; INV-02, INV-12, INV-14,
// INV-17, INV-29; AGENT-13, AGENT-16): the composition root builds the Understanding half. It loads the pack through its manifest (a pack file changed after the manifest was made
// stops the start; an id other than the manifest's is refused), builds the gate, the required slots, the rule set and the evidence mapping from the verified bytes, and builds one
// Interpreter per tenant over the tenant's own orchestrator login, with the compile client over mutual TLS. Every setting is required and has no default. The faults are real: a
// byte changed in a real pack file, a model service that never answers, a table that is really locked, a tenant with no login. Certificates, keys and tokens are SYNTHETIC.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

const undManifestRel = "fintech/manifest.v1.json"

var undPackFiles = []string{"fintech_v1.json", undManifestRel, "fintech/evidence_mapping/mapping.v1.json", "fintech/recon_rules/rules.v1.json",
	"fintech/graph/g1_investigate.json", "fintech/graph/g2_escalate.json", "fintech/graph/g3_complaint.json",
	"fintech/limits/execution_limits.json", "fintech/limits/masking_limits.json", "fintech/limits/session_limits.json", "fintech/limits/trust_limits.json",
	"fintech/context/injection_patterns.v1.json"} // A510: the three action graphs, (A544) the four limits files and (A645) the injection patterns are pinned by the manifest too

// copyPack copies the real pack and its manifest into a fresh directory, so that a test can break one file of it.
func copyPack(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	for _, rel := range undPackFiles {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// reseal rewrites every digest of the manifest from the files as they are now, as CI's step does after an edit that the manifest is meant to cover.
func reseal(t *testing.T, dir string) {
	t.Helper()
	m := readJSON(t, filepath.Join(dir, undManifestRel))
	for _, f := range m["files"].([]any) {
		e := f.(map[string]any)
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(e["path"].(string))))
		if err != nil {
			t.Fatal(err)
		}
		s := sha256.Sum256(b)
		e["sha256"] = hex.EncodeToString(s[:])
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, undManifestRel), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, tail string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(tail); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// modelStub is a stand-in for the model service's listener: it demands a client certificate from the CA and records what it was asked.
type modelStub struct {
	mu        sync.Mutex
	calls     int
	subject   string
	lastIn    map[string]any
	packRef   string
	cancelled bool                                         // the last call was abandoned by the client before the stub answered
	respond   func(ci map[string]any, n int) (int, []byte) // nil: a valid intent for the canonical input
	hold      bool                                         // answer nothing until the client goes away
	srv       *httptest.Server
	files     mtlsFiles
}

func newModelStub(t *testing.T) *modelStub {
	t.Helper()
	ca := newTestCA(t, "noema-poc-ca")
	certPEM, keyPEM := ca.issueAt(t, "model", true, "127.0.0.1")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	m := &modelStub{}
	m.srv = httptest.NewUnstartedServer(http.HandlerFunc(m.serve))
	m.srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12}
	m.srv.StartTLS()
	t.Cleanup(m.srv.Close)
	cc, ck := ca.issue(t, "core-understanding", false)
	m.files = writeMTLS(t, cc, ck, ca.pem)
	return m
}

func (m *modelStub) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		CanonicalInput map[string]any `json:"canonical_input"`
		PackRef        string         `json:"pack_ref"`
	}
	_ = json.Unmarshal(body, &req)
	m.mu.Lock()
	m.calls++
	n := m.calls
	m.subject, m.lastIn, m.packRef, m.cancelled = r.TLS.PeerCertificates[0].Subject.CommonName, req.CanonicalInput, req.PackRef, false
	hold, respond := m.hold, m.respond
	m.mu.Unlock()
	if hold {
		select {
		case <-r.Context().Done():
			m.mu.Lock()
			m.cancelled = true
			m.mu.Unlock()
		case <-time.After(5 * time.Second): // the client never went away: the test fails on `cancelled`
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	status, out := http.StatusOK, []byte(nil)
	if respond != nil {
		status, out = respond(req.CanonicalInput, n)
	} else {
		out = compiled(req.CanonicalInput, intentFor(req.CanonicalInput, n, nil))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

func (m *modelStub) seen() (calls int, subject, packRef string, in map[string]any, cancelled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls, m.subject, m.packRef, m.lastIn, m.cancelled
}

func compiled(ci map[string]any, intent []byte) []byte {
	cib, _ := json.Marshal(ci)
	out, _ := json.Marshal(map[string]json.RawMessage{"intent_object": intent, "canonical_input": cib})
	return out
}

// intentFor is what the model service proposes for a canonical input: the identity members are the model's guess (the gate overwrites them from the session), and the
// markers are where the model has nothing to say.
func intentFor(ci map[string]any, n int, mutate func(m map[string]any)) []byte {
	m := map[string]any{
		"schema_version": "3.0", "intent_id": fmt.Sprintf("int_01J9X3%04d", n), "version": 1, "supersedes": nil, "tenant_id": ci["tenant_id"], "correlation_id": ci["correlation_id"],
		"created_at":  "2026-10-01T09:47:03Z",
		"principal":   map[string]any{"principal_id": ci["principal_ref"], "principal_type": "unset", "authn_ref": ci["session_id"], "asserted_by": "unset"},
		"source":      map[string]any{"mode": "MODE_1", "channel": "text", "locale_detected": []any{"en-NG"}, "asr_confidence": nil, "raw_ref": "raw_01J9X3"},
		"pack":        map[string]any{"pack_id": "fin", "pack_version": "1.0.0"},
		"intent_node": "fin.payments.exception.investigate",
		"slots": map[string]any{
			"subject_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX88213"},
			"question_type":    map[string]any{"type": "Reference", "value": "STATUS"},
		},
		"ambiguity_flags": []any{}, "desired_outcome_refs": []any{"fin.outcome.exception.status_explained"}, "status": "VALIDATED",
		"sig": map[string]any{"key_id": "unsigned", "value": "unsigned"},
	}
	if mutate != nil {
		mutate(m)
	}
	b, _ := json.Marshal(m)
	return b
}

// undSettings is every setting of the half, with the bench values of A356 for the three bounds, over a pack directory and a model service.
func undSettings(packs string, m *modelStub) envMap {
	return envMap{
		EnvPackDir: packs, EnvPackManifest: undManifestRel, EnvPackMaxBytes: "1048576", EnvPackMaxDepth: "32",
		EnvStoreTimeoutMS: "1000", EnvCompileTimeoutMS: "3000", EnvSealTimeoutMS: "1000",
		EnvModelURL: m.srv.URL, EnvModelClientCertFile: m.files.cert, EnvModelClientKeyFile: m.files.key, EnvModelCAFile: m.files.ca, EnvModelMaxResponseBytes: "262144",
	}
}

func merged(base envMap, more envMap) envMap {
	out := envMap{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}

// undFakeRecorder is the orchestrator's Recorder over lazily connecting pools nothing dials: enough for the tests that do not touch a database.
func undFakeRecorder(t *testing.T, tenants ...string) *orchestratorRecorder {
	t.Helper()
	rec, closer, err := build(t, orchEnv(newOrchDirs(t, tenants...), "2"), fakeDBs{})
	if err != nil || rec == nil {
		t.Fatalf("recorder %v, %v", rec, err)
	}
	t.Cleanup(closer)
	return rec
}

// undTurnAdmissions confirms the one INPUT_RECEIVED these tests name, for the session that asks, with the minted input id it carries (A184, A219).
type undTurnAdmissions struct{}

func (undTurnAdmissions) Lookup(_ context.Context, s orchestrator.Session, _ string) (orchestrator.Admission, error) {
	return orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: s.TenantID, PrincipalID: s.PrincipalID, CorrelationID: undCorr, RequestID: undRequestID, ObjectRef: "req_01ARZ3NDEKTSV4RRFFQ69G5FAV"}, nil
}

// undAdmissions answers no lookup: the builder tests never run a turn.
type undAdmissions struct{}

func (undAdmissions) Lookup(context.Context, orchestrator.Session, string) (orchestrator.Admission, error) {
	return orchestrator.Admission{}, errors.New("no admission")
}

func buildUnd(t *testing.T, e envMap, rec *orchestratorRecorder) (UnderstandingParts, error) {
	t.Helper()
	return buildUnderstanding(Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, clock.NewManual(apiEpoch), rec, undAdmissions{})
}

func wantNone(t *testing.T, what string, p UnderstandingParts) {
	t.Helper()
	if p.Interpreter != nil || p.RuleSet != nil || p.Mapping != nil || p.PackRef != "" || p.SealTimeout != 0 {
		t.Errorf("%s: a refused build returned parts: %+v", what, p)
	}
}

// AGENT-16: no setting has a default. A missing one stops the start with an error that names it; nothing is built.
func TestUnderstandingNeedsEverySettingAndHasNoDefaults(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	all := undSettings(copyPack(t), m)
	if _, err := buildUnd(t, all, rec); err != nil {
		t.Fatalf("the complete settings do not build: %v", err)
	}
	for name := range all {
		e := merged(all, nil)
		delete(e, name)
		p, err := buildUnd(t, e, rec)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: got %v; want an error that names it", name, err)
		}
		wantNone(t, "without "+name, p)
	}
}

// A bound is a whole positive number, ASCII, with no sign, space, fraction, exponent, hex form or leading zero; the error names the setting and never repeats the value.
func TestABoundThatIsNotAWholePositiveNumberIsRefused(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	base := undSettings(copyPack(t), m)
	bad := []string{"", "0", "-1", "01", " 5", "5 ", "5\n", "1.5", "1e3", "+5", "0x10", "7z9", "٣", "9223372036854775808", "99999999999999999999"}
	for _, name := range []string{EnvPackMaxBytes, EnvPackMaxDepth, EnvStoreTimeoutMS, EnvCompileTimeoutMS, EnvSealTimeoutMS} {
		for _, v := range bad {
			p, err := buildUnd(t, merged(base, envMap{name: v}), rec)
			if err == nil || !strings.Contains(err.Error(), name) || (v != "" && strings.Contains(err.Error(), v)) {
				t.Errorf("%s=%q: got %v; want an error that names the setting and not the value", name, v, err)
			}
			wantNone(t, name+"="+v, p)
		}
	}
	// A duration that does not fit is refused too, in each of the three bounds in milliseconds.
	for _, name := range []string{EnvStoreTimeoutMS, EnvCompileTimeoutMS, EnvSealTimeoutMS} {
		if _, err := buildUnd(t, merged(base, envMap{name: "9223372036854775807"}), rec); err == nil {
			t.Errorf("%s: a duration over the representable range was accepted", name)
		}
	}
}

// The Interpreter stores through the tenant's own login: without the orchestrator's Recorder there is nothing to store with, and nothing is built.
func TestWithoutTheRecorderNothingIsBuilt(t *testing.T) {
	m := newModelStub(t)
	p, err := buildUnd(t, undSettings(copyPack(t), m), nil)
	if err == nil || !strings.Contains(err.Error(), EnvOrchestratorDSNDir) {
		t.Errorf("got %v; want an error that names the setting the Recorder is built from", err)
	}
	wantNone(t, "no recorder", p)
}

// The compile client is the one the model settings build, over mutual TLS: none of them is an error here, because without a model service there is no Interpreter.
func TestWithoutTheModelSettingsNothingIsBuilt(t *testing.T) {
	m := newModelStub(t)
	e := undSettings(copyPack(t), m)
	for _, name := range modelSettings {
		delete(e, name)
	}
	p, err := buildUnd(t, e, undFakeRecorder(t, "ten_aa"))
	if err == nil || !strings.Contains(err.Error(), EnvModelURL) {
		t.Errorf("got %v; want an error that names the model service settings", err)
	}
	wantNone(t, "no model", p)
}

// A510: the real pack's action graphs are the ones its manifest pins, verified, by the paths the pack file names; nothing else is served.
func TestTheRealPackLoadsItsActionGraphsVerifiedThroughItsManifest(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packs")
	p, err := packload.Load(root, undManifestRel, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	named, _ := readJSON(t, filepath.Join(root, "fintech_v1.json"))["action_graph_files"].([]any)
	if len(named) == 0 || len(p.ActionGraphs) != len(named) {
		t.Fatalf("%d graphs served; the pack names %d", len(p.ActionGraphs), len(named))
	}
	for _, n := range named {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(n.(string))))
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := p.ActionGraphs[n.(string)]; !ok || string(got) != string(want) {
			t.Errorf("graph %v is not served with the bytes on disk", n)
		}
	}
}

// The real pack, through its real manifest: the gate, the required slots, the rule set and the mapping are built from the verified bytes, and the ids are the manifest's.
func TestTheRealPackLoadsThroughItsManifest(t *testing.T) {
	m := newModelStub(t)
	root := filepath.Join("..", "..", "..", "..", "packs")
	p, err := buildUnd(t, merged(undSettings(root, m), envMap{EnvStoreTimeoutMS: "700", EnvCompileTimeoutMS: "2900", EnvSealTimeoutMS: "1500"}), undFakeRecorder(t, "ten_aa", "ten_bb"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Interpreter == nil || p.RuleSet == nil || p.Mapping == nil {
		t.Fatalf("incomplete parts: %+v", p)
	}
	man := readJSON(t, filepath.Join(root, undManifestRel))["expected"].(map[string]any)
	if p.Mapping.ID() != man["evidence_mapping_id"] || p.RuleSet.ID() != man["recon_rule_set_id"] {
		t.Errorf("ids %q and %q; the manifest names %v", p.Mapping.ID(), p.RuleSet.ID(), man)
	}
	pk := readJSON(t, filepath.Join(root, "fintech_v1.json"))
	if want := fmt.Sprintf("%v@%v", pk["pack_id"], pk["pack_version"]); p.PackRef != want {
		t.Errorf("the pack reference is %q; the pack says %q", p.PackRef, want)
	}
	if p.SealTimeout != 1500*time.Millisecond {
		t.Errorf("the seal bound is %v; the setting says 1500 ms", p.SealTimeout)
	}
}

// A438 and the ruling on noema#773 ("pass WithScope plus fin.src.case_system"): the evidence mapping the start builds from the real pack reads the case system's lists. The source id comes from the
// pack's own evidence_sources (never a literal in core) and the case and complaint_ref entities are typed by the scope the pack's descriptors give. A start that omitted either would refuse the
// version 2 mapping, so this pins that the built Mapping answers for that source, and that the list is concluded from a page marked complete only (INV-19).
func TestTheMappingBuiltFromTheRealPackServesTheCaseSystemsLists(t *testing.T) {
	m := newModelStub(t)
	root := filepath.Join("..", "..", "..", "..", "packs")
	p, err := buildUnd(t, undSettings(root, m), undFakeRecorder(t, "ten_aa"))
	if err != nil {
		t.Fatal(err)
	}
	const caseSystem = "fin.src.case_system"
	pk := readJSON(t, filepath.Join(root, "fintech_v1.json"))
	found := false
	for _, s := range pk["evidence_sources"].([]any) {
		if s.(map[string]any)["source_id"] == caseSystem {
			found = true
		}
	}
	if !found {
		t.Fatalf("the real pack does not name %s as an evidence source", caseSystem)
	}
	for _, entity := range []string{"case", "complaint_ref"} {
		rows, err := p.Mapping.ListRows(caseSystem, entity, map[string]any{"items": []any{}, "complete": true})
		if err != nil || len(rows) != 0 {
			t.Errorf("%s from a complete empty page: %v rows, %v; want none and no error", entity, rows, err)
		}
		if _, err := p.Mapping.ListRows(caseSystem, entity, map[string]any{"items": []any{}}); !errors.Is(err, mapping.ErrIncomplete) {
			t.Errorf("%s from a page with no marker: %v; want ErrIncomplete (INV-19)", entity, err)
		}
	}
}

// A333 acceptance: a pack file changed after the manifest was made stops the start, for each of the three files, with an error that does not name the directory.
func TestAPackFileChangedAfterTheManifestStopsStartup(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	for _, rel := range []string{"fintech_v1.json", "fintech/evidence_mapping/mapping.v1.json", "fintech/recon_rules/rules.v1.json", "fintech/context/injection_patterns.v1.json"} {
		dir := copyPack(t)
		appendTo(t, filepath.Join(dir, filepath.FromSlash(rel)), "\n")
		p, err := buildUnd(t, undSettings(dir, m), rec)
		if !errors.Is(err, packload.ErrHash) {
			t.Errorf("%s changed: got %v; want the SHA-256 refusal", rel, err)
		} else if strings.Contains(err.Error(), dir) {
			t.Errorf("the error names the directory: %v", err)
		}
		wantNone(t, rel, p)
	}
}

// A333 acceptance: an id other than the manifest's is refused. The manifest is edited to expect another id, with every digest still right.
func TestAnIdOtherThanTheManifestsIsRefusedAtStartup(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	for name, edit := range map[string]func(exp map[string]any){
		"the mapping":  func(exp map[string]any) { exp["evidence_mapping_id"] = "fin.map.v1@3" },
		"the rule set": func(exp map[string]any) { exp["recon_rule_set_id"] = "fin.recon.rules@3" },
	} {
		dir := copyPack(t)
		man := readJSON(t, filepath.Join(dir, undManifestRel))
		edit(man["expected"].(map[string]any))
		out, _ := json.Marshal(man)
		if err := os.WriteFile(filepath.Join(dir, undManifestRel), out, 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := buildUnd(t, undSettings(dir, m), rec)
		if !errors.Is(err, packload.ErrID) {
			t.Errorf("%s: got %v; want the id refusal", name, err)
		}
		wantNone(t, name, p)
	}
}

// The other ways a pack is not what the manifest pins: a link in place of a file, a second mapping the manifest does not list, a path that leaves the directory, no manifest.
func TestAPackThatIsNotWhatTheManifestPinsStopsStartup(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	cases := map[string]struct {
		prepare func(dir string, e envMap)
		want    error
	}{
		"a symlinked pack file": {func(dir string, _ envMap) {
			real := filepath.Join(dir, "real_v1.json")
			must(t, os.Rename(filepath.Join(dir, "fintech_v1.json"), real))
			must(t, os.Symlink(real, filepath.Join(dir, "fintech_v1.json")))
		}, packload.ErrLink},
		"an unlisted second mapping": {func(dir string, _ envMap) {
			b, _ := os.ReadFile(filepath.Join(dir, "fintech/evidence_mapping/mapping.v1.json"))
			must(t, os.WriteFile(filepath.Join(dir, "fintech/evidence_mapping/mapping.v2.json"), b, 0o644))
		}, packload.ErrUnlisted},
		"a manifest path that escapes": {func(_ string, e envMap) { e[EnvPackManifest] = "../fintech/manifest.v1.json" }, packload.ErrPath},
		"no manifest":                  {func(dir string, _ envMap) { must(t, os.Remove(filepath.Join(dir, undManifestRel))) }, packload.ErrManifest},
		"no directory":                 {func(dir string, e envMap) { e[EnvPackDir] = filepath.Join(dir, "absent") }, packload.ErrConfig},
	}
	for name, c := range cases {
		dir := copyPack(t)
		e := undSettings(dir, m)
		c.prepare(dir, e)
		p, err := buildUnd(t, e, rec)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v; want %v", name, err, c.want)
		}
		wantNone(t, name, p)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// A pack whose digests are right (the manifest was remade) but whose content is not usable by its parser is refused too: the pack, the rule set and the mapping each have their own
// parser, and a parser's refusal stops the start.
func TestAVerifiedPackWhoseContentCannotBeParsedStopsStartup(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	for name, c := range map[string]struct{ rel, body string }{
		"the pack":     {"fintech_v1.json", `{"pack_id":"fin","pack_version":"1.0.0","intent_nodes":[]}`},
		"the rule set": {"fintech/recon_rules/rules.v1.json", `{"ruleset_id":"fin.recon.rules","version":1}`},
		"the mapping":  {"fintech/evidence_mapping/mapping.v1.json", `{"mapping_id":"fin.map.v1","version":1,"label":"SYNTHETIC","ruleset":"fin.recon.rules@1","rows":[]}`},
	} {
		dir := copyPack(t)
		must(t, os.WriteFile(filepath.Join(dir, filepath.FromSlash(c.rel)), []byte(c.body), 0o644))
		reseal(t, dir)
		p, err := buildUnd(t, undSettings(dir, m), rec)
		if err == nil {
			t.Errorf("%s: an unusable file was accepted", name)
		}
		wantNone(t, name, p)
	}
}

// A pack that names no evidence source cannot give the mapping its sources: refused.
func TestAPackWithNoEvidenceSourcesStopsStartup(t *testing.T) {
	m := newModelStub(t)
	dir := copyPack(t)
	pk := readJSON(t, filepath.Join(dir, "fintech_v1.json"))
	pk["evidence_sources"] = []any{}
	out, _ := json.Marshal(pk)
	must(t, os.WriteFile(filepath.Join(dir, "fintech_v1.json"), out, 0o644))
	reseal(t, dir)
	p, err := buildUnd(t, undSettings(dir, m), undFakeRecorder(t, "ten_aa"))
	if err == nil || !strings.Contains(err.Error(), "evidence sources") {
		t.Errorf("got %v; want a refusal of the evidence sources", err)
	}
	wantNone(t, "no sources", p)
}

// ---- the real stack: a real PostgreSQL, the real session middleware, a real mutual-TLS model service ----

const (
	undRequestID = "0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d"
	undCorr      = "corr_01ARZ3NDEKTSV4RRFFQ69G5FAA"
	undText      = "Why is my transfer still pending?"
)

var undSesRe = regexp.MustCompile(`^ses_[0-9a-f]{32}$`)

// undWorld is the Understanding half, built the way the process builds it, behind the edge's verified session.
type undWorld struct {
	*wireWorld
	iss    *edgetest.Issuer
	model  *modelStub
	parts  UnderstandingParts
	handle http.Handler
	probe  *undProbe
}

// undProbe is a route behind the real session middleware. Its handler builds the orchestrator's Session from the verified principal the way the edge does, calls the port with the
// request's context, and keeps what came back.
type undProbe struct {
	port   orchestrator.Interpreter
	mutate func(orchestrator.Session) orchestrator.Session
	in     orchestrator.Interpretation
	err    error
	ctxErr error
}

func newUndWorld(t *testing.T, more envMap) *undWorld {
	t.Helper()
	w := newWireWorld(t)
	// The orchestrator's signing key is a file the process mounts; the database knows its public half, as the deploy step registers it.
	seed, err := hex.DecodeString(aSeed)
	must(t, err)
	pub := []byte(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	var keyID string
	must(t, w.admin.QueryRow(deadline(t), `SELECT key_register('core-workflows', 1, $1)`, pub).Scan(&keyID))

	iss := edgetest.NewIssuer(t)
	m := newModelStub(t)
	e := merged(merged(w.settings(t), undSettings(copyPack(t), m)), more)
	for k, v := range sessionEnv(t, iss) {
		e[k] = v
	}
	e[edge.EnvTenants] = "ten_aa,ten_bb,ten_cc"
	env := osEnv(e)
	clk := clock.NewManual(apiEpoch)
	records, closeRecords, err := buildRecordReader(t.Context(), env)
	must(t, err)
	rec, closeRec, err := buildRecorder(t.Context(), env, clk, ids.New(clk, nil), records)
	must(t, err)
	t.Cleanup(func() { closeRec(); closeRecords() })
	parts, err := buildUnderstanding(env, clk, rec, undTurnAdmissions{})
	if err != nil {
		t.Fatal(err)
	}
	probe := &undProbe{port: parts.Interpreter}
	route := edge.Route{Pattern: "GET /v1/investigations/probe", Roles: []session.Role{session.RoleCustomer, session.RoleOps}, Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		p, _ := session.FromContext(r.Context())
		s := orchestrator.Session{TenantID: p.Tenant, PrincipalID: p.ID, Role: string(p.Role)}
		if probe.mutate != nil {
			s = probe.mutate(s)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		probe.in, probe.err = probe.port.Interpret(ctx, s, orchestrator.Turn{InvestigationID: "inv_01ARZ3NDEKTSV4RRFFQ69G5FAA", CorrelationID: undCorr, Text: undText, RequestID: undRequestID, InputReceivedRef: "recev_01ARZ3NDEKTSV4RRFFQ69G5FAV"})
		probe.ctxErr = ctx.Err()
		rw.WriteHeader(http.StatusNoContent)
	})}
	h, err := edge.Load(e.get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{route})
	if err != nil || h == nil {
		t.Fatalf("edge %v, %v", h, err)
	}
	return &undWorld{wireWorld: w, iss: iss, model: m, parts: parts, handle: h, probe: probe}
}

// as runs one request as the bearer and returns the status; the port's answer is in the probe.
func (u *undWorld) as(t *testing.T, role session.Role, tenant, sub string) int {
	t.Helper()
	u.probe.in, u.probe.err, u.probe.ctxErr = orchestrator.Interpretation{}, nil, nil
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/investigations/probe", nil)
	req.Header.Set("Authorization", bearerAs(u.iss, role, tenant, sub))
	u.handle.ServeHTTP(rec, req)
	return rec.Code
}

func (u *undWorld) n(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	must(t, u.admin.QueryRow(deadline(t), query, args...).Scan(&n))
	return n
}

func (u *undWorld) intents(t *testing.T, tenant string) int {
	return u.n(t, `SELECT count(*) FROM admitted_intent WHERE tenant_id = $1`, tenant)
}

func (u *undWorld) events(t *testing.T, tenant, eventType string) int {
	return u.n(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND event_type = $2`, tenant, eventType)
}

// authnRefOf is A149's reference of a session: `ses_` and the first 32 hex characters of SHA-256 over the JSON array [kid, sub, iat]. It is recomputed here, from the token's own
// claims, so that the test does not trust the code under test for it.
func authnRefOf(kid, sub string, iat int64) string {
	raw, _ := json.Marshal([]any{kid, sub, iat})
	s := sha256.Sum256(raw)
	return "ses_" + hex.EncodeToString(s[:])[:32]
}

// UND-04, REQ-INT-001, REQ-INP-002, REQ-AUD-001, INV-12, A149: through the verified session, a customer's turn is compiled over mutual TLS, admitted by the gate from the verified pack,
// and saved with its INTENT_ADMITTED event by the customer's own tenant's login. The canonical input carries the session's reference (a hash, never the token) and the principal of the
// session; the Record's actor is the session's principal.
func TestATurnIsCompiledAdmittedAndSavedAsTheSessionsTenant(t *testing.T) {
	u := newUndWorld(t, nil)
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		if code := u.as(t, session.RoleCustomer, tenant, "usr_88213"); code != http.StatusNoContent || u.probe.err != nil {
			t.Fatalf("%s: %d, %v", tenant, code, u.probe.err)
		}
		if u.probe.in.Kind != orchestrator.Admitted || !regexp.MustCompile(`^int_[0-9A-Za-z]+@1$`).MatchString(u.probe.in.IntentRef) {
			t.Errorf("%s: %+v; want an admitted intent with a reference", tenant, u.probe.in)
		}
		if got := u.intents(t, tenant); got != 1 {
			t.Errorf("%s: %d admitted intents; want 1", tenant, got)
		}
		if got := u.events(t, tenant, "INTENT_ADMITTED"); got != 1 {
			t.Errorf("%s: %d INTENT_ADMITTED events; want 1", tenant, got)
		}
		if got := u.n(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND event_type = 'INTENT_ADMITTED' AND actor = 'customer:usr_88213' AND written_by = 'core-workflows'`, tenant); got != 1 {
			t.Errorf("%s: the event is not written by core-workflows for customer:usr_88213", tenant)
		}
	}
	calls, subject, packRef, ci, _ := u.model.seen()
	if calls != 2 || subject != "core-understanding" || packRef != u.parts.PackRef {
		t.Errorf("the model service saw %d calls from %q for %q", calls, subject, packRef)
	}
	want := authnRefOf(apiKid, "usr_88213", apiEpoch.Unix())
	if ci["session_id"] != want || !undSesRe.MatchString(want) || ci["principal_ref"] != "usr_88213" || ci["tenant_id"] != "ten_bb" || ci["text"] != undText {
		t.Errorf("the canonical input %v; want the session's reference %s, its principal and its tenant", ci, want)
	}
	if b, _ := json.Marshal(ci); strings.Contains(string(b), "Bearer") || strings.Contains(string(b), "eyJ") {
		t.Errorf("the canonical input carries a token: %s", b)
	}
}

// A refusal at the gate is recorded and is a gate failure, not an error; nothing is saved (REQ-INT-001, REQ-AUD-001).
func TestAProposalTheGateRefusesIsRecordedAndNotSaved(t *testing.T) {
	u := newUndWorld(t, nil)
	u.model.respond = func(ci map[string]any, n int) (int, []byte) {
		return http.StatusOK, compiled(ci, intentFor(ci, n, func(m map[string]any) { m["intent_node"] = "fin.not.in.the.pack" }))
	}
	if code := u.as(t, session.RoleCustomer, "ten_aa", "usr_88213"); code != http.StatusNoContent || u.probe.err != nil || u.probe.in.Kind != orchestrator.GateFailed {
		t.Fatalf("%d, %+v, %v; want a gate failure and no error", code, u.probe.in, u.probe.err)
	}
	if got := u.events(t, "ten_aa", "INTENT_REJECTED_AT_GATE"); got != 1 {
		t.Errorf("%d INTENT_REJECTED_AT_GATE events; want 1", got)
	}
	if u.intents(t, "ten_aa") != 0 || u.events(t, "ten_aa", "INTENT_ADMITTED") != 0 {
		t.Error("a refused proposal was saved")
	}
}

// REQ-INP-002: a proposal that names another principal is saved as the session's, and the attempt is recorded as a security event with the field names only.
func TestAProposalThatNamesAnotherPrincipalIsSavedAsTheSessions(t *testing.T) {
	u := newUndWorld(t, nil)
	u.model.respond = func(ci map[string]any, n int) (int, []byte) {
		return http.StatusOK, compiled(ci, intentFor(ci, n, func(m map[string]any) { m["principal"].(map[string]any)["principal_id"] = "usr_other" }))
	}
	if code := u.as(t, session.RoleCustomer, "ten_aa", "usr_88213"); code != http.StatusNoContent || u.probe.err != nil || u.probe.in.Kind != orchestrator.Admitted {
		t.Fatalf("%d, %+v, %v", code, u.probe.in, u.probe.err)
	}
	if got := u.events(t, "ten_aa", "SECURITY_EVENT"); got != 1 {
		t.Errorf("%d SECURITY_EVENT events; want 1", got)
	}
	if got := u.n(t, `SELECT count(*) FROM admitted_intent WHERE tenant_id = 'ten_aa' AND json->'principal'->>'principal_id' = 'usr_88213'`); got != 1 {
		t.Error("the saved intent is not the session's principal's")
	}
}

// INV-12, AGENT-10: the identity is the verified session's and nothing else. With no session in the context nothing is called; with an orchestrator Session that is not the verified
// one (another tenant, another principal, another role) nothing is called either. The refusal is one fixed error that names no tenant, principal or role.
func TestWithoutTheVerifiedSessionNothingIsCompiled(t *testing.T) {
	u := newUndWorld(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	_, err := u.parts.Interpreter.Interpret(ctx, orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_88213", Role: "customer"},
		orchestrator.Turn{InvestigationID: "inv_01ARZ3NDEKTSV4RRFFQ69G5FAA", CorrelationID: undCorr, Text: undText, RequestID: undRequestID})
	if !errors.Is(err, ErrSessionUnavailable) {
		t.Errorf("no session in the context: got %v; want ErrSessionUnavailable", err)
	}
	for name, mutate := range map[string]func(orchestrator.Session) orchestrator.Session{
		"another tenant":    func(s orchestrator.Session) orchestrator.Session { s.TenantID = "ten_bb"; return s },
		"another principal": func(s orchestrator.Session) orchestrator.Session { s.PrincipalID = "usr_other"; return s },
		"another role":      func(s orchestrator.Session) orchestrator.Session { s.Role = "ops"; return s },
		"no role":           func(s orchestrator.Session) orchestrator.Session { s.Role = ""; return s },
	} {
		u.probe.mutate = mutate
		u.as(t, session.RoleCustomer, "ten_aa", "usr_88213")
		if !errors.Is(u.probe.err, ErrSessionUnavailable) {
			t.Errorf("%s: got %v; want ErrSessionUnavailable", name, u.probe.err)
		} else if msg := u.probe.err.Error(); strings.Contains(msg, "ten_") || strings.Contains(msg, "usr_") {
			t.Errorf("%s: the error names a tenant or a principal: %s", name, msg)
		}
	}
	if calls, _, _, _, _ := u.model.seen(); calls != 0 {
		t.Errorf("the model service was called %d times with no verified session", calls)
	}
	if u.intents(t, "ten_aa")+u.intents(t, "ten_bb") != 0 {
		t.Error("an intent was saved")
	}
}

// A329: one instance per tenant, from the DSN directory; a tenant with no login is refused, with no fallback to another tenant's, no call to the model service and no tenant id in the
// error (A347).
func TestAnUnknownTenantIsRefusedWithNoFallback(t *testing.T) {
	u := newUndWorld(t, nil) // the verified tenants include ten_cc; the orchestrator's directory has ten_aa and ten_bb only
	if code := u.as(t, session.RoleCustomer, "ten_cc", "usr_88213"); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	if !errors.Is(u.probe.err, ErrNoInterpreterForTenant) || strings.Contains(u.probe.err.Error(), "ten_cc") {
		t.Errorf("got %v; want ErrNoInterpreterForTenant and no tenant id", u.probe.err)
	}
	if calls, _, _, _, _ := u.model.seen(); calls != 0 {
		t.Errorf("the model service was called %d times for an unknown tenant", calls)
	}
	if u.intents(t, "ten_aa")+u.intents(t, "ten_bb") != 0 {
		t.Error("an intent was saved for a tenant that has no login")
	}
}

// NOEMA_COMPILE_TIMEOUT_MS bounds the compile call: a model service that never answers is abandoned by the client at the bound, and the failure is the compile client's
// UNAVAILABLE. The request's own deadline (twenty seconds in this route) is far later, so only the setting can have ended the call.
func TestAModelServiceThatNeverAnswersIsAbandonedAtTheCompileBound(t *testing.T) {
	u := newUndWorld(t, envMap{EnvCompileTimeoutMS: "150", EnvStoreTimeoutMS: "30000"})
	u.model.hold = true
	u.as(t, session.RoleCustomer, "ten_aa", "usr_88213")
	var cf *interpreter.CompileFailure
	if !errors.As(u.probe.err, &cf) || cf.Failure != understanding.FailureUnavailable {
		t.Fatalf("got %v; want a compile failure UNAVAILABLE", u.probe.err)
	}
	if u.probe.ctxErr != nil {
		t.Errorf("the request's own deadline ended the call, not the compile bound: %v", u.probe.ctxErr)
	}
	abandoned := false
	for i := 0; i < 300 && !abandoned; i++ { // the service notices the closed connection a moment after the client has gone
		_, _, _, _, abandoned = u.model.seen()
		if !abandoned {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !abandoned {
		t.Error("the model service was not abandoned by the client")
	}
	if u.intents(t, "ten_aa") != 0 {
		t.Error("an intent was saved")
	}
}

// NOEMA_STORE_TIMEOUT_MS bounds the save: with the table really locked by another transaction the save waits and gives up at the bound, and neither the event nor the row stands
// (EC-E16). The request's own deadline is far later, so only the setting can have ended the call.
func TestASaveThatCannotProceedGivesUpAtTheStoreBound(t *testing.T) {
	u := newUndWorld(t, envMap{EnvStoreTimeoutMS: "200", EnvCompileTimeoutMS: "30000"})
	lock, err := u.admin.Begin(deadline(t))
	must(t, err)
	_, err = lock.Exec(deadline(t), `LOCK TABLE admitted_intent IN ACCESS EXCLUSIVE MODE`)
	must(t, err)
	u.as(t, session.RoleCustomer, "ten_aa", "usr_88213")
	must(t, lock.Rollback(deadline(t)))
	if !errors.Is(u.probe.err, interpreter.ErrSave) {
		t.Fatalf("got %v; want ErrSave", u.probe.err)
	}
	if u.probe.ctxErr != nil {
		t.Errorf("the request's own deadline ended the call, not the store bound: %v", u.probe.ctxErr)
	}
	if u.intents(t, "ten_aa") != 0 || u.events(t, "ten_aa", "INTENT_ADMITTED") != 0 {
		t.Error("a save that timed out left an event or a row")
	}
}

// A model service that answers with a server error is a compile failure, never an admitted intent, and nothing is saved.
func TestAModelServiceErrorIsACompileFailureAndNothingIsSaved(t *testing.T) {
	u := newUndWorld(t, nil)
	u.model.respond = func(map[string]any, int) (int, []byte) { return http.StatusInternalServerError, []byte(`{}`) }
	u.as(t, session.RoleCustomer, "ten_aa", "usr_88213")
	var cf *interpreter.CompileFailure
	if !errors.As(u.probe.err, &cf) || cf.Failure != understanding.FailureUnavailable {
		t.Fatalf("got %v; want a compile failure UNAVAILABLE", u.probe.err)
	}
	if u.intents(t, "ten_aa") != 0 {
		t.Error("an intent was saved")
	}
}

// The core trusts only the CA file it is given: a model service whose certificate another CA signed is never asked, and the turn fails as unavailable.
func TestAModelServiceFromAnotherCAIsNeverCalled(t *testing.T) {
	other := newModelStub(t) // its own CA: the core's CA file (the world's own stub's) does not sign it
	u2 := newUndWorld(t, envMap{EnvModelURL: other.srv.URL})
	u2.as(t, session.RoleCustomer, "ten_aa", "usr_88213")
	var cf *interpreter.CompileFailure
	if !errors.As(u2.probe.err, &cf) || cf.Failure != understanding.FailureUnavailable {
		t.Fatalf("got %v; want a compile failure UNAVAILABLE", u2.probe.err)
	}
	if calls, _, _, _, _ := other.seen(); calls != 0 {
		t.Errorf("the other service was called %d times", calls)
	}
}

// Nothing is built without a recorder that has a login, and nothing without a clock: the Interpreter would have no store to save to, or no time to stamp the canonical input with.
func TestAnEmptyRecorderOrNoClockBuildsNothing(t *testing.T) {
	m := newModelStub(t)
	env := Env{Getenv: undSettings(copyPack(t), m).get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	p, err := buildUnderstanding(env, clock.NewManual(apiEpoch), &orchestratorRecorder{stores: map[string]*investigations.Store{}}, undAdmissions{})
	if err == nil {
		t.Error("a recorder with no tenant login was accepted")
	}
	wantNone(t, "an empty recorder", p)
	p, err = buildUnderstanding(env, nil, undFakeRecorder(t, "ten_aa"), undAdmissions{})
	if err == nil || !strings.Contains(err.Error(), "clock") {
		t.Errorf("no clock: got %v; want a refusal that names the clock", err)
	}
	wantNone(t, "no clock", p)
}

// A recorder whose pool is not a database pool of a tenant's login cannot give the intent store its pool: refused, with a fixed text.
func TestAStoreWithNoPoolOfItsOwnIsRefused(t *testing.T) {
	m := newModelStub(t)
	rec := &orchestratorRecorder{stores: map[string]*investigations.Store{"ten_aa": {Pool: noPool{}}}}
	p, err := buildUnd(t, undSettings(copyPack(t), m), rec)
	if err == nil || strings.Contains(err.Error(), "ten_aa") {
		t.Errorf("got %v; want a refusal that names no tenant", err)
	}
	wantNone(t, "a store with no pool", p)
}

type noPool struct{}

func (noPool) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("synthetic: no pool")
}

// The bounds are applied where they are named: a number that is valid but over what a 32-bit integer holds is refused for the pack bounds and for the model response bound (which is the
// gate's bound, A333); a pack file bound too small for the pack stops the load, and a nesting bound too small for the mapping stops the parser. The error texts are fixed.
func TestTheBoundsAreAppliedWhereTheyAreNamed(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	base := undSettings(copyPack(t), m)
	for _, name := range []string{EnvPackMaxBytes, EnvPackMaxDepth, EnvModelMaxResponseBytes} {
		if _, err := buildUnd(t, merged(base, envMap{name: "2147483648"}), rec); err == nil {
			t.Errorf("%s: a value over 32 bits was accepted", name)
		}
	}
	if _, err := buildUnd(t, merged(base, envMap{EnvPackMaxBytes: "1000"}), rec); !errors.Is(err, packload.ErrManifest) {
		t.Errorf("a pack bound under the manifest's size: got %v; want the manifest refusal", err)
	}
	if _, err := buildUnd(t, merged(base, envMap{EnvPackMaxDepth: "1"}), rec); err == nil {
		t.Error("a nesting bound of 1 was accepted for a mapping that nests deeper")
	}
	for name, want := range map[string]string{
		EnvPackMaxBytes:     "cmd/core: NOEMA_PACK_MAX_BYTES must be a whole positive number of bytes",
		EnvPackMaxDepth:     "cmd/core: NOEMA_PACK_MAX_DEPTH must be a whole positive number",
		EnvStoreTimeoutMS:   "cmd/core: NOEMA_STORE_TIMEOUT_MS must be a whole positive number of milliseconds",
		EnvCompileTimeoutMS: "cmd/core: NOEMA_COMPILE_TIMEOUT_MS must be a whole positive number of milliseconds",
		EnvSealTimeoutMS:    "cmd/core: NOEMA_SEAL_TIMEOUT_MS must be a whole positive number of milliseconds",
	} {
		if _, err := buildUnd(t, merged(base, envMap{name: "7z9"}), rec); err == nil || err.Error() != want {
			t.Errorf("%s: got %v; want %q", name, err, want)
		}
	}
	if _, err := buildUnd(t, merged(base, envMap{EnvStoreTimeoutMS: "9223372036854775807"}), rec); err == nil || err.Error() != "cmd/core: NOEMA_STORE_TIMEOUT_MS must be a whole positive number of milliseconds" {
		t.Errorf("a duration over the range: got %v", err)
	}
}

// The evidence sources come from the verified pack, each with an id and none twice.
func TestAPackWhoseEvidenceSourcesAreNotSoundStopsStartup(t *testing.T) {
	m := newModelStub(t)
	rec := undFakeRecorder(t, "ten_aa")
	for name, edit := range map[string]func(src []any) []any{
		"a repeated source": func(src []any) []any { return append(src, src[0]) },
		"a source with no id": func(src []any) []any {
			bad := map[string]any{}
			for k, v := range src[0].(map[string]any) {
				bad[k] = v
			}
			bad["source_id"] = ""
			return append(src, bad)
		},
	} {
		dir := copyPack(t)
		pk := readJSON(t, filepath.Join(dir, "fintech_v1.json"))
		pk["evidence_sources"] = edit(pk["evidence_sources"].([]any))
		out, _ := json.Marshal(pk)
		must(t, os.WriteFile(filepath.Join(dir, "fintech_v1.json"), out, 0o644))
		reseal(t, dir)
		p, err := buildUnd(t, undSettings(dir, m), rec)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		wantNone(t, name, p)
	}
}

// A gate refusal is recorded through the same login, within NOEMA_STORE_TIMEOUT_MS: with the Record's table really locked the append waits and gives up at the bound, and the turn
// fails as a Record failure, never as a gate refusal that was not recorded (the Record comes first, EC-E16).
func TestARefusalThatCannotBeRecordedGivesUpAtTheStoreBound(t *testing.T) {
	u := newUndWorld(t, envMap{EnvStoreTimeoutMS: "200", EnvCompileTimeoutMS: "30000"})
	u.model.respond = func(ci map[string]any, n int) (int, []byte) {
		return http.StatusOK, compiled(ci, intentFor(ci, n, func(m map[string]any) { m["intent_node"] = "fin.not.in.the.pack" }))
	}
	lock, err := u.admin.Begin(deadline(t))
	must(t, err)
	_, err = lock.Exec(deadline(t), `LOCK TABLE record_event IN ACCESS EXCLUSIVE MODE`)
	must(t, err)
	u.as(t, session.RoleCustomer, "ten_aa", "usr_88213")
	must(t, lock.Rollback(deadline(t)))
	if !errors.Is(u.probe.err, interpreter.ErrRecord) {
		t.Fatalf("got %v; want ErrRecord", u.probe.err)
	}
	if u.probe.ctxErr != nil {
		t.Errorf("the request's own deadline ended the call, not the store bound: %v", u.probe.ctxErr)
	}
}

// The Record's actor and the saved intent's principal are the verified session's: an operator's session is an operator (its principal type, from the session and not from the route),
// the issuer is the one that signed the token, and the authentication reference is the recomputed hash.
func TestTheSavedIdentityIsTheVerifiedSessionsInEveryMember(t *testing.T) {
	u := newUndWorld(t, nil)
	if code := u.as(t, session.RoleOps, "ten_aa", "usr_ops1"); code != http.StatusNoContent || u.probe.err != nil || u.probe.in.Kind != orchestrator.Admitted {
		t.Fatalf("%d, %+v, %v", code, u.probe.in, u.probe.err)
	}
	if got := u.n(t, `SELECT count(*) FROM record_event WHERE tenant_id = 'ten_aa' AND event_type = 'INTENT_ADMITTED' AND actor = 'operator:usr_ops1'`); got != 1 {
		t.Error("the Record's actor is not operator:usr_ops1")
	}
	var typ, by, ref, pid string
	must(t, u.admin.QueryRow(deadline(t), `SELECT json->'principal'->>'principal_type', json->'principal'->>'asserted_by', json->'principal'->>'authn_ref', json->'principal'->>'principal_id'
		FROM admitted_intent WHERE tenant_id = 'ten_aa'`).Scan(&typ, &by, &ref, &pid))
	if typ != "operator" || by != session.Issuer || ref != authnRefOf(apiKid, "usr_ops1", apiEpoch.Unix()) || pid != "usr_ops1" {
		t.Errorf("the saved principal is %q, %q, %q, %q", typ, by, ref, pid)
	}
}

// With no verified session and an orchestrator Session with nothing in it, the answer is still no session: an empty context must not equal an empty Session.
func TestAnEmptySessionIsNotTheAbsenceOfOne(t *testing.T) {
	var s edgeSessions
	if _, err := s.Resolve(t.Context(), orchestrator.Session{}); !errors.Is(err, ErrSessionUnavailable) {
		t.Errorf("got %v; want ErrSessionUnavailable", err)
	}
}

// UND-17, A219: the Interpreter port confirms each turn's recorded input, so a build with no admissions is refused and nothing is built.
func TestNoAdmissionsBuildsNothing(t *testing.T) {
	m := newModelStub(t)
	env := Env{Getenv: undSettings(copyPack(t), m).get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	p, err := buildUnderstanding(env, clock.NewManual(apiEpoch), undFakeRecorder(t, "ten_aa"), nil)
	if err == nil || !strings.Contains(err.Error(), "admissions") {
		t.Errorf("no admissions: got %v; want a refusal that names them", err)
	}
	wantNone(t, "no admissions", p)
}
