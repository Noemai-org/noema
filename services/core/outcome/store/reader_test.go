package store_test

// LANE-5 slice 4 (A675, A692; interim, relayed; REQ-VER-001, REQ-SEC-004; INV-04, INV-12, AGENT-13): the read-only Record reader on the tenant's outcome login. It returns what Execution wrote
// for one grant, with the tenant in the WHERE and row security forced; a row of another tenant, or none, is request.ErrNotFound; it writes nothing. These tests need the pgsodium database and run
// in CI only. All rows are SYNTHETIC.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/outcome/request"
	"github.com/Noemai-org/noema/services/core/outcome/store"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

const (
	rdGrant   = "grt_rd01"
	rdReceipt = "rcp_rd01"
	rdCap     = "fin.cap.create_escalation_case"
	rdPlan    = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// seedFacts plants a decision, a grant that names a capability and a plan hash, its spend and a signed receipt carrying stepParams (nil: the member is absent).
func (w *world) seedFacts(t *testing.T, tenantID, grant, receipt string, stepParams map[string]any) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T') ON CONFLICT DO NOTHING`,
		`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value) VALUES ('dec_' || $2::text, $1, jsonb_build_object('decision_id', 'dec_' || $2::text, 'tenant_id', $1::text, 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k', 'value', 's')) || ` + testpg.BoundFieldsSQL + `, 'ALLOW', '` + testpg.FixtureIntentHash + `', '[]', 'k', 's')`,
		`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		  VALUES ($2, $1, 'dec_' || $2::text, 'n_' || $2, jsonb_build_object('correlation_id', 'corr_abc123', 'principal_id', 'usr_100', 'decision_id', 'dec_' || $2::text, 'tenant_id', $1::text,
		  'capability_id', '` + rdCap + `', 'plan_hash', '` + rdPlan + `') || ` + testpg.GrantBoundSQL + `, 'p', '1', now() + interval '1 hour', 'k', 's')`,
		`INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($2, $1, 'n_' || $2, 'idk_v1_' || $2, 1, now())`,
	} {
		if _, err := w.admin.Exec(ctx(t), q, tenantID, grant); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	doc := map[string]any{"schema_version": "1.0", "correlation_id": "corr_abc123", "receipt_id": receipt, "grant_id": grant, "tenant_id": tenantID,
		"idempotency_key": "idk_v1_" + grant, "logical_action": rdCap, "attempt": int64(1), "dispatched_at": verifiedAt.UTC().Format(time.RFC3339Nano),
		"transport_result": "HTTP_201", "execution_state": "ACKNOWLEDGED", "external_ack": "CASE-55012"}
	if stepParams != nil {
		doc["step_params"] = stepParams
	}
	signed, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	sig := hex.EncodeToString(ed25519.Sign(w.key.Priv, append([]byte("noema/receipt/v1\n"), signed...)))
	withSig := map[string]any{"sig": map[string]any{"key_id": w.key.ID, "value": sig}}
	for k, v := range doc {
		withSig[k] = v
	}
	body, err := crypto.CanonicalJSON(withSig)
	if err != nil {
		t.Fatal(err)
	}
	testpgMap(t, w, tenantID)
	var inserted bool
	if err := w.admin.QueryRow(ctx(t), `SELECT receipt_insert($1, $2, $3, 1, $4, $5, 'ACKNOWLEDGED', 'HTTP_201', 'CASE-55012', $6, $7::jsonb, $8, $9, $10, $6)`,
		receipt, tenantID, grant, "idk_v1_"+grant, rdCap, verifiedAt, string(body), w.key.ID, sig, signed).Scan(&inserted); err != nil || !inserted {
		t.Fatalf("receipt_insert: inserted=%v err=%v", inserted, err)
	}
}

