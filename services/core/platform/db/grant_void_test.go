package db_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PERD v1.18 section 38.2, REQ-EXE-004, INV-27, US-19 (DEC-2026-10-28-spent-grant-void): the grant lifecycle is
// ISSUED -> SPENT, VOID or EXPIRED, and SPENT -> VOID only for a confirmed REJECTED_STALE_VERSION refusal. These tests run
// raw statements against the real trigger, as the roles that exist in production.
//
// The refusal is confirmed from the signed receipt document (execution-receipt.v1: tenant_id, grant_id, attempt,
// idempotency_key, execution_state, transport_result, external_ack, sig), cross-checked against the row's own columns and
// against grant_spend. raw_status is a diagnostic and may only agree with the signed transport_result.

// validSig has the shape of an HMAC-SHA-256 as the core writes it: 64 lower-case hex characters (crypto.Sign).
var validSig = strings.Repeat("ab", 32)

const (
	conflictClass = "VERSION_CONFLICT_409_412"
	// conflictRaw is what the Ledger stores for the refusal the real mock-case gives: the adapter class, then the raw
	// code "409:VERSION_MISMATCH" (services/core/adapters/synthetic_tushe/README.md).
	conflictRaw = conflictClass + ":409:VERSION_MISMATCH"
	voidGrant1  = `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_1'`
)

type absentMember struct{}

var (
	absent   = absentMember{}
	jsonNull = json.RawMessage("null")
)

// spentGrant makes a grant SPENT, as E8 does: one grant_spend row (attempt 1), then the status change.
func spentGrant(t *testing.T, pool *pgxpool.Pool, grant, nonce string) {
	t.Helper()
	if grant != "grt_1" {
		if _, err := pool.Exec(ctx(t), `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
			VALUES ($1, 'ten_a', 'dec_1', $2, '{}', 'pol_1', '1', now() + interval '1 hour', 'k1', 'aa')`, grant, nonce); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, 'ten_a', $2, 'idk_v1_'||$1, 1, now())`, grant, nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx(t), `UPDATE decision_grant SET status = 'SPENT' WHERE grant_id = $1`, grant); err != nil {
		t.Fatal(err)
	}
}

// rcp is one execution_receipt row: its columns, and overrides of its signed document. The document follows the columns
// as the Ledger writes it (controller.Receipt.Document), so an override is a forgery or a mismatch.
type rcp struct {
	grant          string
	attempt        int
	state          string // execution_state column
	raw            string // raw_status column
	ack            string // external_ack column; "" is stored as the empty string, as the Ledger stores it
	tenant         string
	key            string
	sigKey, sigVal string
	doc            map[string]any // overrides of the signed document; absent removes a member
}

// refusal is a well-formed, confirmed refusal for a grant.
func refusal(grant string) rcp {
	return rcp{grant: grant, attempt: 1, state: "REJECTED_STALE_VERSION", raw: conflictRaw, tenant: "ten_a", key: "idk_v1_" + grant, sigKey: "k1", sigVal: validSig}
}

func (r rcp) document() map[string]any {
	class, _, _ := strings.Cut(r.raw, ":")
	doc := map[string]any{"schema_version": "1.0", "tenant_id": r.tenant, "correlation_id": "corr_1", "receipt_id": fmt.Sprintf("rcp_%s_%d", r.grant, r.attempt),
		"grant_id": r.grant, "idempotency_key": r.key, "logical_action": "ROUTE_CASE", "attempt": r.attempt, "dispatched_at": "2026-10-28T09:00:00Z",
		"transport_result": class, "execution_state": r.state, "external_ack": jsonNull, "sig": map[string]any{"key_id": r.sigKey, "value": r.sigVal}}
	if r.ack != "" {
		doc["external_ack"] = r.ack
	}
	for k, v := range r.doc {
		if v == absent {
			delete(doc, k)
		} else {
			doc[k] = v
		}
	}
	return doc
}

func insertReceipt(t *testing.T, pool *pgxpool.Pool, r rcp) {
	t.Helper()
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T') ON CONFLICT DO NOTHING`, r.tenant); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(r.document())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx(t), `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, created_at)
		VALUES ($1, $2, $3, $4, $5, 'ROUTE_CASE', $6, $7, $8, now(), $9::jsonb, $10, $11, now())`,
		fmt.Sprintf("rcp_%s_%d", r.grant, r.attempt), r.tenant, r.grant, r.attempt, r.key, r.state, r.raw, r.ack, string(body), r.sigKey, r.sigVal); err != nil {
		t.Fatal(err)
	}
}

