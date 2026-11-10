package export_test

// Tests for REC-09 (REQ-AUD-003, REQ-BEN-001, REQ-BEN-009; INV-11, INV-12, INV-22). Synthetic data only (AGENT-25): no real person,
// account or institution. The world and the story helpers follow record/reconstruct's tests so the export reads the same Record.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/export"
)

func TestMain(m *testing.M) { testpg.Main(m) }

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

const (
	tenant = "ten_synth01"
	other  = "ten_synth02"
)

type world struct {
	admin   *pgxpool.Pool
	writers map[string]*pgxpool.Pool
	readers map[string]*pgxpool.Pool
	dsn     string
	clk     *clock.Manual
	app     *chain.Appender
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenant, other} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	w := &world{admin: admin, dsn: dsn, writers: map[string]*pgxpool.Pool{}, readers: map[string]*pgxpool.Pool{}}
	for _, id := range []string{tenant, other} {
		w.readers[id] = testpg.TenantLogin(t, admin, dsn, "record_read", id)
	}
	w.clk = clock.NewManual(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))
	w.app = &chain.Appender{Clock: w.clk, IDs: ids.New(w.clk, zeros{})}
	return w
}

var groupOfEmitter = map[string]string{"core-trust": "trust_rw", "core-execution": "execution_rw", "core-outcome": "outcome_rw", "core-evidence": "evidence_rw", "core-workflows": "orchestrator_rw"}

func (w *world) writer(t *testing.T, tenantID, emitter string) *pgxpool.Pool {
	t.Helper()
	key := tenantID + "/" + emitter
	if p, ok := w.writers[key]; ok {
		return p
	}
	if group, ok := groupOfEmitter[emitter]; ok {
		w.writers[key] = testpg.TenantLogin(t, w.admin, w.dsn, group, tenantID)
	} else {
		w.writers[key] = testpg.EmitterLogin(t, w.admin, w.dsn, tenantID, emitter)
	}
	return w.writers[key]
}

type ev struct {
	writtenBy, actor, eventType, objectRef string
	payload                                map[string]any
}

func (w *world) append(t *testing.T, tenantID, corr string, events ...ev) {
	t.Helper()
	for _, e := range events {
		w.clk.Advance(time.Second)
		tx, err := w.writer(t, tenantID, e.writtenBy).Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		actor := e.actor
		if actor == "" {
			actor = e.writtenBy
		}
		if _, err := w.app.Append(ctx(t), tx, chain.Event{TenantID: tenantID, CorrelationID: corr, WrittenBy: e.writtenBy, Actor: actor,
			EventType: e.eventType, ObjectRef: e.objectRef, Payload: e.payload}); err != nil {
			_ = tx.Rollback(ctx(t))
			t.Fatalf("append %s: %v", e.eventType, err)
		}
		if err := tx.Commit(ctx(t)); err != nil {
			t.Fatal(err)
		}
	}
}

func (w *world) tamper(t *testing.T, sql string) {
	t.Helper()
	conn, err := w.admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if tag, err := conn.Exec(ctx(t), sql); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("tamper %q: %v (%d rows)", sql, err, tag.RowsAffected())
	}
}

const (
	customer   = "customer:usr_synth_014"
	capability = "test.cap.create_case"
	grantID    = "grt_synth0001"
)

