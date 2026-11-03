package reconstruct_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// -update rewrites the golden reconstructions in testdata/. tools/contracts/test_reconstruction_golden.py validates
// the committed files against contracts/schemas/reconstruction.v3.json.
var update = flag.Bool("update", false, "rewrite testdata/*.golden.json")

func TestMain(m *testing.M) { testpg.Main(m) }

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// zeros is entropy for test IDs: with a clock that moves one second per event, every ID is unique and the same on
// every run, so a reconstruction can be compared byte for byte with a committed golden file.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

type world struct {
	admin  *pgxpool.Pool
	writer *pgxpool.Pool // appends as execution_rw, through record_lock_head and record_insert only
	reader *pgxpool.Pool // reads as record_read, through record_read_chain only (migration 0003)
	dsn    string
	clk    *clock.Manual
	app    *chain.Appender
}

const (
	tenant = "ten_synth01"
	other  = "ten_synth02"
)

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
	writer, err := db.OpenAsRole(ctx(t), dsn, "execution_rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Close)
	reader, err := db.OpenAsRole(ctx(t), dsn, "record_read")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	clk := clock.NewManual(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))
	return &world{admin: admin, writer: writer, reader: reader, dsn: dsn, clk: clk, app: &chain.Appender{Clock: clk, IDs: ids.New(clk, zeros{})}}
}

// ev is one Record event of a story, before it is appended.
type ev struct {
	writtenBy, actor, eventType, objectRef, kind, reason string
	payload                                              map[string]any
}

func (w *world) append(t *testing.T, tenantID, corr string, events ...ev) []chain.Stored {
	t.Helper()
	var out []chain.Stored
	for _, e := range events {
		w.clk.Advance(time.Second)
		tx, err := w.writer.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		actor := e.actor
		if actor == "" {
			actor = e.writtenBy
		}
		st, err := w.app.Append(ctx(t), tx, chain.Event{TenantID: tenantID, CorrelationID: corr, WrittenBy: e.writtenBy, Actor: actor,
			EventType: e.eventType, Kind: e.kind, Reason: e.reason, ObjectRef: e.objectRef, Payload: e.payload})
		if err != nil {
			_ = tx.Rollback(ctx(t))
			t.Fatalf("append %s: %v", e.eventType, err)
		}
		if err := tx.Commit(ctx(t)); err != nil {
			t.Fatal(err)
		}
		out = append(out, st)
	}
	return out
}

// Synthetic stories (AGENT-25): no real person, account or institution. The pack IDs use the neutral "test" vertical.
const (
	customer   = "customer:usr_synth_014"
	target     = "test.transaction:TX0001"
	capability = "test.cap.create_case"
	grantID    = "grt_synth0001"
)

// us01 is US-01 "Unique transaction" (PERD v1.19 story table): decision ALLOW, zero grants issued, the explanation
// cites a KNOWN state, the Record holds CANDIDATES_RESOLVED with one candidate and a margin of at least 0.35. The
// margin is a decimal string because a payload holds integers only (PERD 32.1).
func us01() []ev {
	return []ev{
		{writtenBy: "core-edge", actor: customer, eventType: "SESSION_VALIDATED", objectRef: "ses_synth0001", payload: map[string]any{"channel": "TEXT", "locale": "en"}},
		{writtenBy: "core-edge", actor: customer, eventType: "INPUT_RECEIVED", objectRef: "req_synth0001", payload: map[string]any{"channel": "TEXT", "request_id": "req_synth0001"}},
		{writtenBy: "model-service", eventType: "INTENT_PROPOSED", objectRef: "int_synth0001", payload: map[string]any{"intent_node": "test.intent.locate_transfer", "note": "model text is never a fact"}},
		{writtenBy: "core-edge", actor: customer, eventType: "INTENT_ADMITTED", objectRef: "int_synth0001", payload: map[string]any{"intent_node": "test.intent.locate_transfer", "intent_id": "int_synth0001"}},
		{writtenBy: "core-evidence", eventType: "CANDIDATES_RESOLVED", objectRef: "cns_synth0001", payload: map[string]any{"resolved_target": target, "candidate_count": 1, "margin": "0.41"}},
		{writtenBy: "core-evidence", eventType: "EVIDENCE_READ", objectRef: "evr_synth0001", payload: map[string]any{"fact_status": "KNOWN", "source": "mock-ledger"}},
		{writtenBy: "core-trust", eventType: "STATE_RECONCILED", objectRef: "rec_synth0001", payload: map[string]any{"classification": "SETTLED", "fact_status": "KNOWN"}},
		{writtenBy: "core-trust", eventType: "POLICY_EVALUATED", objectRef: "dec_synth0001", payload: map[string]any{"policy_id": "test.policy.read_only", "policy_version": "3"}},
		{writtenBy: "core-trust", eventType: "DECISION_MADE", objectRef: "dec_synth0001", payload: map[string]any{"decision_outcome": "ALLOW", "reason_codes": []any{"STATE_KNOWN"}, "decision_id": "dec_synth0001"}},
		{writtenBy: "core-edge", eventType: "EXPLANATION_RENDERED", objectRef: "exp_synth0001", payload: map[string]any{"claims": []any{
			map[string]any{"fact_id": "f1", "text": "Your transfer ending 0001 has settled.", "bound_fact_status": "KNOWN", "disclosed_refs": []any{target}},
		}}},
	}
}