// receipt inserts a well-formed receipt of the given state, raw status and acknowledgement for a grant's first attempt.
func receipt(t *testing.T, pool *pgxpool.Pool, grant string, attempt int, state, raw, ack string) {
	t.Helper()
	r := refusal(grant)
	r.attempt, r.state, r.raw, r.ack = attempt, state, raw, ack
	insertReceipt(t, pool, r)
}

func status(t *testing.T, pool *pgxpool.Pool, grant string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx(t), `SELECT status FROM decision_grant WHERE grant_id = $1`, grant).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// The permitted transition: SPENT -> VOID with the grant's only receipt a confirmed REJECTED_STALE_VERSION refusal.
func TestASpentGrantBecomesVoidOnAConfirmedStaleVersionRefusal(t *testing.T) {
	cases := map[string]rcp{
		"the refusal the real mock-case gives (409 VERSION_MISMATCH)": refusal("grt_1"),
		// raw_status is a diagnostic: the decision is the signed transport_result, so a class with no code is as good.
		"the class alone as the raw status": func() rcp { r := refusal("grt_1"); r.raw = conflictClass; return r }(),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			pool := migrated(t)
			seed(t, pool)
			spentGrant(t, pool, "grt_1", "n1")
			insertReceipt(t, pool, r)
			if err := try(t, pool, "execution_rw", voidGrant1); err != nil {
				t.Fatalf("execution_rw voids a grant after a confirmed refusal: %v", err)
			}
			// try rolls back; the committed update goes through the superuser, which the trigger binds as well.
			if _, err := pool.Exec(ctx(t), voidGrant1); err != nil {
				t.Fatalf("committed SPENT -> VOID: %v", err)
			}
			if got := status(t, pool, "grt_1"); got != "VOID" {
				t.Fatalf("status %q, want VOID", got)
			}
		})
	}
}

type refusedCase struct {
	name string
	set  func(t *testing.T, pool *pgxpool.Pool)
}

// one receipt, with a change to its columns or to its signed document
func one(name string, change func(r *rcp)) refusedCase {
	return refusedCase{name, func(t *testing.T, p *pgxpool.Pool) {
		r := refusal("grt_1")
		change(&r)
		insertReceipt(t, p, r)
	}}
}

func doc(name string, member string, value any) refusedCase {
	return one(name, func(r *rcp) { r.doc = map[string]any{member: value} })
}

// one signature on both sides: the document's sig member and the row's columns carry the same key and value
func sigShape(name, key, value string) refusedCase {
	return one(name, func(r *rcp) { r.sigKey, r.sigVal = key, value })
}

func runRefused(t *testing.T, cases []refusedCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := migrated(t)
			seed(t, pool)
			spentGrant(t, pool, "grt_1", "n1")
			c.set(t, pool)
			wantCode(t, try(t, pool, "execution_rw", voidGrant1), violation, "SPENT -> VOID")
			wantCode(t, try(t, pool, "postgres", voidGrant1), violation, "SPENT -> VOID as the superuser")
			if got := status(t, pool, "grt_1"); got != "SPENT" {
				t.Fatalf("status %q, want SPENT", got)
			}
		})
	}
}

// No receipt, or a receipt of any other state, never voids a spent grant.
func TestASpentGrantStaysSpentWithoutAConfirmedStaleVersionRefusal(t *testing.T) {
	other := func(state, raw, ack string) refusedCase {
		return refusedCase{"a " + state + " receipt (" + raw + ")", func(t *testing.T, p *pgxpool.Pool) { receipt(t, p, "grt_1", 1, state, raw, ack) }}
	}
	runRefused(t, []refusedCase{
		{"no receipt", func(*testing.T, *pgxpool.Pool) {}},
		other("ACKNOWLEDGED", "ACK_2XX:201", "CASE-1"),
		other("REJECTED_BY_TARGET", "REJECTED_4XX:422", ""),
		// mock-case's 412 PRECONDITION_FAILED is a refused create, not a stale version: REJECTED_BY_TARGET.
		other("REJECTED_BY_TARGET", "DUPLICATE_CREATE:412:PRECONDITION_FAILED", ""),
		other("OUTCOME_UNKNOWN", "TIMEOUT", ""),
		other("FAILED_TERMINAL", "DEFECT", ""),
		other("DISPATCHED", "DISPATCHED", ""),
		// the signed transport_result is the conflict but the state is not the refusal's
		one("a receipt of another state that records the version conflict", func(r *rcp) { r.state = "REJECTED_BY_TARGET" }),
		one("a stale-version receipt whose transport result is not the version conflict", func(r *rcp) { r.raw = "ACK_2XX:201" }),
		one("a stale-version receipt that carries an acknowledgement", func(r *rcp) { r.ack = "CASE-1" }),
	})
}

