package main

// TRU-02 (REQ-TRU-005, REQ-OPS-003, REQ-AUD-001; INV-09, INV-12, INV-21, AGENT-13): the policy publication command. Every test runs the command's own run function against a throwaway
// PostgreSQL database with the real migrations, the real Trust login of a tenant and the real Record signer, and reads the result back as Execution does (policystore.Active) and
// from the Record. The pack is the shipped packs/fintech_v1.json or a copy of it with the policy changed. All data is SYNTHETIC.
//
// What these tests do not and cannot show: that a second document under the same version is refused. The publication record holds the version, not the document (policystore,
// DEBT-23), so the command cannot tell two documents of one version apart; status/TRU-02-policypublish.md says so.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA  = "ten_aa"
	tenantB  = "ten_bb"
	policyID = "fin.pol"
	packName = "fintech_v1.json"
	packRoot = "../../../../packs"
)

// keyDirAt is the index of the key directory in the arguments w.args builds.
const keyDirAt = 9

var epoch = time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

type world struct {
	t                    *testing.T
	admin                *pgxpool.Pool
	adminDSN             string
	trustA, trustB, exeA *pgxpool.Pool
	keyDir               string
	files                string
}

// writeKey writes a SYNTHETIC core-trust signing key as the deploy step does (0400, the seed in hex) and returns the loaded ring.
func writeKey(t *testing.T, dir, label string) *signing.Ring {
	t.Helper()
	seed := make([]byte, 32)
	copy(seed, label)
	path := filepath.Join(dir, "core-trust.ed25519.1.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(seed)+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	ring, err := signing.LoadRing("core-trust", dir, signing.Options{MaxActive: 1})
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
	}
	w := &world{t: t, admin: admin, adminDSN: dsn, keyDir: t.TempDir(), files: t.TempDir(),
		trustA: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantA), trustB: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantB),
		exeA: testpg.TenantLogin(t, admin, dsn, "execution_rw", tenantA)}
	ring := writeKey(t, w.keyDir, "policypublish registered")
	for id, pub := range ring.PublicKeys() {
		if id != "core-trust.ed25519.1" {
			t.Fatalf("unexpected key %s", id)
		}
		if _, err := admin.Exec(ctx(t), `SELECT key_register('core-trust', 1, $1)`, []byte(pub)); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// dsnFile writes the DSN of a pool's login into a file only its owner can read, as the deploy step mounts one.
func (w *world) dsnFile(name, dsn string) string {
	w.t.Helper()
	p := filepath.Join(w.files, name)
	if err := os.WriteFile(p, []byte(dsn+"\n"), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) login(name string, p *pgxpool.Pool) string {
	return w.dsnFile(name, p.Config().ConnString())
}

type report struct {
	Result        string `json:"result"`
	Reason        string `json:"reason"`
	Policy        string `json:"policy"`
	TenantID      string `json:"tenant_id"`
	CorrelationID string `json:"correlation_id"`
	Event         string `json:"event"`
	Service       string `json:"service"`
	Power         string `json:"power"`
	Level         string `json:"level"`
	Msg           string `json:"msg"`
}

type outcome struct {
	code     int
	out, err string
	rep      report
}

func (w *world) args(tenant, dsnFile, packDir string) []string {
	return []string{"--tenant", tenant, "--pack-dir", packDir, "--pack-file", packName, "--dsn-file", dsnFile, "--key-dir", w.keyDir, "--key-max-active", "1", "--published-by", "adm_synth01"}
}

func (w *world) run(args ...string) outcome {
	w.t.Helper()
	var out, errw bytes.Buffer
	code := run(args, &out, &errw, clock.NewManual(epoch))
	o := outcome{code: code, out: out.String(), err: errw.String()}
	lines := strings.Split(strings.TrimSpace(o.out), "\n")
	if last := lines[len(lines)-1]; last != "" {
		_ = json.Unmarshal([]byte(last), &o.rep)
	}
	return o
}

func (w *world) publish(tenant string, login *pgxpool.Pool) outcome {
	w.t.Helper()
	return w.run(w.args(tenant, w.login("trust-"+tenant, login), packRoot)...)
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(w.t), query, args...).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *world) nothingWritten() {
	w.t.Helper()
	if n := w.count(`SELECT count(*) FROM policy_publication`); n != 0 {
		w.t.Fatalf("%d publications written, want none", n)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 0 {
		w.t.Fatalf("%d POLICY_PUBLISHED events written, want none", n)
	}
}

func (w *world) activeAsExecution() string {
	w.t.Helper()
	v, err := policystore.Active(ctx(w.t), w.exeA, tenantA, policyID)
	if err != nil {
		w.t.Fatal(err)
	}
	return v
}

// variantPack writes a copy of the shipped pack into a new directory with the policy changed (or removed, when edit returns false), and returns the directory.
func variantPack(t *testing.T, edit func(pol map[string]any) bool) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packRoot, packName))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var pol map[string]any
	if err := json.Unmarshal(top["policy"], &pol); err != nil {
		t.Fatal(err)
	}
	if edit(pol) {
		if top["policy"], err = json.Marshal(pol); err != nil {
			t.Fatal(err)
		}
	} else {
		delete(top, "policy")
	}
	b, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, packName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func withVersion(v string) func(map[string]any) bool {
	return func(pol map[string]any) bool { pol["version"] = v; return true }
}