// story is one case: a customer asks, evidence is read twice, Trust decides, Execution dispatches, Outcome verifies, an
// explanation is rendered. CIRCUIT_STATE_CHANGED carries the case's correlation but is a system-level type the export leaves out.
func story(decision string) []ev {
	return []ev{
		{writtenBy: "core-edge", actor: customer, eventType: "SESSION_VALIDATED", objectRef: "ses_synth0001", payload: map[string]any{"channel": "TEXT"}},
		{writtenBy: "core-edge", actor: customer, eventType: "INPUT_RECEIVED", objectRef: "req_synth0001", payload: map[string]any{"request_id": "req_synth0001", "text_note": "LEAK-CANARY-7731 customer words"}},
		{writtenBy: "model-service", eventType: "INTENT_PROPOSED", objectRef: "int_synth0001", payload: map[string]any{"intent_node": "test.intent.open_case"}},
		{writtenBy: "core-edge", actor: customer, eventType: "INTENT_ADMITTED", objectRef: "int_synth0001", payload: map[string]any{"intent_id": "int_synth0001"}},
		{writtenBy: "core-evidence", eventType: "CANDIDATES_RESOLVED", objectRef: "cns_synth0001", payload: map[string]any{"candidate_count": 1}},
		{writtenBy: "core-evidence", eventType: "EVIDENCE_READ", objectRef: "evr_synth0001", payload: map[string]any{"fact_status": "KNOWN", "source": "mock-ledger"}},
		{writtenBy: "core-evidence", eventType: "EVIDENCE_READ", objectRef: "evr_synth0002", payload: map[string]any{"fact_status": "KNOWN", "source": "mock-switch"}},
		{writtenBy: "core-execution", eventType: "CIRCUIT_STATE_CHANGED", objectRef: "cir_synth0001", payload: map[string]any{"state": "CLOSED"}},
		{writtenBy: "core-trust", eventType: "STATE_RECONCILED", objectRef: "rec_synth0001", payload: map[string]any{"classification": "SETTLED"}},
		{writtenBy: "core-trust", eventType: "POLICY_EVALUATED", objectRef: "dec_synth0001", payload: map[string]any{"policy_id": "test.policy.read_only"}},
		{writtenBy: "core-trust", eventType: "DECISION_MADE", objectRef: "dec_synth0001", payload: map[string]any{"decision_outcome": decision, "decision_id": "dec_synth0001"}},
		{writtenBy: "core-trust", eventType: "PLAN_CREATED", objectRef: "pln_synth0001", payload: map[string]any{"plan_id": "pln_synth0001", "capability_id": capability}},
		{writtenBy: "core-trust", eventType: "GRANT_ISSUED", objectRef: grantID, payload: map[string]any{"grant_id": grantID}},
		{writtenBy: "core-execution", eventType: "GRANT_SPENT", objectRef: grantID, payload: map[string]any{"grant_id": grantID, "attempt": 1}},
		{writtenBy: "core-execution", eventType: "DISPATCH_ATTEMPTED", objectRef: grantID, payload: map[string]any{"attempt": 1, "logical_action": capability}},
		{writtenBy: "core-execution", eventType: "RECEIPT", objectRef: grantID, payload: map[string]any{"receipt_id": "rcp_synth0001", "execution_state": "OUTCOME_UNKNOWN"}},
		{writtenBy: "core-outcome", eventType: "VERIFICATION_READ", objectRef: "ver_synth0001", payload: map[string]any{"fact_status": "KNOWN"}},
		{writtenBy: "core-outcome", eventType: "VERDICT", objectRef: "ver_synth0001", payload: map[string]any{"verdict": "ACHIEVED", "verdict_id": "ver_synth0001"}},
		{writtenBy: "core-edge", eventType: "EXPLANATION_RENDERED", objectRef: "exp_synth0001", payload: map[string]any{"claims": []any{}}},
	}
}

// otherCase is a second case of the same tenant, appended in between, so the first case's seq is not the chain's seq.
func otherCase() []ev {
	return []ev{
		{writtenBy: "core-edge", actor: "customer:usr_synth_099", eventType: "INPUT_RECEIVED", objectRef: "req_synth0099", payload: map[string]any{"request_id": "req_synth0099"}},
		{writtenBy: "core-trust", eventType: "DECISION_MADE", objectRef: "dec_synth0099", payload: map[string]any{"decision_outcome": "REJECT"}},
	}
}