// testpgMap maps the admin session to the tenant, as sigfixture does before receipt_insert (a fixture row of the test database, not a back door).
func testpgMap(t *testing.T, w *world, tenantID string) {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant_login (login_role, power, tenant_id) VALUES (session_user, 'execution', $1) ON CONFLICT (login_role) DO UPDATE SET tenant_id = EXCLUDED.tenant_id`, tenantID); err != nil {
		t.Fatalf("map the session: %v", err)
	}
}

// REQ-VER-001: the reader returns what Execution wrote, step_params as canonical text.
func TestTheReaderReturnsWhatExecutionWroteForTheGrant(t *testing.T) {
	w := newWorld(t)
	w.seedFacts(t, tenant, rdGrant, rdReceipt, map[string]any{"transaction_ref": "TX1", "priority_rank": int64(2), "urgent": true, "note": nil})
	f, err := store.Reader{Pool: w.outcome(t)}.Load(ctx(t), tenant, rdGrant)
	if err != nil {
		t.Fatal(err)
	}
	r := f.Receipt
	if r.ReceiptID != rdReceipt || r.TenantID != tenant || r.GrantID != rdGrant || r.PrincipalID != "usr_100" || r.State != "ACKNOWLEDGED" || r.LogicalAction != rdCap || r.Attempt != 1 {
		t.Errorf("receipt facts wrong: %+v", r)
	}
	if r.ExternalAck == nil || *r.ExternalAck != "CASE-55012" || r.RawStatus != "HTTP_201" || len(r.Document) == 0 || r.SigKeyID != w.key.ID || r.SigValue == "" || r.CorrelationID != "corr_abc123" {
		t.Errorf("receipt row members wrong: %+v", r)
	}
	if f.CapabilityID != rdCap || f.PlanHash != rdPlan || f.IntentID != "int_synth01" || f.DecisionIntentHash != testpg.FixtureIntentHash || f.GrantIntentHash != testpg.FixtureIntentHash {
		t.Errorf("grant and decision facts wrong: %+v", f)
	}
	want := map[string]string{"transaction_ref": "TX1", "priority_rank": "2", "urgent": "true", "note": ""}
	if len(f.StepParams) != len(want) {
		t.Errorf("step_params = %v, want %v", f.StepParams, want)
	}
	for k, v := range want {
		if got, ok := f.StepParams[k]; !ok || got != v {
			t.Errorf("step_params[%s] = %q (present %v), want %q", k, got, ok, v)
		}
	}
}

// A receipt written before A692 has no step_params: the reader returns none and no error (the builder refuses a bound param that is absent).
func TestAReceiptWithoutStepParamsGivesNone(t *testing.T) {
	w := newWorld(t)
	w.seedFacts(t, tenant, rdGrant, rdReceipt, nil)
	f, err := store.Reader{Pool: w.outcome(t)}.Load(ctx(t), tenant, rdGrant)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.StepParams) != 0 {
		t.Errorf("step_params = %v, want none", f.StepParams)
	}
}

// A step_params value that is an object or an array is not what the contract allows: an error, never a guessed text.
func TestAStepParamThatIsNotAScalarIsAnError(t *testing.T) {
	w := newWorld(t)
	w.seedFacts(t, tenant, rdGrant, rdReceipt, map[string]any{"transaction_ref": map[string]any{"x": "y"}})
	if f, err := (store.Reader{Pool: w.outcome(t)}).Load(ctx(t), tenant, rdGrant); err == nil {
		t.Fatalf("a nested step_param was read as %+v", f)
	}
}

// INV-12, REQ-SEC-004: a grant that does not exist, or that belongs to another tenant, is not found. The tenant is in the WHERE and row security is forced, so neither the tenant
// argument nor the login can reach another tenant's row.
func TestAnotherTenantsGrantIsNotFound(t *testing.T) {
	w := newWorld(t)
	w.seedFacts(t, tenant, rdGrant, rdReceipt, map[string]any{"transaction_ref": "TX1"})
	w.seedFacts(t, other, "grt_rd02", "rcp_rd02", map[string]any{"transaction_ref": "TX2"})
	rd := store.Reader{Pool: w.outcome(t)} // the login mapped to `tenant`
	for name, c := range map[string][2]string{
		"no such grant":                          {tenant, "grt_none"},
		"another tenant's grant, own tenant":     {tenant, "grt_rd02"},
		"another tenant's grant, its own tenant": {other, "grt_rd02"},
	} {
		if f, err := rd.Load(ctx(t), c[0], c[1]); !errors.Is(err, request.ErrNotFound) {
			t.Errorf("%s: got (%+v, %v), want ErrNotFound", name, f, err)
		}
	}
}

// A read without a deadline is refused before it runs (a context deadline on every call).
func TestAReadWithoutADeadlineIsRefused(t *testing.T) {
	w := newWorld(t)
	w.seedFacts(t, tenant, rdGrant, rdReceipt, nil)
	if _, err := (store.Reader{Pool: w.outcome(t)}).Load(context.Background(), tenant, rdGrant); err == nil {
		t.Fatal("a read with no deadline ran")
	}
}

// A675: read-only. The reader's source holds no statement that writes and no call to a function.
func TestTheReaderSourceHoldsNoWrite(t *testing.T) {
	src, err := os.ReadFile("reader.go")
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`(?i)\b(insert|update|delete|truncate|alter|drop|create|grant)\b[^"\n]*\b(into|set|from|table|role|function|on)\b|\.Exec\(|SendBatch|CopyFrom|BeginTx`).Find(src); m != nil {
		t.Errorf("reader.go holds something that writes or opens a transaction: %q", m)
	}
	if !regexp.MustCompile(`tenant_id\s*=\s*\$1`).Match(src) {
		t.Error("reader.go does not put the tenant in the WHERE")
	}
}
