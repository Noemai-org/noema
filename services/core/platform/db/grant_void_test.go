package db_test

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PERD v1.18 section 38.2, REQ-EXE-004, INV-27, US-19 (DEC-2026-10-28-spent-grant-void): the grant lifecycle is
// ISSUED -> SPENT, VOID or EXPIRED, and SPENT -> VOID only for a confirmed REJECTED_STALE_VERSION refusal. These tests run
// raw statements against the real trigger, as the roles that exist in production.

const (
	conflictRaw = "VERSION_CONFLICT_409_412:409:VERSION_MISMATCH"
	voidGrant1  = `UPDATE decision_grant SET status = 'VOID' WHERE grant_id = 'grt_1'`
)

// spentGrant makes grt_1 SPENT, as E8 does: one grant_spend row, then the status change.
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

// receipt inserts a signed-looking receipt row for a grant. ack "" is stored as the empty string, as the Ledger stores it.
func receipt(t *testing.T, pool *pgxpool.Pool, grant string, attempt int, state, raw, ack string) {
	t.Helper()
	if _, err := pool.Exec(ctx(t), `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, created_at)
		VALUES ($1, 'ten_a', $2, $3, 'idk_v1_'||$2, 'ROUTE_CASE', $4, $5, $6, now(), '{}', 'k1', 'aa', now())`,
		fmt.Sprintf("rcp_%s_%d", grant, attempt), grant, attempt, state, raw, ack); err != nil {
		t.Fatal(err)
	}
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
	pool := migrated(t)
	seed(t, pool)
	spentGrant(t, pool, "grt_1", "n1")
	receipt(t, pool, "grt_1", 1, "REJECTED_STALE_VERSION", conflictRaw, "")
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
}

// Every other state of the one receipt, and every weaker form of the refusal, still blocks SPENT -> VOID.
func TestASpentGrantStaysSpentWithoutAConfirmedStaleVersionRefusal(t *testing.T) {
	cases := []struct {
		name string
		set  func(t *testing.T, pool *pgxpool.Pool)
	}{
		{"no receipt", func(*testing.T, *pgxpool.Pool) {}},
		{"an ACKNOWLEDGED receipt", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 1, "ACKNOWLEDGED", "ACK_2XX:201", "CASE-1")
		}},
		{"a REJECTED_BY_TARGET receipt", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 1, "REJECTED_BY_TARGET", "REJECTED_4XX:422", "")
		}},
		{"an OUTCOME_UNKNOWN receipt", func(t *testing.T, p *pgxpool.Pool) { receipt(t, p, "grt_1", 1, "OUTCOME_UNKNOWN", "TIMEOUT", "") }},
		{"a FAILED_TERMINAL receipt", func(t *testing.T, p *pgxpool.Pool) { receipt(t, p, "grt_1", 1, "FAILED_TERMINAL", "DEFECT", "") }},
		{"a DISPATCHED receipt", func(t *testing.T, p *pgxpool.Pool) { receipt(t, p, "grt_1", 1, "DISPATCHED", "DISPATCHED", "") }},
		{"a receipt of another state that records the version conflict", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 1, "REJECTED_BY_TARGET", conflictRaw, "")
		}},
		{"a stale-version receipt whose transport result is not the version conflict", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 1, "REJECTED_STALE_VERSION", "ACK_2XX:201", "")
		}},
		{"a stale-version receipt that carries an external acknowledgement", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 1, "REJECTED_STALE_VERSION", conflictRaw, "CASE-1")
		}},
		{"a stale-version receipt of another attempt than the spend", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 2, "REJECTED_STALE_VERSION", conflictRaw, "")
		}},
		{"a stale-version receipt next to another receipt of the same grant", func(t *testing.T, p *pgxpool.Pool) {
			receipt(t, p, "grt_1", 1, "REJECTED_STALE_VERSION", conflictRaw, "")
			receipt(t, p, "grt_1", 2, "ACKNOWLEDGED", "ACK_2XX:201", "CASE-1")
		}},
		{"a stale-version receipt that belongs to another grant", func(t *testing.T, p *pgxpool.Pool) {
			spentGrant(t, p, "grt_2", "n2")
			receipt(t, p, "grt_2", 1, "REJECTED_STALE_VERSION", conflictRaw, "")
		}},
	}
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