// Forged receipts: the row's columns say one thing and the signed document says another, or the signature is not the row's.
func TestAForgedReceiptDoesNotVoidASpentGrant(t *testing.T) {
	runRefused(t, []refusedCase{
		doc("the signed state is ACKNOWLEDGED while the column says REJECTED_STALE_VERSION", "execution_state", "ACKNOWLEDGED"),
		doc("the signed document carries an acknowledgement while the column is empty", "external_ack", "CASE-1"),
		one("the column carries an acknowledgement while the signed document has none", func(r *rcp) { r.ack = "CASE-1"; r.doc = map[string]any{"external_ack": jsonNull} }),
		doc("the signed transport result is not the conflict while the columns say it is", "transport_result", "ACK_2XX"),
		one("the signature key in the document differs from the row's", func(r *rcp) { r.doc = map[string]any{"sig": map[string]any{"key_id": "k2", "value": validSig}} }),
		one("the signature value in the document differs from the row's", func(r *rcp) {
			r.doc = map[string]any{"sig": map[string]any{"key_id": "k1", "value": strings.Repeat("cd", 32)}}
		}),
		doc("the signed document has no signature", "sig", absent),
		// PERD v1.18 REQ-EXE-004, EXE-04: the signature must at least have the shape of an HMAC-SHA-256 (64 lower-case hex
		// characters, which is what crypto.Verify accepts), on both sides; shape is not verification.
		sigShape("an empty key and an empty value on both sides", "", ""),
		sigShape("an empty value on both sides", "k1", ""),
		sigShape("an empty key on both sides", "", validSig),
		sigShape("a value that is not hex (64 characters)", "k1", strings.Repeat("zz", 32)),
		sigShape("a hex value of 63 characters", "k1", validSig[:63]),
		sigShape("a hex value of 65 characters", "k1", validSig+"a"),
		sigShape("a short hex value", "k1", "aa"),
		sigShape("upper-case hex", "k1", strings.ToUpper(validSig)),
		sigShape("hex with a leading space", "k1", " "+validSig[:63]),
		sigShape("hex with a trailing newline", "k1", validSig[:63]+"\n"),
		doc("the signed document has no external_ack member", "external_ack", absent),
		doc("the signed external_ack is an empty string", "external_ack", ""),
		doc("the signed document is not about a receipt state at all", "execution_state", absent),
	})
}

// Mismatched receipts: tenant, grant, attempt or idempotency key is not the one that was spent.
func TestAReceiptOfAnotherTenantGrantOrAttemptDoesNotVoidASpentGrant(t *testing.T) {
	runRefused(t, []refusedCase{
		doc("the signed document names another grant", "grant_id", "grt_9"),
		doc("the signed document names another tenant", "tenant_id", "ten_b"),
		one("the row belongs to another tenant (and so does its document)", func(r *rcp) { r.tenant = "ten_b" }),
		doc("the signed document names another attempt than the spend", "attempt", 2),
		doc("the signed attempt is a string", "attempt", "1"),
		one("the row is the receipt of another attempt than the spend", func(r *rcp) { r.attempt = 2 }),
		one("the row's idempotency key is not the spend's", func(r *rcp) { r.key = "idk_v1_other" }),
		doc("the signed idempotency key is not the spend's", "idempotency_key", "idk_v1_other"),
		// the row's columns are wrong while the signed document is right: each column is checked on its own
		one("only the row's tenant column is another tenant's", func(r *rcp) { r.tenant = "ten_b"; r.doc = map[string]any{"tenant_id": "ten_a"} }),
		one("only the row's attempt column is not the spend's", func(r *rcp) { r.attempt = 2; r.doc = map[string]any{"attempt": 1} }),
		one("only the row's idempotency key column is not the spend's", func(r *rcp) { r.key = "idk_v1_other"; r.doc = map[string]any{"idempotency_key": "idk_v1_grt_1"} }),
		one("only the row's state column is not the refusal", func(r *rcp) {
			r.state = "REJECTED_BY_TARGET"
			r.doc = map[string]any{"execution_state": "REJECTED_STALE_VERSION"}
		}),
		{"a refusal that belongs to another grant", func(t *testing.T, p *pgxpool.Pool) {
			spentGrant(t, p, "grt_2", "n2")
			insertReceipt(t, p, refusal("grt_2"))
		}},
	})
}

