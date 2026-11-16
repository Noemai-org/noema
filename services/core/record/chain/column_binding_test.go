package chain_test

// SR's finding 1 on noema#263 (REQ-AUD-006, DEBT-03, D8 check 3): the typed columns the Record serves must equal the fields of the document that was signed. The
// signature authenticates the stored body; reconstruction reads the columns. A correctly signed body stored with one column changed must be refused with NS001
// (the database cannot recompute payload_hash from the payload; the chain verifier does). All data is SYNTHETIC.

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// signedParams returns the 17 arguments of record_insert_t for a genuinely signed event as the emitter, built the way the appender builds them.
func signedParams(t *testing.T, w *world, tx pgx.Tx, tenant, emitter string) []any {
	t.Helper()
	var seq int64
	var prev string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &prev); err != nil {
		t.Fatalf("lock head: %v", err)
	}
	ev := chain.Event{TenantID: tenant, CorrelationID: "corr_abc123", WrittenBy: emitter, Actor: emitter, EventType: "GRANT_SPENT", ObjectRef: "grt_bind01"}
	payloadHash, payloadJSON, err := chain.PayloadHash(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := forgedAt.Truncate(time.Millisecond)
	body := chain.Body(ev, "recev_bind01", seq+1, prev, payloadHash, at)
	hash, err := chain.Hash(prev, body)
	if err != nil {
		t.Fatal(err)
	}
	bodyJSON, err := crypto.CanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	keyID, sig, signed, err := w.app.Signers[emitter].SignRecordEvent(body)
	if err != nil {
		t.Fatal(err)
	}
	return []any{"recev_bind01", seq + 1, prev, hash, emitter, ev.EventType, ev.ObjectRef, payloadHash, ev.CorrelationID, at, chain.SchemaVersion, emitter, string(bodyJSON), payloadJSON, keyID, sig, signed}
}

const insertSigned = `SELECT record_insert_t($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14::jsonb, $15, $16, $17)`

func TestACorrectlySignedBodyWithAChangedColumnIsRefused(t *testing.T) {
	const tenant, emitter = "ten_cb", "core-execution"
	w := newWorld(t, tenant)
	pool := w.writer(t, tenant, emitter)
	try := func(alter func(p []any)) error {
		tx, err := pool.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx(t)) }()
		p := signedParams(t, w, tx, tenant, emitter)
		if alter != nil {
			alter(p)
		}
		_, err = tx.Exec(ctx(t), insertSigned, p...)
		return err
	}
	if err := try(nil); err != nil {
		t.Fatalf("control: the unaltered signed event must be stored: %v", err)
	}
	cases := map[string]func(p []any){
		"event_type":     func(p []any) { p[5] = "GRANT_ISSUED" },
		"object_ref":     func(p []any) { p[6] = "grt_other01" },
		"payload_hash":   func(p []any) { p[7] = "sha256:1111111111111111111111111111111111111111111111111111111111111111" },
		"correlation_id": func(p []any) { p[8] = "corr_other01" },
		"at":             func(p []any) { p[9] = forgedAt.Add(time.Second) },
		"schema_version": func(p []any) { p[10] = "2.0" },
		"actor":          func(p []any) { p[11] = "core-trust" },
	}
	for column, alter := range cases {
		err := try(alter)
		if got := sqlState(err); got != "NS001" {
			t.Errorf("a correctly signed body stored with a changed %s: got %v (SQLSTATE %q), want NS001", column, err, got)
		}
	}
}