// us06 is US-06 "Silent success": the receipt is OUTCOME_UNKNOWN, the verdict is ACHIEVED from a read by idempotency
// key, and there is exactly one dispatch.
func us06() []ev {
	return []ev{
		{writtenBy: "core-trust", eventType: "DECISION_MADE", objectRef: "dec_synth0006", payload: map[string]any{"decision_outcome": "ALLOW", "decision_id": "dec_synth0006"}},
		{writtenBy: "core-trust", eventType: "PLAN_CREATED", objectRef: "pln_synth0006", payload: map[string]any{"plan_id": "pln_synth0006", "capability_id": capability}},
		{writtenBy: "core-trust", eventType: "GRANT_ISSUED", objectRef: grantID, payload: map[string]any{"grant_id": grantID, "capability_id": capability, "expires_at": "2026-11-02T10:00:00Z"}},
		{writtenBy: "core-execution", eventType: "REVALIDATION_PASSED", objectRef: grantID, payload: map[string]any{"grant_id": grantID}},
		{writtenBy: "core-execution", eventType: "GRANT_SPENT", objectRef: grantID, payload: map[string]any{"grant_id": grantID, "attempt": 1, "idempotency_key": "idk_v1_synth"}},
		{writtenBy: "core-execution", eventType: "DISPATCH_ATTEMPTED", objectRef: grantID, payload: map[string]any{"attempt": 1, "logical_action": capability}},
		{writtenBy: "core-execution", eventType: "RECEIPT", objectRef: grantID, payload: map[string]any{"receipt_id": "rcp_synth0006", "grant_id": grantID, "attempt": 1,
			"logical_action": capability, "execution_state": "OUTCOME_UNKNOWN", "transport_result": "TIMEOUT"}},
		{writtenBy: "core-outcome", eventType: "VERIFICATION_READ", objectRef: "ver_synth0006", payload: map[string]any{"capability_id": capability, "fact_status": "KNOWN", "source": "mock-case"}},
		{writtenBy: "core-outcome", eventType: "VERDICT", objectRef: "ver_synth0006", payload: map[string]any{"capability_id": capability, "verdict": "ACHIEVED", "verdict_id": "ver_synth0006"}},
		{writtenBy: "core-edge", eventType: "EXPLANATION_RENDERED", objectRef: "exp_synth0006", payload: map[string]any{"claims": []any{
			map[string]any{"fact_id": "f1", "text": "Your case was opened once.", "bound_fact_status": "KNOWN", "disclosed_refs": []any{}},
		}}},
	}
}

func build(t *testing.T, w *world, tenantID, corr string) ([]byte, error) {
	t.Helper()
	return reconstruct.Build(ctx(t), w.reader, tenantID, corr)
}

type body struct {
	Chain struct {
		Verified    bool   `json:"verified"`
		HeadEventID string `json:"head_event_id"`
	} `json:"chain"`
	Events []struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
	} `json:"events"`
	Sections []struct {
		Kind  string `json:"kind"`
		Items []struct {
			RecordEventID string         `json:"record_event_id"`
			Basis         string         `json:"basis"`
			Facts         map[string]any `json:"facts"`
		} `json:"items"`
	} `json:"sections"`
	Timeline []struct {
		RecordEventID string `json:"record_event_id"`
		EventType     string `json:"event_type"`
		Text          string `json:"text"`
	} `json:"timeline"`
}