// Ambiguous receipts: more than one receipt for the grant is never a confirmation, even when each one is a refusal.
func TestMoreThanOneReceiptDoesNotVoidASpentGrant(t *testing.T) {
	runRefused(t, []refusedCase{
		{"two refusals", func(t *testing.T, p *pgxpool.Pool) {
			insertReceipt(t, p, refusal("grt_1"))
			second := refusal("grt_1")
			second.attempt = 2
			insertReceipt(t, p, second)
		}},
		{"a refusal and an acknowledged receipt", func(t *testing.T, p *pgxpool.Pool) {
			insertReceipt(t, p, refusal("grt_1"))
			receipt(t, p, "grt_1", 2, "ACKNOWLEDGED", "ACK_2XX:201", "CASE-1")
		}},
		{"an acknowledged receipt and a refusal", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 1, "ACKNOWLEDGED", "ACK_2XX:201", "CASE-1")
			second := refusal("grt_1")
			second.attempt = 2
			insertReceipt(t, p, second)
		}},
	})
}

// The receipt contract types transport_result as a string of the shape ^[A-Z][A-Z0-9_]*$ and fixes no value set
// (contracts/schemas/execution-receipt.v1.json; state-registry section 4). The trigger therefore compares the signed field
// with the one class both the adapter and the controller define (VERSION_CONFLICT_409_412) and refuses anything else:
// missing, null, of another type, another shape, another value, or contradicted by the raw status. This is a limitation:
// the value set is not part of the contract.
func TestAnUnexpectedMalformedOrContradictedTransportResultDoesNotVoidASpentGrant(t *testing.T) {
	cases := []refusedCase{
		doc("transport_result is missing", "transport_result", absent),
		doc("transport_result is null", "transport_result", jsonNull),
		doc("transport_result is a number", "transport_result", 409),
		doc("transport_result is an object", "transport_result", map[string]any{"class": conflictClass}),
		doc("transport_result is empty", "transport_result", ""),
		doc("transport_result is lower case", "transport_result", "version_conflict_409_412"),
		doc("transport_result has a leading space", "transport_result", " "+conflictClass),
		doc("transport_result has a trailing space", "transport_result", conflictClass+" "),
		doc("transport_result has a longer name", "transport_result", conflictClass+"_X"),
		doc("transport_result has a shorter name", "transport_result", "VERSION_CONFLICT"),
		doc("transport_result has the status and the error name in it", "transport_result", "409:VERSION_MISMATCH"),
	}
	for _, v := range []string{"TIMEOUT", "REJECTED_4XX", "DUPLICATE_CREATE", "ACK_2XX", "DUPLICATE_409", "CONNECTION_RESET", "SERVER_5XX", "DEFECT"} {
		cases = append(cases, doc("transport_result is another class: "+v, "transport_result", v))
	}
	// raw_status is a diagnostic, but it must agree with the signed field; a contradiction makes the receipt ambiguous.
	cases = append(cases,
		one("the raw status names another class than the signed field", func(r *rcp) { r.raw = "ACK_2XX:201"; r.doc = map[string]any{"transport_result": conflictClass} }),
		one("the raw status is empty", func(r *rcp) { r.raw = ""; r.doc = map[string]any{"transport_result": conflictClass} }),
		one("the raw status starts with a colon", func(r *rcp) {
			r.raw = ":409:VERSION_MISMATCH"
			r.doc = map[string]any{"transport_result": conflictClass}
		}),
		one("the raw status has the conflict class with a suffix", func(r *rcp) {
			r.raw = conflictClass + "X:409"
			r.doc = map[string]any{"transport_result": conflictClass}
		}),
	)
	runRefused(t, cases)
}