// REQ-TRU-005, INV-09: the version in force for the tenant is the one the pack's document names, and Execution reads it back as policy_id@version.
func TestPublishesThePackPolicyVersionAndActiveReadsItBack(t *testing.T) {
	w := newWorld(t)
	o := w.publish(tenantA, w.trustA)
	if o.code != 0 || o.rep.Result != "published" || o.rep.Policy != "fin.pol@3.1" || o.rep.TenantID != tenantA {
		t.Fatalf("exit %d, report %+v, stderr %q; want 0, published fin.pol@3.1", o.code, o.rep, o.err)
	}
	if o.rep.Event != "policy_publish" || o.rep.Service != "policypublish" || o.rep.Power != "trust" || o.rep.Level != "info" || !strings.HasPrefix(o.rep.CorrelationID, "corr_") {
		t.Fatalf("the log line lacks its fields: %+v", o.rep)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.1" {
		t.Fatalf("active %q, want fin.pol@3.1", v)
	}
	var typ, by, actor, ref, payload, corr string
	if err := w.admin.QueryRow(ctx(t), `SELECT event_type, written_by, actor, object_ref, payload::text, correlation_id FROM record_event WHERE tenant_id = $1`, tenantA).Scan(&typ, &by, &actor, &ref, &payload, &corr); err != nil {
		t.Fatal(err)
	}
	if typ != "POLICY_PUBLISHED" || by != "core-trust" || actor != "admin:adm_synth01" || ref != "pol_fin_pol_3_1" || corr != o.rep.CorrelationID {
		t.Fatalf("event %s by %s actor %s ref %s correlation %s", typ, by, actor, ref, corr)
	}
	for _, want := range []string{`"policy_id": "fin.pol"`, `"version": "3.1"`, `"published_by": "adm_synth01"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload %s lacks %s", payload, want)
		}
	}
	if n := w.count(`SELECT count(*) FROM policy_publication WHERE tenant_id = $1`, tenantB); n != 0 {
		t.Fatalf("tenant B has %d publications after a publication for tenant A", n)
	}
}

// Idempotent: a second run with the same pack changes nothing, says so, and exits 0 (a seed script may run twice).
func TestASecondRunWithTheSameVersionIsANoOpThatSaysAlreadyPublished(t *testing.T) {
	w := newWorld(t)
	if o := w.publish(tenantA, w.trustA); o.code != 0 {
		t.Fatalf("first run: exit %d %q", o.code, o.err)
	}
	o := w.publish(tenantA, w.trustA)
	if o.code != 0 || o.rep.Result != "already_published" || o.rep.Policy != "fin.pol@3.1" {
		t.Fatalf("second run: exit %d, report %+v, stderr %q; want 0, already_published", o.code, o.rep, o.err)
	}
	if n := w.count(`SELECT count(*) FROM policy_publication`); n != 1 {
		t.Fatalf("%d publications, want 1", n)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 1 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 1: a no-op writes nothing", n)
	}
}

// The version comes from the document, never from a flag: a newer document is published and becomes active; the older one after it is refused as policystore requires.
func TestANewerPackVersionPublishesAndAnOlderOneAfterItIsRefused(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	newer := variantPack(t, withVersion("3.2"))
	if o := w.run(w.args(tenantA, login, packRoot)...); o.code != 0 || o.rep.Policy != "fin.pol@3.1" {
		t.Fatalf("3.1: exit %d %+v", o.code, o.rep)
	}
	if o := w.run(w.args(tenantA, login, newer)...); o.code != 0 || o.rep.Result != "published" || o.rep.Policy != "fin.pol@3.2" {
		t.Fatalf("3.2: exit %d %+v %q", o.code, o.rep, o.err)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.2" {
		t.Fatalf("active %q, want fin.pol@3.2", v)
	}
	o := w.run(w.args(tenantA, login, packRoot)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "not_newer" || o.rep.Level != "error" || o.err == "" {
		t.Fatalf("3.1 after 3.2: exit %d, report %+v, stderr %q; want 1, refused, not_newer", o.code, o.rep, o.err)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.2" {
		t.Fatalf("active %q after the refused downgrade, want fin.pol@3.2", v)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 2 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 2: a refused downgrade leaves none", n)
	}
}

// REQ-SEC-004, INV-12: a tenant that does not exist, or that the login is not the Trust login of, is refused with one answer, before anything is written.
func TestAnUnknownTenantIsRefusedAndNothingIsWritten(t *testing.T) {
	w := newWorld(t)
	o := w.run(w.args("ten_zz", w.login("trust-a", w.trustA), packRoot)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "login_not_for_tenant" {
		t.Fatalf("unknown tenant: exit %d, report %+v, stderr %q", o.code, o.rep, o.err)
	}
	w.nothingWritten()
}

func TestAWrongLoginIsRefusedAndNothingIsWritten(t *testing.T) {
	w := newWorld(t)
	for name, dsn := range map[string]string{
		"another tenant's trust login": w.login("trust-b", w.trustB),
		"the tenant's execution login": w.login("exec-a", w.exeA),
		"the administrator (unmapped)": w.dsnFile("admin", w.adminDSN),
	} {
		o := w.run(w.args(tenantA, dsn, packRoot)...)
		if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "login_not_for_tenant" {
			t.Errorf("%s: exit %d, report %+v, stderr %q; want 1, refused, login_not_for_tenant", name, o.code, o.rep, o.err)
		}
	}
	w.nothingWritten()
}

// A login that may only read must not learn "already published" either: the login is checked before the active version is read.
func TestALoginThatIsNotTheTrustLoginIsRefusedEvenWhenThePolicyIsAlreadyPublished(t *testing.T) {
	w := newWorld(t)
	if o := w.publish(tenantA, w.trustA); o.code != 0 {
		t.Fatalf("publish: exit %d %q", o.code, o.err)
	}
	o := w.run(w.args(tenantA, w.login("exec-a", w.exeA), packRoot)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "login_not_for_tenant" {
		t.Fatalf("execution login: exit %d, report %+v; want refused", o.code, o.rep)
	}
}

// AGENT-13: no DSN, password, login name or driver text in the output, on success, on a refusal and when the database cannot be reached.
func TestTheOutputHoldsNoCredentialAndNoDriverText(t *testing.T) {
	w := newWorld(t)
	good := w.login("trust-a", w.trustA)
	cfg := w.trustA.Config().ConnConfig
	unreachable := w.dsnFile("down", "postgres://svc_secretname:SECRETPW@127.0.0.1:1/secretdb?sslmode=disable&connect_timeout=2")
	cases := map[string]outcome{
		"success":     w.run(w.args(tenantA, good, packRoot)...),
		"refusal":     w.run(w.args(tenantA, w.login("exec-a", w.exeA), packRoot)...),
		"unreachable": w.run(w.args(tenantA, unreachable, packRoot)...),
	}
	if c := cases["unreachable"].code; c != 2 {
		t.Fatalf("an unreachable database: exit %d, want 2", c)
	}
	for name, o := range cases {
		text := o.out + o.err
		for _, banned := range []string{cfg.User, "SECRETPW", "svc_secretname", "secretdb", "postgres://", "password", "pgconn", "ERROR:", w.trustA.Config().ConnString()} {
			if banned != "" && strings.Contains(text, banned) {
				t.Errorf("%s: the output holds %q:\n%s", name, banned, text)
			}
		}
	}
}

func TestTheDocumentNamesThePolicyAndNoFlagDoes(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	for _, extra := range [][]string{{"--policy-id", "fin.other"}, {"--version", "9.9"}, {"--dsn", "postgres://x"}} {
		if o := w.run(append(w.args(tenantA, login, packRoot), extra...)...); o.code != 2 {
			t.Errorf("%v: exit %d, want 2 (unknown flag)", extra, o.code)
		}
	}
	w.nothingWritten()
}

func TestAMissingOrMalformedArgumentIsAUsageError(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	full := w.args(tenantA, login, packRoot)
	for i := 0; i < len(full); i += 2 {
		args := append(append([]string{}, full[:i]...), full[i+2:]...)
		if o := w.run(args...); o.code != 2 {
			t.Errorf("without %s: exit %d, want 2", full[i], o.code)
		}
	}
	for name, args := range map[string][]string{
		"a tenant that is not an ID": w.args("tenant-a", login, packRoot),
		"a max-active of zero":       append(w.args(tenantA, login, packRoot), "--key-max-active", "0"),
		"a stray argument":           append(w.args(tenantA, login, packRoot), "extra"),
		"no arguments":               nil,
	} {
		if o := w.run(args...); o.code != 2 {
			t.Errorf("%s: exit %d, want 2", name, o.code)
		}
	}
	w.nothingWritten()
}

// REQ-TRU-004: a document the policy loader refuses is never published: nothing is written.
func TestAnInvalidPolicyDocumentIsRefusedBeforeAnyWrite(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	rules := func(pol map[string]any) []any { return pol["rules"].([]any) }
	cases := map[string]string{
		"a rule for a capability the pack does not register": variantPack(t, func(pol map[string]any) bool {
			rules(pol)[0].(map[string]any)["applies_to"] = []any{"fin.cap.never_registered"}
			return true
		}),
		"a rule with an unknown effect": variantPack(t, func(pol map[string]any) bool {
			rules(pol)[0].(map[string]any)["effect"] = "ALLOW"
			return true
		}),
		"no policy in the pack":         variantPack(t, func(map[string]any) bool { return false }),
		"a version outside its pattern": variantPack(t, withVersion("3.1.0")),
		"no policy ID":                  variantPack(t, func(pol map[string]any) bool { delete(pol, "policy_id"); return true }),
	}
	for name, dir := range cases {
		o := w.run(w.args(tenantA, login, dir)...)
		if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "invalid_policy" {
			t.Errorf("%s: exit %d, report %+v, stderr %q; want 1, refused, invalid_policy", name, o.code, o.rep, o.err)
		}
	}
	w.nothingWritten()
}

// The pack file is read inside the pack directory only: a name with a separator, and a link that leaves the directory, cannot be read.
func TestAPackFileThatLeavesThePackDirectoryCannotBeRead(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	real, err := filepath.Abs(filepath.Join(packRoot, packName))
	if err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	if err := os.Symlink(real, filepath.Join(linked, packName)); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"a link out of the directory": w.args(tenantA, login, linked),
		"a parent path":               append(w.args(tenantA, login, packRoot+"/fintech"), "--pack-file", "../fintech_v1.json"),
		"an absolute path":            append(w.args(tenantA, login, packRoot), "--pack-file", real),
		"a missing file":              append(w.args(tenantA, login, packRoot), "--pack-file", "missing.json"),
		"a missing directory":         w.args(tenantA, login, filepath.Join(t.TempDir(), "nowhere")),
	} {
		if o := w.run(args...); o.code != 2 {
			t.Errorf("%s: exit %d, want 2", name, o.code)
		}
	}
	w.nothingWritten()
}

func TestADsnFileThatCannotBeUsedIsRefused(t *testing.T) {
	w := newWorld(t)
	empty := w.dsnFile("empty", "")
	garbage := w.dsnFile("garbage", "this is not a DSN SECRETWORD")
	link := filepath.Join(w.files, "link")
	if err := os.Symlink(w.login("trust-a", w.trustA), link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(w.files, "nowhere"), "empty": empty, "unparsable": garbage, "a link": link, "a directory": w.files} {
		o := w.run(w.args(tenantA, path, packRoot)...)
		if o.code != 2 {
			t.Errorf("%s: exit %d, want 2", name, o.code)
		}
		if strings.Contains(o.out+o.err, "SECRETWORD") {
			t.Errorf("%s: the output quotes the file", name)
		}
	}
	w.nothingWritten()
}

// PERD 32.4: the Record comes first. A signing key the database does not know makes the append fail; the publication, written in the same transaction, is not kept.
func TestWhenTheRecordAppendFailsNoPublicationIsKept(t *testing.T) {
	w := newWorld(t)
	unknown := t.TempDir()
	writeKey(t, unknown, "a key nobody registered")
	args := w.args(tenantA, w.login("trust-a", w.trustA), packRoot)
	args[keyDirAt] = unknown
	if o := w.run(args...); o.code != 2 || o.rep.Result != "error" {
		t.Fatalf("exit %d, report %+v, stderr %q; want 2, error", o.code, o.rep, o.err)
	}
	w.nothingWritten()
	if v := w.activeAsExecution(); v != "" {
		t.Fatalf("active %q, want none", v)
	}
}

func TestAKeyDirectoryWithNoKeyCannotSign(t *testing.T) {
	w := newWorld(t)
	args := w.args(tenantA, w.login("trust-a", w.trustA), packRoot)
	args[keyDirAt] = t.TempDir()
	if o := w.run(args...); o.code != 2 {
		t.Fatalf("exit %d, want 2", o.code)
	}
	args[keyDirAt] = filepath.Join(t.TempDir(), "nowhere")
	if o := w.run(args...); o.code != 2 {
		t.Fatalf("a missing key directory: exit %d, want 2", o.code)
	}
	w.nothingWritten()
}

// Two seed scripts at once: both succeed, one publishes, the other finds it already published (the database serialises the version check).
func TestTwoRunsAtOnceLeaveOnePublicationAndBothSucceed(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	const runs = 6
	results := make([]outcome, runs)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = w.run(w.args(tenantA, login, packRoot)...)
		}()
	}
	wg.Wait()
	published := 0
	for i, o := range results {
		if o.code != 0 {
			t.Errorf("run %d: exit %d, report %+v, stderr %q", i, o.code, o.rep, o.err)
		}
		if o.rep.Result == "published" {
			published++
		} else if o.rep.Result != "already_published" {
			t.Errorf("run %d: result %q", i, o.rep.Result)
		}
	}
	if published != 1 {
		t.Fatalf("%d runs published, want exactly 1", published)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 1 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 1", n)
	}
}
