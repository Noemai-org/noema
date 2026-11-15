package recordsig

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// AppendUnguarded appends ev the way chain.Appender.Append does, minus the payload digit-run guard of A184 (REQ-AUD-001, A154), and returns what Append would. It exists for
// one reason: a test that must put a run of more than four digits INTO the Record, to prove that a reader masks it (reconstruct, the export), or to prove that the test
// scan finds it. Production code cannot reach it (this package is test-only, see the package comment). A test that calls it also calls testpg.AllowUnguardedRecord, so the
// scan of the stored Record does not fail the test for the very rows it made on purpose. The database still verifies the signature and the chain, so the row is a real one.
func AppendUnguarded(t testing.TB, a *chain.Appender, tx pgx.Tx, ev chain.Event) chain.Stored {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	payloadHash, payloadJSON, err := chain.PayloadHash(ev.Payload)
	if err != nil {
		t.Fatalf("recordsig: payload: %v", err)
	}
	id, err := a.IDs.ID("recev")
	if err != nil {
		t.Fatalf("recordsig: event id: %v", err)
	}
	at := a.Clock.Now().UTC().Truncate(time.Millisecond)
	s, ok := a.Signers[ev.WrittenBy]
	if !ok || s == nil {
		t.Fatalf("recordsig: no signing key for %q", ev.WrittenBy)
	}
	var seq int64
	var prev string
	if err := tx.QueryRow(ctx, `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &prev); err != nil {
		t.Fatalf("recordsig: lock head: %v", err)
	}
	body := chain.Body(ev, id, seq+1, prev, payloadHash, at)
	hash, err := chain.Hash(prev, body)
	if err != nil {
		t.Fatalf("recordsig: hash: %v", err)
	}
	bodyJSON, err := crypto.CanonicalJSON(body)
	if err != nil {
		t.Fatalf("recordsig: canonical body: %v", err)
	}
	keyID, sig, signed, err := s.SignRecordEvent(body)
	if err != nil {
		t.Fatalf("recordsig: sign: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT record_insert_t($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14::jsonb, $15, $16, $17)`,
		id, seq+1, prev, hash, ev.WrittenBy, ev.EventType, ev.ObjectRef, payloadHash, ev.CorrelationID, at,
		chain.SchemaVersion, ev.Actor, string(bodyJSON), payloadJSON, keyID, sig, signed); err != nil {
		t.Fatalf("recordsig: insert: %s", fmt.Sprint(err))
	}
	return chain.Stored{EventID: id, Seq: seq + 1, PrevHash: prev, Hash: hash}
}