func parse(t *testing.T, raw []byte) body {
	t.Helper()
	var b body
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&b); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	return b
}

func (b body) section(kind string) []struct {
	RecordEventID string         `json:"record_event_id"`
	Basis         string         `json:"basis"`
	Facts         map[string]any `json:"facts"`
} {
	for _, s := range b.Sections {
		if s.Kind == kind {
			return s.Items
		}
	}
	return nil
}

// checkCitations: every item and timeline line cites an event of the body, and every event is in the timeline, in
// chain order (REQ-AUD-003: each element cites its record_event_id; built only from Record events).
func checkCitations(t *testing.T, b body, want []chain.Stored) {
	t.Helper()
	if len(b.Events) != len(want) || len(b.Timeline) != len(want) {
		t.Fatalf("%d events and %d timeline lines, want %d of each", len(b.Events), len(b.Timeline), len(want))
	}
	in := map[string]bool{}
	for i, e := range b.Events {
		if e.EventID != want[i].EventID || b.Timeline[i].RecordEventID != want[i].EventID {
			t.Fatalf("event %d is %s (timeline %s), want %s in chain order", i, e.EventID, b.Timeline[i].RecordEventID, want[i].EventID)
		}
		in[e.EventID] = true
	}
	kinds := []string{"WHO", "REQUEST", "AMBIGUITY", "RESOLUTION", "OBSERVED_STATE", "POLICY", "AUTHORITY", "DECISION", "ACTION",
		"DOWNSTREAM_RESULT", "VERIFICATION", "EXPLANATION"}
	if len(b.Sections) != len(kinds) {
		t.Fatalf("%d sections, want 12", len(b.Sections))
	}
	for i, s := range b.Sections {
		if s.Kind != kinds[i] {
			t.Fatalf("section %d is %s, want %s", i, s.Kind, kinds[i])
		}
		for _, it := range s.Items {
			if !in[it.RecordEventID] {
				t.Fatalf("%s item cites %s, which is not an event of this reconstruction", s.Kind, it.RecordEventID)
			}
		}
	}
	if !b.Chain.Verified || b.Chain.HeadEventID != want[len(want)-1].EventID {
		t.Fatalf("chain %+v, want verified with head %s", b.Chain, want[len(want)-1].EventID)
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if !bytes.Equal(append(got, '\n'), want) {
		t.Fatalf("%s differs from %s", name, path)
	}
}

// US-01 reconstructs from the Record alone (REQ-AUD-003, INV-10): ALLOW, no grant, one candidate with margin 0.41, a
// KNOWN state cited by the explanation. INTENT_PROPOSED (model output) is in the timeline and in no section.
func TestUS01ReconstructsFromTheRecordAlone(t *testing.T) {
	w := newWorld(t)
	st := w.append(t, tenant, "corr_us01", us01()...)
	raw, err := build(t, w, tenant, "corr_us01")
	if err != nil {
		t.Fatal(err)
	}
	b := parse(t, raw)
	checkCitations(t, b, st)
	res := b.section("RESOLUTION")
	if len(res) != 1 || res[0].Facts["candidate_count"] != json.Number("1") || res[0].Facts["margin"] != "0.41" || res[0].Facts["resolved_target"] != target {
		t.Fatalf("RESOLUTION %+v, want one candidate, margin 0.41, target %s", res, target)
	}
	if d := b.section("DECISION"); len(d) != 1 || d[0].Facts["decision_outcome"] != "ALLOW" || d[0].Basis != "RULE" {
		t.Fatalf("DECISION %+v, want one ALLOW on a rule", d)
	}
	if a := b.section("ACTION"); len(a) != 0 {
		t.Fatalf("ACTION %+v, want none: US-01 issues no grant", a)
	}
	for _, e := range b.Events {
		if e.EventType == "GRANT_ISSUED" {
			t.Fatal("US-01 has a GRANT_ISSUED event")
		}
	}
	if x := b.section("EXPLANATION"); len(x) != 1 || x[0].Facts["bound_fact_status"] != "KNOWN" {
		t.Fatalf("EXPLANATION %+v, want one claim bound to a KNOWN fact", x)
	}
	if who := b.section("WHO"); len(who) != 1 || who[0].Basis != "PRINCIPAL_ACTION" {
		t.Fatalf("WHO %+v, want the authenticated session", who)
	}
	for _, s := range b.Sections {
		for _, it := range s.Items {
			if it.RecordEventID == st[2].EventID {
				t.Fatalf("the model's INTENT_PROPOSED is cited as a fact in %s", s.Kind)
			}
		}
	}
	golden(t, "us01", raw)
}

// US-06 reconstructs from the Record alone: one dispatch, its receipt OUTCOME_UNKNOWN on a VALID grant, then a
// verdict ACHIEVED from a read (INV-07: the timeout stays unknown until Outcome says otherwise).
func TestUS06ReconstructsFromTheRecordAlone(t *testing.T) {
	w := newWorld(t)
	st := w.append(t, tenant, "corr_us06", us06()...)
	raw, err := build(t, w, tenant, "corr_us06")
	if err != nil {
		t.Fatal(err)
	}
	b := parse(t, raw)
	checkCitations(t, b, st)
	act := b.section("ACTION")
	if len(act) != 1 {
		t.Fatalf("ACTION has %d items, want exactly one dispatch", len(act))
	}
	f := act[0].Facts
	if f["execution_state"] != "OUTCOME_UNKNOWN" || f["grant_state"] != "VALID" || f["capability_id"] != capability || f["attempt"] != json.Number("1") {
		t.Fatalf("ACTION facts %v, want OUTCOME_UNKNOWN on a VALID grant, attempt 1", f)
	}
	if v := b.section("VERIFICATION"); len(v) != 1 || v[0].Facts["verdict"] != "ACHIEVED" || v[0].Basis != "EVIDENCE" {
		t.Fatalf("VERIFICATION %+v, want one ACHIEVED on evidence", v)
	}
	if d := b.section("DOWNSTREAM_RESULT"); len(d) != 1 || d[0].RecordEventID != st[7].EventID {
		t.Fatalf("DOWNSTREAM_RESULT %+v, want the verification read", d)
	}
	golden(t, "us06", raw)
}

// REQ-AUD-003 acceptance, "reproducible": the same Record gives the same bytes, and appends to other correlations or
// tenants afterwards do not change a finished correlation's body.
func TestReconstructionIsReproducible(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_us06", us06()...)
	first, err := build(t, w, tenant, "corr_us06")
	if err != nil {
		t.Fatal(err)
	}
	w.append(t, tenant, "corr_us01", us01()...)
	w.append(t, other, "corr_us06", us06()...)
	second, err := build(t, w, tenant, "corr_us06")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the reconstruction changed after unrelated appends")
	}
}