func sha256Of(t *testing.T, v any) string {
	t.Helper()
	b, err := crypto.CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func export1(t *testing.T, w *world, tenantID, corr string) (export.AuditRecord, error) {
	t.Helper()
	return export.AuditRecordOf(ctx(t), w.readers[tenantID], export.Session{TenantID: tenantID}, corr)
}

func ptr(s string) *string { return &s }

// REC-09 acceptance 1 (REQ-AUD-003, REQ-BEN-001): a reviewer reconstructs the decisions from the export alone. The expectations
// below are written from the neutral table of the Benchmark Owner (noema#355), not from the Record.
func TestAReviewerReconstructsTheDecisionsFromTheExportAlone(t *testing.T) {
	w := newWorld(t)
	st := story("ALLOW")
	w.append(t, tenant, "corr_case01", st[:5]...)
	w.append(t, tenant, "corr_case02", otherCase()...)
	w.append(t, tenant, "corr_case01", st[5:]...)
	got, err := export1(t, w, tenant, "corr_case01")
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		step, actor string
		evidence    []string
		decision    *string
		outcome     *string
	}
	// The CIRCUIT_STATE_CHANGED event (system-level) is not an entry; seq runs 1..n.
	wants := []want{
		{"INTAKE", "system", nil, nil, nil}, // SESSION_VALIDATED, written by the edge
		{"INTAKE", "customer", nil, nil, nil},
		{"UNDERSTAND", "model", nil, nil, nil},
		{"UNDERSTAND", "system", nil, nil, nil},
		{"RESOLVE_TARGET", "system", nil, nil, nil},
		{"READ_EVIDENCE", "system", []string{"tc_1"}, nil, nil},
		{"READ_EVIDENCE", "system", []string{"tc_1", "tc_2"}, nil, nil},
		{"RECONCILE", "policy", nil, nil, nil},
		{"DECIDE", "policy", []string{"tc_1", "tc_2"}, nil, nil},
		{"DECIDE", "policy", []string{"tc_1", "tc_2"}, ptr("ALLOW"), nil},
		{"PLAN", "policy", nil, nil, nil},
		{"AUTHORISE", "policy", nil, nil, nil},
		{"AUTHORISE", "executor", nil, nil, nil},
		{"EXECUTE", "executor", nil, nil, nil},
		{"EXECUTE", "executor", nil, nil, nil},
		{"VERIFY", "verifier", nil, nil, nil},
		{"VERIFY", "verifier", nil, nil, ptr("ACHIEVED")},
		{"EXPLAIN", "system", []string{"tc_1", "tc_2"}, nil, nil},
	}
	if len(got.Entries) != len(wants) {
		t.Fatalf("got %d entries, want %d: %+v", len(got.Entries), len(wants), got.Entries)
	}
	rows, err := chain.ReadChain(ctx(t), w.readers[tenant])
	if err != nil {
		t.Fatal(err)
	}
	var caseRows []chain.Row
	for _, r := range rows {
		if r.CorrelationID == "corr_case01" && r.EventType != "CIRCUIT_STATE_CHANGED" {
			caseRows = append(caseRows, r)
		}
	}
	prev := []string{}
	for i, e := range got.Entries {
		x := wants[i]
		if e.Seq != i+1 || e.Step != x.step || e.Actor != x.actor {
			t.Errorf("entry %d: got seq=%d step=%s actor=%s, want seq=%d step=%s actor=%s", i+1, e.Seq, e.Step, e.Actor, i+1, x.step, x.actor)
		}
		if strings.Join(e.EvidenceIDs, ",") != strings.Join(x.evidence, ",") || e.EvidenceIDs == nil {
			t.Errorf("entry %d (%s): evidence %v, want %v (an empty list, never null)", i+1, e.Step, e.EvidenceIDs, x.evidence)
		}
		if fmt.Sprint(deref(e.Decision)) != fmt.Sprint(deref(x.decision)) || fmt.Sprint(deref(e.Outcome)) != fmt.Sprint(deref(x.outcome)) {
			t.Errorf("entry %d (%s): decision=%v outcome=%v, want %v and %v", i+1, e.Step, deref(e.Decision), deref(e.Outcome), deref(x.decision), deref(x.outcome))
		}
		// Ruling A115: outputs_digest = sha256(JCS(["noema/record-export/v1", event_id, payload_hash])). The event id is not exported, so the digest cannot be
		// recomputed from a guessed payload; a holder of the Record can. inputs_digest chains the previous outputs_digest.
		if want := sha256Of(t, []string{"noema/record-export/v1", caseRows[i].EventID, caseRows[i].PayloadHash}); e.OutputsDigest != want {
			t.Errorf("entry %d: outputs_digest %s, want the A115 commitment %s", i+1, e.OutputsDigest, want)
		}
		if e.OutputsDigest == caseRows[i].PayloadHash {
			t.Errorf("entry %d: outputs_digest is the Record's payload_hash, which a guessed payload can confirm", i+1)
		}
		if want := sha256Of(t, prev); e.InputsDigest != want {
			t.Errorf("entry %d: inputs_digest %s, want the digest of the previous outputs_digest %s", i+1, e.InputsDigest, want)
		}
		prev = []string{e.OutputsDigest}
		var body struct {
			At string `json:"at"`
		}
		if err := json.Unmarshal([]byte(caseRows[i].Body), &body); err != nil || e.At != body.At {
			t.Errorf("entry %d: at %q, want the Record's %q", i+1, e.At, body.At)
		}
	}
	d, err := export.Digest(got.Entries)
	if err != nil || d != got.Digest {
		t.Errorf("digest %q, recomputed %q (%v)", got.Digest, d, err)
	}
	if want := sha256Of(t, got.Entries); got.Digest != want {
		t.Errorf("digest %s is not the canonical-JSON digest of the entries %s", got.Digest, want)
	}
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// REC-09 acceptance 2: the digest is stable across two runs on the same Record, and does not move when another case appends later.
func TestTheExportDigestIsStableAcrossTwoRuns(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_case01", story("ALLOW")...)
	a, err := export1(t, w, tenant, "corr_case01")
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := a.JSON()
	w.append(t, tenant, "corr_case02", otherCase()...)
	b, err := export1(t, w, tenant, "corr_case01")
	if err != nil {
		t.Fatal(err)
	}
	jb, _ := b.JSON()
	if a.Digest == "" || a.Digest != b.Digest || string(ja) == "" || string(ja) != string(jb) {
		t.Fatalf("two exports differ: %s vs %s", a.Digest, b.Digest)
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(a.Digest) {
		t.Fatalf("digest %q is not sha256:<64 hex>", a.Digest)
	}
}

// REC-09 acceptance 3 (INV-11): a chain that does not verify is refused and nothing is exported, whether the break is in the case
// or in another case of the same tenant. Fault: a Record with a broken chain; a Record with a gap.
func TestAChainThatDoesNotVerifyIsRefused(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"an edited hashed body", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"grt_forged01"') WHERE seq = 3`},
		{"a broken prior hash", `UPDATE record_event SET prev_hash = 'sha256:` + strings.Repeat("1", 64) + `' WHERE seq = 4`},
		{"a deleted event (a gap)", `DELETE FROM record_event WHERE seq = 5`},
		{"an edited payload", `UPDATE record_event SET payload = '{"verdict": "FAILED"}' WHERE seq = 18`},
		{"a break in another case of the tenant", `UPDATE record_event SET payload = '{"decision_outcome": "ALLOW"}' WHERE seq = 21`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.append(t, tenant, "corr_case01", story("ALLOW")...)
			w.append(t, tenant, "corr_case02", otherCase()...)
			if _, err := export1(t, w, tenant, "corr_case01"); err != nil {
				t.Fatalf("a clean chain does not export: %v", err)
			}
			w.tamper(t, c.sql)
			got, err := export1(t, w, tenant, "corr_case01")
			var be *chain.BreakError
			if !errors.Is(err, export.ErrIntegrity) || !errors.As(err, &be) || len(got.Entries) != 0 || got.Digest != "" {
				t.Fatalf("tampering (%s): got %v and %d entries, want ErrIntegrity with the break and nothing exported", c.name, err, len(got.Entries))
			}
		})
	}
}

