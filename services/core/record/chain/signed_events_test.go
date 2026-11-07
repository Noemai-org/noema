package chain_test

// REC-08 closure matrix (REQ-AUD-006, DEBT-03, DEBT-08; INV-10, INV-11; DEC-2026-11-01 D2, D7, D11, D12; docs/security/debt-03-08-record-writer-closure-design.md,
// rows 1 to 3, 7, 9, 11). The database accepts a Record event only if its Ed25519 signature verifies under a registered, ACTIVE key of the identity in
// `written_by`, over the exact bytes it is given (signed_jcs), and those bytes say what the row says (tenant, seq, prev_hash, written_by). The private key is
// never in PostgreSQL. record_insert_t takes the signature as three more arguments; the unsigned form no longer exists. Written first, against a function that
// does not exist yet: every case below fails today, the positive control included. All keys and data are SYNTHETIC.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const recordEventTag = "noema/record-event/v1\n"

type signedWorld struct {
	*world
	keys map[string]ed25519.PrivateKey // key id to private key
}

func newSignedWorld(t *testing.T, tenant string) *signedWorld {
	t.Helper()
	return &signedWorld{world: newWorld(t, tenant), keys: map[string]ed25519.PrivateKey{}}
}

func (w *signedWorld) register(t *testing.T, identity string, version int) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := w.admin.QueryRow(ctx(t), `SELECT key_register($1, $2, $3)`, identity, version, []byte(pub)).Scan(&id); err != nil {
		t.Fatalf("key_register(%s, %d): %v", identity, version, err)
	}
	w.keys[id] = priv
	return id
}

// insert appends one correctly hashed event for the tenant's chain through record_insert_t with the three signature arguments. sign returns the key ID, the
// hex signature and the signed bytes for a body; a nil sign sends an empty signature.
func (w *signedWorld) insert(t *testing.T, tenant, emitter string, sign func(body map[string]any, jcs []byte) (string, string), mutate func(body map[string]any)) error {
	t.Helper()
	tx, err := w.writer(t, tenant, emitter).Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	var seq int64
	var prev string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head_t()`).Scan(&seq, &prev); err != nil {
		t.Fatalf("lock head: %v", err)
	}
	ev := chain.Event{TenantID: tenant, CorrelationID: "corr_abc123", WrittenBy: emitter, Actor: emitter, EventType: "GRANT_SPENT", ObjectRef: "grt_signed001"}
	payloadHash, _, err := chain.PayloadHash(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := chain.Body(ev, "recev_signed001", seq+1, prev, payloadHash, forgedAt)
	if mutate != nil {
		mutate(body)
	}
	hash, err := chain.Hash(prev, body)
	if err != nil {
		t.Fatal(err)
	}
	jcs, err := crypto.CanonicalJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	keyID, sig := "", ""
	if sign != nil {
		keyID, sig = sign(body, jcs)
	}
	bodyJSON := jcs
	_, err = tx.Exec(ctx(t), `SELECT record_insert_t($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, NULL, $14, $15, $16)`,
		"recev_signed001", seq+1, prev, hash, emitter, ev.EventType, ev.ObjectRef, payloadHash, ev.CorrelationID, forgedAt, chain.SchemaVersion, emitter,
		string(bodyJSON), keyID, sig, jcs)
	if err == nil {
		err = tx.Commit(ctx(t))
	}
	return err
}

// signWith signs the exact bytes with the private key of keyID, claiming keyID.
func (w *signedWorld) signWith(keyID string) func(map[string]any, []byte) (string, string) {
	return func(_ map[string]any, jcs []byte) (string, string) {
		priv := w.keys[keyID]
		return keyID, hex.EncodeToString(ed25519.Sign(priv, append([]byte(recordEventTag), jcs...)))
	}
}

func wantNS001(t *testing.T, err error, what string) {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "NS001" {
		t.Fatalf("%s: got %v, want SQLSTATE NS001", what, err)
	}
}

func TestAGenuinelySignedEventIsStoredForEveryPower(t *testing.T) {
	for _, emitter := range []string{"core-trust", "core-execution", "core-outcome", "core-evidence", "core-workflows"} {
		t.Run(emitter, func(t *testing.T) {
			w := newSignedWorld(t, "ten_ff")
			id := w.register(t, emitter, 1)
			if err := w.insert(t, "ten_ff", emitter, w.signWith(id), nil); err != nil {
				t.Fatalf("a genuinely signed event was refused: %v", err)
			}
			var n int
			if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event WHERE event_id = 'recev_signed001'`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("stored %d rows (%v), want 1", n, err)
			}
		})
	}
}