// INV-11, REQ-AUD-003 failure behaviour: a broken chain is refused, never reconstructed. Each kind of tampering, in the
// requested correlation or in another one of the same tenant, makes Build return ErrIntegrity and serve nothing.
func TestABrokenChainIsRefusedNotReconstructed(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"an edited hashed body", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"grt_forged01"') WHERE seq = 3`},
		{"an edited body and its column", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"grt_forged01"'), object_ref = 'grt_forged01' WHERE seq = 3`},
		{"a broken prior hash", `UPDATE record_event SET prev_hash = 'sha256:` + strings.Repeat("1", 64) + `' WHERE seq = 4`},
		{"a deleted event", `DELETE FROM record_event WHERE seq = 5`},
		{"an edited payload", `UPDATE record_event SET payload = '{"verdict": "ACHIEVED"}' WHERE seq = 7`},
		{"an edited event type column", `UPDATE record_event SET event_type = 'VERDICT' WHERE seq = 6`},
		{"a break in another correlation of the tenant", `UPDATE record_event SET payload = '{"decision_outcome": "REJECT"}' WHERE seq = 19`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.append(t, tenant, "corr_us06", us06()...)
			w.append(t, tenant, "corr_us01", us01()...)
			if _, err := build(t, w, tenant, "corr_us06"); err != nil {
				t.Fatalf("a clean chain does not reconstruct: %v", err)
			}
			tamper(t, w, c.sql)
			raw, err := build(t, w, tenant, "corr_us06")
			var be *chain.BreakError
			if !errors.Is(err, reconstruct.ErrIntegrity) || !errors.As(err, &be) || raw != nil {
				t.Fatalf("tampering (%s): got %v and %d bytes, want ErrIntegrity with the break and nothing served", c.name, err, len(raw))
			}
		})
	}
}

// tamper edits the Record as an administrator who bypasses the triggers, the attack the chain exists to detect.
func tamper(t *testing.T, w *world, sql string) {
	t.Helper()
	conn, err := w.admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	tag, err := conn.Exec(ctx(t), sql)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("tamper %q: %v (%d rows)", sql, err, tag.RowsAffected())
	}
}