// REC-09 acceptance 4 (INV-12, REQ-SEC-004): a call for another tenant is refused. Fault: a cross-tenant call.
func TestACallForAnotherTenantIsRefused(t *testing.T) {
	w := newWorld(t)
	w.append(t, other, "corr_case01", story("ALLOW")...)
	// The session is tenant one; the correlation belongs to tenant two. Tenant one's login cannot read it.
	if _, err := export1(t, w, tenant, "corr_case01"); !errors.Is(err, export.ErrNotFound) {
		t.Fatalf("another tenant's correlation: got %v, want ErrNotFound", err)
	}
	// The runner hands tenant two's handle to tenant one's session: refused as a mismatch, not as a broken chain.
	got, err := export.AuditRecordOf(ctx(t), w.readers[other], export.Session{TenantID: tenant}, "corr_case01")
	if !errors.Is(err, export.ErrTenantMismatch) || len(got.Entries) != 0 {
		t.Fatalf("a handle of another tenant: got %v and %d entries, want ErrTenantMismatch and nothing", err, len(got.Entries))
	}
	// No session tenant: refused before the database is touched.
	if _, err := export.AuditRecordOf(ctx(t), w.readers[tenant], export.Session{}, "corr_case01"); !errors.Is(err, export.ErrNoSession) {
		t.Fatalf("no session tenant: got %v, want ErrNoSession", err)
	}
	// The writer's login (execution_rw) is not the read path: the database refuses it.
	wr := w.writer(t, tenant, "core-execution")
	if _, err := export.AuditRecordOf(ctx(t), wr, export.Session{TenantID: tenant}, "corr_case01"); err == nil || errors.Is(err, export.ErrIntegrity) || !strings.Contains(err.Error(), "42501") {
		t.Fatalf("a non record_read login: got %v, want a permission refusal (42501)", err)
	}
}

