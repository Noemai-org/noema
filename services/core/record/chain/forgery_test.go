package chain_test

// REC-08 closure test, first half (REQ-AUD-006, DEBT-03, DEBT-08; INV-10, INV-11; docs/security/debt-03-08-record-writer-closure-design.md, matrix
// rows 1 and 5). Written before any REC-08 code and seen failing: today the database checks only that an event extends the head, so a holder of one
// runtime credential, with no signing key, appends a well-formed event under the emitter that credential is bound to. The forger needs no key to
// compute `hash` (it is a plain SHA-256), only the credential. After REC-08 the same attempt must be refused (SQLSTATE NS001, or an error because the
// unsigned function no longer exists). The other rows (a wrong key, another identity's key, replay, revocation, unavailable verification) need the
// signed interface and arrive with REC-08 itself. All data is SYNTHETIC.

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// forgedAt is a SYNTHETIC instant (the clock rule: no time.Now outside platform/clock).
var forgedAt = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

func TestAForgedUnsignedRecordEventIsRefusedForEveryRuntimeCredential(t *testing.T) {
	const tenant = "ten_ff"
	for _, emitter := range []string{"core-trust", "core-execution", "core-outcome", "core-evidence", "core-workflows"} {
		t.Run(emitter, func(t *testing.T) {
			w := newWorld(t, tenant)
			pool := w.writer(t, tenant, emitter)
			tx, err := pool.Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx(t)) }()
			if err := forge(t, tx, tenant, emitter); err == nil {
				t.Fatalf("a forged event written as %s by a holder of its database credential, with no signing key, was accepted (DEBT-03)", emitter)
			}
		})
	}
}

// forge appends a correctly hashed event as the emitter, the way a holder of the credential can: lock the head, hash, insert.
func forge(t *testing.T, tx pgx.Tx, tenant, emitter string) error {
	t.Helper()
	var seq int64
	var prev string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &prev); err != nil {
		t.Fatalf("lock head: %v", err)
	}
	ev := chain.Event{TenantID: tenant, CorrelationID: "corr_abc123", WrittenBy: emitter, Actor: emitter, EventType: "GRANT_SPENT", ObjectRef: "grt_forged01"}
	payloadHash, _, err := chain.PayloadHash(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := chain.Body(ev, "recev_forged01", seq+1, prev, payloadHash, forgedAt)
	hash, err := chain.Hash(prev, body)
	if err != nil {
		t.Fatal(err)
	}
	bodyJSON, err := crypto.CanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx(t), `SELECT record_insert_t($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, NULL)`,
		"recev_forged01", seq+1, prev, hash, emitter, ev.EventType, ev.ObjectRef, payloadHash, ev.CorrelationID, forgedAt, chain.SchemaVersion, emitter, string(bodyJSON))
	return err
}