// Tenant isolation (INV-12, REQ-SEC-004): the tenant is the caller's, and another tenant's correlation is not found.
func TestAnotherTenantsCorrelationIsNotFound(t *testing.T) {
	w := newWorld(t)
	w.append(t, other, "corr_us06", us06()...)
	if _, err := build(t, w, tenant, "corr_us06"); !errors.Is(err, reconstruct.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if _, err := build(t, w, tenant, "corr_none01"); !errors.Is(err, reconstruct.ErrNotFound) {
		t.Fatalf("an unknown correlation: got %v, want ErrNotFound", err)
	}
}

// Migration 0003 (approved escalation E1): only record_read can reconstruct. The same call as execution_rw is refused
// by the database, and the refusal is not mistaken for a broken chain.
func TestOnlyRecordReadCanReconstruct(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_us06", us06()...)
	_, err := reconstruct.Build(ctx(t), w.writer, tenant, "corr_us06")
	if err == nil || errors.Is(err, reconstruct.ErrIntegrity) || !strings.Contains(err.Error(), "42501") {
		t.Fatalf("execution_rw: got %v, want a permission refusal (42501)", err)
	}
}

// A receipt whose capability is not a pack ID cannot be served under the contract, so the reconstruction is refused
// rather than served partially. Regression case for the receipts the ledger writes today (logical_action
// "ESCALATE_CASE", not a pack ID; reported in REC-03's pull request).
func TestAReceiptWithoutAPackCapabilityIsRefused(t *testing.T) {
	w := newWorld(t)
	events := us06()
	events[6].payload["logical_action"] = "ESCALATE_CASE"
	w.append(t, tenant, "corr_us06", events...)
	if _, err := build(t, w, tenant, "corr_us06"); !errors.Is(err, reconstruct.ErrUnprojectable) {
		t.Fatalf("got %v, want ErrUnprojectable", err)
	}
}

func TestBuildNeedsAContextDeadline(t *testing.T) {
	w := newWorld(t)
	if _, err := reconstruct.Build(context.Background(), w.reader, tenant, "corr_us06"); err == nil {
		t.Fatal("Build without a deadline did not fail")
	}
}

// --- The handler (approved escalation E2: library, handler, default-refuse authoriser; not wired into cmd/core) ---

type fixedAuth struct{ c Caller }

type Caller = reconstruct.Caller

func (f fixedAuth) Authenticate(*http.Request) (reconstruct.Caller, error) { return f.c, nil }

func serve(t *testing.T, h *reconstruct.Handler, corr string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(reconstruct.Route, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/reconstructions/"+corr, nil))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %q", rec.Body.String())
	}
	return rec, out
}

func errorCode(t *testing.T, out map[string]any) (string, bool) {
	t.Helper()
	e, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope: %v", out)
	}
	if _, ok := out["events"]; ok {
		t.Fatal("an error answer carries events")
	}
	return e["code"].(string), e["retryable"].(bool)
}

// The default authoriser refuses everything, and the database is never touched: DB is nil here, so any read would
// panic.
func TestTheDefaultAuthoriserRefusesEveryRequest(t *testing.T) {
	var log bytes.Buffer
	h := &reconstruct.Handler{IDs: ids.New(clock.NewManual(time.Unix(0, 0)), zeros{}), Log: slog.New(slog.NewJSONHandler(&log, nil))}
	rec, out := serve(t, h, "corr_us06")
	if code, retry := errorCode(t, out); rec.Code != http.StatusUnauthorized || code != "UNAUTHENTICATED" || retry {
		t.Fatalf("got %d %s, want 401 UNAUTHENTICATED, not retryable", rec.Code, code)
	}
	if !strings.Contains(log.String(), `"msg":"RECORD_ACCESSED"`) || !strings.Contains(log.String(), `"outcome":"UNAUTHENTICATED"`) {
		t.Fatalf("no RECORD_ACCESSED line for the refusal: %s", log.String())
	}
}