// An unknown correlation of the session's own tenant is also not found; the export needs a deadline like every Record read.
func TestAnUnknownCorrelationIsNotFoundAndTheContextNeedsADeadline(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_case01", story("ALLOW")...)
	if _, err := export1(t, w, tenant, "corr_none01"); !errors.Is(err, export.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if _, err := export.AuditRecordOf(context.Background(), w.readers[tenant], export.Session{TenantID: tenant}, "corr_case01"); err == nil {
		t.Fatal("an export without a context deadline did not fail")
	}
}

// Outcome writes the verdict document nested under "verdict" (outcome/store), the enum at verdict.verdict; the flat form that the
// reconstruction reads is accepted too (execution/recovery does the same). Both give the same outcome label.
func TestTheVerdictLabelIsReadFromTheNestedDocumentOutcomeWrites(t *testing.T) {
	w := newWorld(t)
	s := story("ALLOW")
	s[17].payload = map[string]any{"verdict": map[string]any{"verdict": "NOT_ACHIEVED", "verdict_id": "ver_synth0001"}, "reason": "READ_MISMATCH"}
	w.append(t, tenant, "corr_case01", s...)
	got, err := export1(t, w, tenant, "corr_case01")
	if err != nil {
		t.Fatal(err)
	}
	last := got.Entries[len(got.Entries)-2]
	if last.Step != "VERIFY" || deref(last.Outcome) != "NOT_ACHIEVED" {
		t.Fatalf("got step=%s outcome=%v, want VERIFY and NOT_ACHIEVED", last.Step, deref(last.Outcome))
	}
}

// An event whose payload does not carry a legal disposition or verdict label is refused, never guessed (fail closed).
func TestAnIllegalDispositionOrVerdictIsRefused(t *testing.T) {
	for name, mut := range map[string]func([]ev){
		"a disposition outside the decision set": func(s []ev) { s[10].payload["decision_outcome"] = "MAYBE" },
		"a decision with no disposition":         func(s []ev) { delete(s[10].payload, "decision_outcome") },
		"a verdict with no label":                func(s []ev) { delete(s[17].payload, "verdict") },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			s := story("ALLOW")
			mut(s)
			w.append(t, tenant, "corr_case01", s...)
			if got, err := export1(t, w, tenant, "corr_case01"); !errors.Is(err, export.ErrUnprojectable) || len(got.Entries) != 0 {
				t.Fatalf("got %v and %d entries, want ErrUnprojectable and nothing", err, len(got.Entries))
			}
		})
	}
}

// INV-22, REQ-BEN-009: only the neutral fields leave. No payload text, no customer words, no key that is not in the contract.
func TestNothingButTheNeutralFieldsLeavesTheRecord(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_case01", story("ALLOW")...)
	got, err := export1(t, w, tenant, "corr_case01")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := got.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"LEAK-CANARY-7731", "customer words", "mock-ledger", "test.intent", "usr_synth", capability, "grt_synth", "expected", "ground_truth"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the export contains %q, which is not a neutral field", leak)
		}
	}
	checkAgainstSchema(t, raw)
}

// checkAgainstSchema reads AuditRecord and AuditEntry from the contract and checks the export field by field: the same key set
// (additionalProperties false), the same patterns, the same nullability.
func armResultSchema(t *testing.T) string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "*-arm-result.v1.json"))
	if err != nil || len(m) != 1 {
		t.Fatalf("the arm-result contract: %v (%v)", m, err)
	}
	return m[0]
}