// SPENT is final for every other target, even with a confirmed refusal on the Record; VOID and EXPIRED are final.
func TestEveryOtherTransitionOutOfAFinalStatusIsStillRefused(t *testing.T) {
	setup := map[string]func(t *testing.T, pool *pgxpool.Pool){
		"SPENT with a confirmed refusal": func(t *testing.T, p *pgxpool.Pool) {
			spentGrant(t, p, "grt_1", "n1")
			receipt(t, p, "grt_1", 1, "REJECTED_STALE_VERSION", conflictRaw, "")
		},
		"SPENT without a receipt": func(t *testing.T, p *pgxpool.Pool) { spentGrant(t, p, "grt_1", "n1") },
		"VOID after a confirmed refusal": func(t *testing.T, p *pgxpool.Pool) {
			spentGrant(t, p, "grt_1", "n1")
			receipt(t, p, "grt_1", 1, "REJECTED_STALE_VERSION", conflictRaw, "")
			if _, err := p.Exec(ctx(t), voidGrant1); err != nil {
				t.Fatal(err)
			}
		},
		"VOID before any spend": func(t *testing.T, p *pgxpool.Pool) {
			if _, err := p.Exec(ctx(t), voidGrant1); err != nil {
				t.Fatal(err)
			}
		},
		"EXPIRED": func(t *testing.T, p *pgxpool.Pool) {
			if _, err := p.Exec(ctx(t), `UPDATE decision_grant SET status = 'EXPIRED' WHERE grant_id = 'grt_1'`); err != nil {
				t.Fatal(err)
			}
		},
	}
	targets := map[string][]string{
		"SPENT with a confirmed refusal": {"ISSUED", "EXPIRED"},
		"SPENT without a receipt":        {"ISSUED", "EXPIRED", "VOID"},
		"VOID after a confirmed refusal": {"ISSUED", "SPENT", "EXPIRED"},
		"VOID before any spend":          {"ISSUED", "SPENT", "EXPIRED"},
		"EXPIRED":                        {"ISSUED", "SPENT", "VOID"},
	}
	for from, set := range setup {
		for _, to := range targets[from] {
			t.Run(from+" to "+to, func(t *testing.T) {
				pool := migrated(t)
				seed(t, pool)
				set(t, pool)
				before := status(t, pool, "grt_1")
				wantCode(t, try(t, pool, "postgres", fmt.Sprintf(`UPDATE decision_grant SET status = '%s' WHERE grant_id = 'grt_1'`, to)), violation, from+" to "+to)
				if got := status(t, pool, "grt_1"); got != before {
					t.Fatalf("status %q, want %q", got, before)
				}
			})
		}
	}
}

// The ISSUED transitions are unchanged: an unspent grant may become SPENT, VOID or EXPIRED, with no receipt.
func TestAnIssuedGrantStillMovesToEachFinalStatus(t *testing.T) {
	for _, to := range []string{"SPENT", "VOID", "EXPIRED"} {
		t.Run(to, func(t *testing.T) {
			pool := migrated(t)
			seed(t, pool)
			if err := try(t, pool, "execution_rw", fmt.Sprintf(`UPDATE decision_grant SET status = '%s' WHERE grant_id = 'grt_1'`, to)); err != nil {
				t.Fatalf("ISSUED -> %s: %v", to, err)
			}
		})
	}
}

// The permitted transition changes the status and nothing else, and only execution_rw may make it.
func TestTheVoidOfASpentGrantChangesOnlyItsStatusAndOnlyExecutionMakesIt(t *testing.T) {
	pool := migrated(t)
	seed(t, pool)
	spentGrant(t, pool, "grt_1", "n1")
	receipt(t, pool, "grt_1", 1, "REJECTED_STALE_VERSION", conflictRaw, "")
	wantCode(t, try(t, pool, "postgres", `UPDATE decision_grant SET status = 'VOID', json = '{"a":1}' WHERE grant_id = 'grt_1'`), violation, "status and json together")
	wantCode(t, try(t, pool, "postgres", `UPDATE decision_grant SET status = 'VOID', nonce = 'other' WHERE grant_id = 'grt_1'`), violation, "status and nonce together")
	wantCode(t, try(t, pool, "execution_rw", `UPDATE decision_grant SET status = 'VOID', json = '{}' WHERE grant_id = 'grt_1'`), denied, "execution_rw status and json together")
	for _, role := range []string{"trust_rw", "outcome_rw", "evidence_rw", "orchestrator_rw", "record_append"} {
		wantCode(t, try(t, pool, role, voidGrant1), denied, role+" voiding a spent grant")
	}
	wantCode(t, try(t, pool, "postgres", `DELETE FROM decision_grant WHERE grant_id = 'grt_1'`), violation, "DELETE of a spent grant")
	if got := status(t, pool, "grt_1"); got != "SPENT" {
		t.Fatalf("status %q, want SPENT", got)
	}
}

// The spend is of the grant's tenant: a spend recorded under another tenant never confirms a refusal.
func TestARefusalNextToASpendOfAnotherTenantDoesNotVoidASpentGrant(t *testing.T) {
	pool := migrated(t)
	seed(t, pool)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('grt_1', 'ten_b', 'n1', 'idk_v1_grt_1', 1, now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx(t), `UPDATE decision_grant SET status = 'SPENT' WHERE grant_id = 'grt_1'`); err != nil {
		t.Fatal(err)
	}
	insertReceipt(t, pool, refusal("grt_1"))
	wantCode(t, try(t, pool, "execution_rw", voidGrant1), violation, "SPENT -> VOID with a spend of another tenant")
	if got := status(t, pool, "grt_1"); got != "SPENT" {
		t.Fatalf("status %q, want SPENT", got)
	}
}