func TestTheHandlerServesOnlyItsRolesAndAnswersWithContractCodes(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_us06", us06()...)
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	audit := Caller{TenantID: tenant, Principal: "auditor:usr_synth_900", Roles: []string{"audit"}}

	for _, c := range []struct {
		name   string
		caller Caller
		corr   string
		status int
		code   string
	}{
		{"an operator with neither the ops nor the audit role", Caller{TenantID: tenant, Principal: "operator:usr_synth_1", Roles: []string{"viewer"}}, "corr_us06", 404, "NOT_FOUND_OR_NOT_AUTHORIZED"},
		{"an auditor of another tenant", Caller{TenantID: other, Principal: "auditor:usr_synth_2", Roles: []string{"audit"}}, "corr_us06", 404, "NOT_FOUND_OR_NOT_AUTHORIZED"},
		{"a malformed correlation ID", audit, "not-a-correlation", 400, "INPUT_INVALID"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec, out := serve(t, &reconstruct.Handler{DB: w.reader, Auth: fixedAuth{c.caller}, IDs: gen}, c.corr)
			if code, _ := errorCode(t, out); rec.Code != c.status || code != c.code {
				t.Fatalf("got %d %s, want %d %s", rec.Code, code, c.status, c.code)
			}
		})
	}

	t.Run("an auditor of the tenant", func(t *testing.T) {
		rec, _ := serve(t, &reconstruct.Handler{DB: w.reader, Auth: fixedAuth{audit}, IDs: gen}, "corr_us06")
		want, err := build(t, w, tenant, "corr_us06")
		if err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), want) || rec.Header().Get("Noema-Correlation-Id") != "corr_us06" {
			t.Fatalf("got %d, header %q; want 200 with the built body", rec.Code, rec.Header().Get("Noema-Correlation-Id"))
		}
	})

	t.Run("a broken chain answers 409 and serves nothing", func(t *testing.T) {
		tamper(t, w, `UPDATE record_event SET payload = '{"verdict": "ACHIEVED"}' WHERE seq = 7`)
		rec, out := serve(t, &reconstruct.Handler{DB: w.reader, Auth: fixedAuth{audit}, IDs: gen}, "corr_us06")
		if code, retry := errorCode(t, out); rec.Code != http.StatusConflict || code != "RECORD_INTEGRITY_FAILURE" || retry {
			t.Fatalf("got %d %s, want 409 RECORD_INTEGRITY_FAILURE, not retryable", rec.Code, code)
		}
	})

	t.Run("a database the reader cannot reach is DEPENDENCY_UNAVAILABLE", func(t *testing.T) {
		closed, err := db.OpenAsRole(ctx(t), w.dsn, "record_read")
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		rec, out := serve(t, &reconstruct.Handler{DB: closed, Auth: fixedAuth{audit}, IDs: gen}, "corr_us06")
		if code, retry := errorCode(t, out); rec.Code != http.StatusServiceUnavailable || code != "DEPENDENCY_UNAVAILABLE" || !retry {
			t.Fatalf("got %d %s retryable=%v, want 503 DEPENDENCY_UNAVAILABLE, retryable", rec.Code, code, retry)
		}
	})
}

// REQ-AUD-003 expected behaviour: P95 < 1 s for a case with 200 events [REC]. Measured on a real Record of 200 events
// in the requested correlation, after 200 events of another correlation (the whole tenant chain is verified).
func TestP95ForATwoHundredEventCaseIsUnderOneSecond(t *testing.T) {
	w := newWorld(t)
	filler := make([]ev, 0, 200)
	for i := 0; i < 200; i++ {
		filler = append(filler, ev{writtenBy: "core-evidence", eventType: "EVIDENCE_READ", objectRef: fmt.Sprintf("evr_synth%04d", i),
			payload: map[string]any{"fact_status": "KNOWN", "source": "mock-ledger"}})
	}
	w.append(t, tenant, "corr_other01", filler...)
	w.append(t, tenant, "corr_big0001", filler...)
	const runs = 20
	wall := clock.System{} // the measured duration is real time; platform/clock is the only source of it
	took := make([]time.Duration, 0, runs)
	for i := 0; i < runs; i++ {
		start := wall.Now()
		if _, err := build(t, w, tenant, "corr_big0001"); err != nil {
			t.Fatal(err)
		}
		took = append(took, wall.Now().Sub(start))
	}
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	p95 := took[runs*95/100-1]
	t.Logf("P95 over %d builds of a 200-event case in a 400-event chain: %v (budget 1s)", runs, p95)
	if p95 >= time.Second {
		t.Fatalf("P95 %v, budget 1s", p95)
	}
}