func checkAgainstSchema(t *testing.T, raw []byte) {
	t.Helper()
	b, err := os.ReadFile(armResultSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Required   []string                  `json:"required"`
			Properties map[string]map[string]any `json:"properties"`
			Pattern    string                    `json:"pattern"`
			Extra      any                       `json:"additionalProperties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Entries []map[string]any `json:"entries"`
		Digest  string           `json:"digest"`
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&doc); err != nil {
		t.Fatalf("the export is not an AuditRecord: %v", err)
	}
	rec, entry, tc := schema.Defs["AuditRecord"], schema.Defs["AuditEntry"], schema.Defs["ToolCallId"]
	if len(rec.Properties) != 2 || doc.Digest == "" || !regexp.MustCompile(rec.Properties["digest"]["pattern"].(string)).MatchString(doc.Digest) {
		t.Fatalf("the audit_record does not match AuditRecord: %v", rec.Properties)
	}
	toolCall := regexp.MustCompile(tc.Pattern)
	for i, e := range doc.Entries {
		if len(e) != len(entry.Properties) {
			t.Errorf("entry %d has %d keys, the contract has %d", i+1, len(e), len(entry.Properties))
		}
		for _, k := range entry.Required {
			v, ok := e[k]
			if !ok {
				t.Errorf("entry %d lacks %s", i+1, k)
				continue
			}
			if p, ok := entry.Properties[k]["pattern"].(string); ok {
				if s, isStr := v.(string); !isStr || !regexp.MustCompile(p).MatchString(s) {
					t.Errorf("entry %d: %s=%v does not match %s", i+1, k, v, p)
				}
			}
		}
		for _, id := range e["evidence_ids"].([]any) {
			if !toolCall.MatchString(id.(string)) {
				t.Errorf("entry %d: evidence id %v is not a ToolCallId", i+1, id)
			}
		}
		for _, k := range []string{"decision", "outcome"} {
			if v := e[k]; v != nil && !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(v.(string)) {
				t.Errorf("entry %d: %s=%v is not an upper-case label", i+1, k, v)
			}
		}
	}
}

// INV-22 at the source: the package imports only the standard library, pgx and the core's platform and chain packages. Nothing
// from the scoring side, the oracle or the held-out set can be reached by an import.
func TestThePackageImportsOnlyTheRecordAndPlatformPackages(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources: %v", err)
	}
	const core = "github.com/Noemai-org/noema/services/core/"
	allowed := map[string]bool{core + "platform/crypto": true, core + "record/chain": true, "github.com/jackc/pgx/v5": true}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			if first, _, _ := strings.Cut(p, "/"); !strings.Contains(first, ".") {
				continue // the standard library
			}
			if !allowed[p] {
				t.Errorf("%s imports %s, which is not on the list", f, p)
			}
		}
	}
}

// Fail closed: every Record event type has exactly one row, in the step table or among the system-level types the export leaves
// out. A type the contract adds later makes this test fail, not the export silently drop it (noema#355, item 1).
func TestEveryRecordEventTypeHasARowInTheStepTable(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "record-event.v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Defs struct {
			Types struct {
				Enum []string `json:"enum"`
			} `json:"RecordEventType"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &s); err != nil || len(s.Defs.Types.Enum) != 57 {
		t.Fatalf("the contract's event types: %v (%d)", err, len(s.Defs.Types.Enum))
	}
	steps := map[string]int{}
	left := 0
	for _, et := range s.Defs.Types.Enum {
		step, inCase, known := export.StepOf(et)
		if !known {
			t.Errorf("%s has no row", et)
			continue
		}
		if inCase {
			steps[step]++
		} else {
			left++
		}
	}
	names := make([]string, 0, len(steps))
	for k := range steps {
		names = append(names, k)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "AUTHORISE,CASE,CLARIFY,CONFIRM,DECIDE,EXECUTE,EXPLAIN,INTAKE,PLAN,READ_EVIDENCE,RECONCILE,RESOLVE_TARGET,SECURITY,UNDERSTAND,VERIFY" || left != 13 {
		t.Fatalf("steps %v and %d left out, want the 15 steps and 13 system-level types of noema#355", names, left)
	}
	if _, _, known := export.StepOf("NOT_A_RECORD_EVENT"); known {
		t.Fatal("an unknown type is known")
	}
}

// The scoring side's worked example (noema#355, item 7), computed there with the scorer's sha256_of: three entries
// whose payload hashes are sha256 of "example payload 1", "2" and "3". The Go digest equals the Python one.
func TestTheWorkedExampleOfNoema355HasTheSameDigest(t *testing.T) {
	ph := func(n int) string {
		sum := sha256.Sum256([]byte(fmt.Sprintf("example payload %d", n)))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	entries := []export.Entry{
		{Seq: 1, At: "2026-11-08T09:00:00Z", Step: "INTAKE", Actor: "customer", EvidenceIDs: []string{}, InputsDigest: sha256Of(t, []string{}), OutputsDigest: ph(1)},
		{Seq: 2, At: "2026-11-08T09:00:02Z", Step: "READ_EVIDENCE", Actor: "system", EvidenceIDs: []string{"tc_1"}, InputsDigest: sha256Of(t, []string{ph(1)}), OutputsDigest: ph(2)},
		{Seq: 3, At: "2026-11-08T09:00:03Z", Step: "DECIDE", Actor: "policy", EvidenceIDs: []string{"tc_1"}, InputsDigest: sha256Of(t, []string{ph(2)}), OutputsDigest: ph(3), Decision: ptr("ESCALATE")},
	}
	if !strings.HasPrefix(entries[0].InputsDigest, "sha256:4f53cda1") || !strings.HasPrefix(entries[1].InputsDigest, "sha256:36c03b4b") || !strings.HasPrefix(entries[2].InputsDigest, "sha256:7cf7967c") {
		t.Fatalf("the inputs digests differ from the worked example: %s %s %s", entries[0].InputsDigest, entries[1].InputsDigest, entries[2].InputsDigest)
	}
	got, err := export.Digest(entries)
	const want = "sha256:baae831e7875de3ceb03516bc5c3cefde33e0735e7b0d0640a56e6e981be7cb9"
	if err != nil || got != want {
		t.Fatalf("digest %q (%v), want %s", got, err, want)
	}
}

// Ruling A115, first adversarial regression (noema#383, the Security Reviewer's F1 on noema#377): no event id of the Record appears anywhere in the export.
func TestNoRecordEventIDAppearsAnywhereInTheExport(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_case01", story("ALLOW")...)
	got, err := export1(t, w, tenant, "corr_case01")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := got.JSON()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := chain.ReadChain(ctx(t), w.readers[tenant])
	if err != nil || len(rows) < 15 {
		t.Fatalf("reading the Record: %v (%d rows)", err, len(rows))
	}
	for _, r := range rows {
		if strings.Contains(string(raw), r.EventID) {
			t.Errorf("the export contains the event id %s", r.EventID)
		}
	}
}

// Ruling A115, second adversarial regression: a guessed set of low-entropy payloads, hashed the obvious ways (the Record's own payload hash, and a digest
// of it alone or under the format label), matches no value the export carries. Before A115 outputs_digest WAS the payload hash, so every payload below that
// a case holds would have matched.
func TestAGuessedSetOfLowEntropyPayloadsMatchesNoExportedValue(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_case01", story("ALLOW")...)
	got, err := export1(t, w, tenant, "corr_case01")
	if err != nil {
		t.Fatal(err)
	}
	exported := map[string]bool{got.Digest: true}
	for _, e := range got.Entries {
		exported[e.InputsDigest], exported[e.OutputsDigest] = true, true
	}
	guesses := []map[string]any{
		{}, {"state": "CLOSED"}, {"state": "OPEN"}, {"classification": "SETTLED"}, {"decision_outcome": "ALLOW"}, {"decision_outcome": "ESCALATE"},
		{"decision_outcome": "REJECT"}, {"fact_status": "KNOWN", "source": "mock-ledger"}, {"fact_status": "KNOWN"}, {"channel": "TEXT"},
		{"claims": []any{}}, {"candidate_count": 1}, {"attempt": 1},
	}
	tried := 0
	for _, g := range guesses {
		b, err := crypto.CanonicalJSON(g)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		h := "sha256:" + hex.EncodeToString(sum[:])
		for name, v := range map[string]string{"the payload hash": h, "a digest of it": sha256Of(t, []string{h}), "it under the label": sha256Of(t, []string{"noema/record-export/v1", h}),
			"it with an empty id": sha256Of(t, []string{"noema/record-export/v1", "", h})} {
			tried++
			if exported[v] {
				t.Errorf("the guessed payload %v matches an exported value as %s", g, name)
			}
		}
	}
	if tried != 4*len(guesses) {
		t.Fatalf("tried %d candidates", tried)
	}
}