func TestAForgedEventIsRefusedBySignatureCheck(t *testing.T) {
	cases := map[string]func(w *signedWorld) (func(map[string]any, []byte) (string, string), func(map[string]any)){
		"no signature at all": func(w *signedWorld) (func(map[string]any, []byte) (string, string), func(map[string]any)) {
			w.register(t, "core-execution", 1)
			return nil, nil
		},
		"a signature made with a key the database does not hold (right key ID)": func(w *signedWorld) (func(map[string]any, []byte) (string, string), func(map[string]any)) {
			id := w.register(t, "core-execution", 1)
			_, other, _ := ed25519.GenerateKey(rand.Reader)
			return func(_ map[string]any, jcs []byte) (string, string) {
				return id, hex.EncodeToString(ed25519.Sign(other, append([]byte(recordEventTag), jcs...)))
			}, nil
		},
		"the key of another identity (core-outcome's key for a core-execution event)": func(w *signedWorld) (func(map[string]any, []byte) (string, string), func(map[string]any)) {
			w.register(t, "core-execution", 1)
			out := w.register(t, "core-outcome", 1)
			return w.signWith(out), nil
		},
		"a signature over the receipt domain tag instead of the record-event tag": func(w *signedWorld) (func(map[string]any, []byte) (string, string), func(map[string]any)) {
			id := w.register(t, "core-execution", 1)
			return func(_ map[string]any, jcs []byte) (string, string) {
				return id, hex.EncodeToString(ed25519.Sign(w.keys[id], append([]byte("noema/receipt/v1\n"), jcs...)))
			}, nil
		},
		"signed bytes that say another sequence number than the row (a replay at another seq)": func(w *signedWorld) (func(map[string]any, []byte) (string, string), func(map[string]any)) {
			id := w.register(t, "core-execution", 1)
			return func(body map[string]any, _ []byte) (string, string) {
				replay := map[string]any{}
				for k, v := range body {
					replay[k] = v
				}
				replay["seq"] = int64(999) // a genuine signature over bytes that name another position in the chain than the row
				b, _ := crypto.CanonicalJSON(replay)
				return id, hex.EncodeToString(ed25519.Sign(w.keys[id], append([]byte(recordEventTag), b...)))
			}, nil
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			w := newSignedWorld(t, "ten_ff")
			sign, mutate := mk(w)
			wantNS001(t, w.insert(t, "ten_ff", "core-execution", sign, mutate), name)
			var n int
			if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("%d row(s) stored for a refused event (%v)", n, err)
			}
		})
	}
}

func TestAnEmitterWithNoRegisteredKeyCannotAppend(t *testing.T) {
	w := newSignedWorld(t, "ten_ff")
	id := w.register(t, "core-execution", 1)
	// model-service has no key under the default of OD-P-73; a core-execution key does not stand in for it.
	wantNS001(t, w.insert(t, "ten_ff", "model-service", w.signWith(id), nil), "an emitter with no key")
}

func TestARevokedOrRetiredKeyIsRefusedAndRotationKeepsBothActiveKeys(t *testing.T) {
	w := newSignedWorld(t, "ten_ff")
	old := w.register(t, "core-execution", 1)
	next := w.register(t, "core-execution", 2) // the rotation overlap: two ACTIVE keys
	if err := w.insert(t, "ten_ff", "core-execution", w.signWith(old), nil); err != nil {
		t.Fatalf("an event under the old key during the overlap: %v", err)
	}
	if _, err := w.admin.Exec(ctx(t), `SELECT key_retire($1)`, old); err != nil {
		t.Fatal(err)
	}
	if err := w.insert(t, "ten_ff", "core-execution", w.signWith(next), nil); err != nil {
		t.Fatalf("an event under the new key: %v", err)
	}
	wantNS001(t, w.insert(t, "ten_ff", "core-execution", w.signWith(old), nil), "an event under a retired key")
	if _, err := w.admin.Exec(ctx(t), `SELECT key_revoke($1, now())`, next); err != nil {
		t.Fatal(err)
	}
	wantNS001(t, w.insert(t, "ten_ff", "core-execution", w.signWith(next), nil), "an event under a revoked key")
}