// Architect decision 1 of 1 Nov 2026 (REQ-AUD-003 Security): the ops and audit roles are served; customers and
// approvers never are, even if a session wrongly carried an allowed role (INV-12: the principal kind is the session's).
func TestOpsAndAuditAreServedAndCustomersAndApproversNeverAre(t *testing.T) {
	w := newWorld(t)
	w.append(t, tenant, "corr_us06", us06()...)
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	for _, c := range []struct {
		name   string
		caller Caller
		status int
	}{
		{"ops", Caller{TenantID: tenant, Principal: "operator:usr_synth_1", Roles: []string{"ops"}}, 200},
		{"audit", Caller{TenantID: tenant, Principal: "auditor:usr_synth_2", Roles: []string{"audit"}}, 200},
		{"a customer, even with the audit role", Caller{TenantID: tenant, Principal: "customer:usr_synth_3", Roles: []string{"audit"}}, 404},
		{"an approver, even with the ops role", Caller{TenantID: tenant, Principal: "approver:usr_synth_4", Roles: []string{"ops"}}, 404},
		{"a customer role", Caller{TenantID: tenant, Principal: "operator:usr_synth_5", Roles: []string{"customer"}}, 404},
		{"no principal", Caller{TenantID: tenant, Roles: []string{"audit"}}, 404},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec, out := serve(t, &reconstruct.Handler{DB: w.reader, Auth: fixedAuth{c.caller}, IDs: gen}, "corr_us06")
			if rec.Code != c.status {
				t.Fatalf("got %d, want %d", rec.Code, c.status)
			}
			if c.status != 200 {
				if code, _ := errorCode(t, out); code != "NOT_FOUND_OR_NOT_AUTHORIZED" {
					t.Fatalf("code %s, want NOT_FOUND_OR_NOT_AUTHORIZED", code)
				}
			}
		})
	}
}

// REQ-AUD-003 Security ("Masked"), AGENTS.md section 7 (mask to the last four digits): a run of five or more digits in
// a served free-text or scalar fact keeps only its last four. References and IDs, which the contract patterns check
// and which the reader matches on, are not facts of this kind and are served as written.
func TestServedFactsAreMaskedToTheLastFourDigits(t *testing.T) {
	w := newWorld(t)
	events := us01()
	events[9].payload = map[string]any{"claims": []any{
		map[string]any{"fact_id": "f1", "text": "Sent from 0123456789 to 9876543210 on day 12.", "bound_fact_status": "KNOWN", "disclosed_refs": []any{target}},
	}}
	events[5].payload["source"] = "branch 4402211"
	w.append(t, tenant, "corr_us01", events...)
	raw, err := build(t, w, tenant, "corr_us01")
	if err != nil {
		t.Fatal(err)
	}
	b := parse(t, raw)
	if got := b.section("EXPLANATION")[0].Facts["text"]; got != "Sent from ******6789 to ******3210 on day 12." {
		t.Fatalf("claim text %q, want each long digit run masked to its last four", got)
	}
	if got := b.section("OBSERVED_STATE")[0].Facts["source"]; got != "branch ***2211" {
		t.Fatalf("source %q, want masked", got)
	}
	if got := b.section("RESOLUTION")[0].Facts["resolved_target"]; got != target {
		t.Fatalf("resolved_target %q, want the reference unchanged", got)
	}
	for _, digits := range []string{"0123456789", "9876543210", "4402211"} {
		if bytes.Contains(raw, []byte(digits)) {
			t.Fatalf("the body still contains %s", digits)
		}
	}
}

// REQ-SEC-004, INV-12 (REC-03, work order of 3 Nov 2026, DEBT-04): the handler takes the tenant from the authenticated caller and
// from nothing else. Two tenants hold the same correlation ID; each caller sees only its own tenant's events, whatever the request
// says about the tenant. This is the protection the handler gives. The database does not give it yet (DEBT-04, below).
func TestTheHandlerServesOnlyTheCallersTenantWhateverTheRequestSays(t *testing.T) {
	w := newWorld(t)
	mine := w.append(t, tenant, "corr_shared01", us06()...)
	theirs := w.append(t, other, "corr_shared01", us01()...)
	gen := ids.New(clock.NewManual(time.Unix(0, 0)), zeros{})
	ids := func(st []chain.Stored) map[string]bool {
		m := map[string]bool{}
		for _, s := range st {
			m[s.EventID] = true
		}
		return m
	}
	for _, c := range []struct {
		name           string
		caller         Caller
		want, mustMiss []chain.Stored
	}{
		{"the auditor of the first tenant", Caller{TenantID: tenant, Principal: "auditor:usr_synth_a", Roles: []string{"audit"}}, mine, theirs},
		{"the auditor of the second tenant", Caller{TenantID: other, Principal: "auditor:usr_synth_b", Roles: []string{"audit"}}, theirs, mine},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, ask := range []struct{ query, header string }{
				{"", ""},
				{"?tenant_id=" + other + "&tenant=" + other, ""},
				{"?tenant_id=" + tenant, "Noema-Tenant-Id"},
			} {
				mux := http.NewServeMux()
				mux.Handle(reconstruct.Route, &reconstruct.Handler{DB: w.reader, Auth: fixedAuth{c.caller}, IDs: gen})
				req := httptest.NewRequest(http.MethodGet, "/v1/reconstructions/corr_shared01"+ask.query, nil)
				for _, h := range []string{"Noema-Tenant-Id", "X-Tenant-Id", "Tenant"} {
					req.Header.Set(h, map[bool]string{true: tenant, false: other}[c.caller.TenantID == other])
				}
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != 200 {
					t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
				body := parse(t, rec.Body.Bytes())
				got := map[string]bool{}
				for _, e := range body.Events {
					got[e.EventID] = true
				}
				if !reflect.DeepEqual(got, ids(c.want)) {
					t.Fatalf("the caller of %s got events %v, want exactly %v", c.caller.TenantID, got, ids(c.want))
				}
				for id := range ids(c.mustMiss) {
					if got[id] {
						t.Fatalf("the caller of %s was served %s, which belongs to the other tenant", c.caller.TenantID, id)
					}
				}
			}
		})
	}
}

// DEBT-04 (accepted by the Architect on 3 Nov 2026, option B for this pull request): record_read_chain, record_lock_head and
// record_insert trust a caller-supplied tenant ID, so a connection that holds the record_read role can read any tenant by naming
// it. Option A, a login per tenant plus a mapping the database checks against session_user, lands before the endpoint is wired
// into cmd/core (EDG-01). When it lands, delete the Skip: the body below is the assertion that must then hold.
func TestRecordReadChainDeniesAnotherTenantAtTheDatabase(t *testing.T) {
	t.Skip("DEBT-04: record_read_chain trusts a caller-supplied tenant; option A (a login per tenant and a database-checked mapping) lands before the endpoint is wired into cmd/core")
	w := newWorld(t)
	w.append(t, other, "corr_us06", us06()...)
	var n int
	err := w.reader.QueryRow(ctx(t), `SELECT count(*) FROM record_read_chain($1)`, other).Scan(&n)
	if err == nil {
		t.Fatalf("a connection that holds record_read for %s read %d events of %s", tenant, n, other)
	}
}

// A transaction whose reads fail and whose rollback is observed: the cleanup of a request must be bounded.
type observedTx struct {
	pgx.Tx
	called      bool
	errAtCall   error     // the context's state at the moment Rollback was called
	deadline    time.Time // and its deadline then
	hasDeadline bool
}

func (o *observedTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("the database is not answering")
}

func (o *observedTx) Rollback(ctx context.Context) error {
	o.called, o.errAtCall = true, ctx.Err()
	o.deadline, o.hasDeadline = ctx.Deadline()
	return nil
}

type observedDB struct{ tx *observedTx }

func (d observedDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return d.tx, nil }

// Review of noema#107 (major): the rollback of a request must neither depend on a deadline that has already passed nor run
// without a limit. A stalled database must not hold the request after the answer has been decided.
func TestTheRollbackAfterAFailedReadIsBoundedByItsOwnDeadline(t *testing.T) {
	tx := &observedTx{}
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	cancel() // the request's own context is already over
	if _, err := reconstruct.Build(parent, observedDB{tx}, tenant, "corr_us06"); err == nil {
		t.Fatal("a read that fails was reported as success")
	}
	if !tx.called {
		t.Fatal("the transaction was not rolled back")
	}
	if tx.errAtCall != nil {
		t.Fatalf("the rollback was given a context that was already over (%v): it could never run", tx.errAtCall)
	}
	if !tx.hasDeadline {
		t.Fatal("the rollback has no deadline: a stalled database would hold the request for ever")
	}
	if left := tx.deadline.Sub(clock.System{}.Now()); left > 10*time.Second {
		t.Fatalf("the rollback may run for %v; it must be bounded to seconds", left)
	}
}
