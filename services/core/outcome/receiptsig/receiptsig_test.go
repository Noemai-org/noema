package receiptsig_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/receiptsig"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// REQ-AUD-006, DEC-2026-11-01-receipt-and-record-signing D4, D6, D7 and section 8: Outcome verifies an Execution receipt with
// core-execution's PUBLIC key. The message is the domain tag "noema/receipt/v1\n" followed by the RFC 8785 bytes of the receipt
// without sig. These tests build that message themselves, from the specification and not from the code under test. Every key is
// generated for the run and is SYNTHETIC (AGENT-25).

const (
	keyID    = "core-execution.ed25519.1"
	tagRecpt = "noema/receipt/v1\n"
)

var dispatched = time.Date(2026, 11, 4, 9, 0, 0, 123456789, time.UTC) // SYNTHETIC

type keyTable map[string]receiptsig.PublicKey

func (k keyTable) Lookup(_ context.Context, id string) (receiptsig.PublicKey, bool, error) {
	pk, ok := k[id]
	return pk, ok, nil
}

type failingTable struct{}

func (failingTable) Lookup(context.Context, string) (receiptsig.PublicKey, bool, error) {
	return receiptsig.PublicKey{}, false, errors.New("signing_key unavailable")
}

type fixture struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	doc  map[string]any
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader) // a random key per run
	if err != nil {
		t.Fatal(err)
	}
	return fixture{pub: pub, priv: priv, doc: map[string]any{
		"schema_version": "1", "correlation_id": "corr_abc123", "receipt_id": "rcp_0001", "grant_id": "grt_0001", "tenant_id": "ten_aa",
		"idempotency_key": "idk_v1_abc", "logical_action": "ESCALATE_CASE", "attempt": int64(1), "dispatched_at": dispatched.Format(time.RFC3339Nano),
		"transport_result": "TIMEOUT", "execution_state": "OUTCOME_UNKNOWN", "external_ack": nil,
	}}
}

func (f fixture) message(t *testing.T, tag string, doc map[string]any) []byte {
	t.Helper()
	jcs, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(tag), jcs...)
}

// receipt returns the Receipt Outcome loads: its columns, and the stored document with sig inside it.
func (f fixture) receipt(t *testing.T, id string, sig []byte) verifier.Receipt {
	t.Helper()
	value := hex.EncodeToString(sig)
	stored := map[string]any{}
	for k, v := range f.doc {
		stored[k] = v
	}
	stored["sig"] = map[string]any{"key_id": id, "value": value}
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	return verifier.Receipt{ReceiptID: "rcp_0001", TenantID: "ten_aa", GrantID: "grt_0001", Attempt: 1, IdempotencyKey: "idk_v1_abc",
		LogicalAction: "ESCALATE_CASE", State: "OUTCOME_UNKNOWN", CorrelationID: "corr_abc123", DispatchedAt: dispatched,
		Document: raw, SigKeyID: id, SigValue: value}
}

func (f fixture) signed(t *testing.T) verifier.Receipt {
	t.Helper()
	return f.receipt(t, keyID, ed25519.Sign(f.priv, f.message(t, tagRecpt, f.doc)))
}

func (f fixture) checker(status receiptsig.KeyStatus) receiptsig.Checker {
	return receiptsig.Checker{Keys: keyTable{keyID: {Key: f.pub, Status: status}}}
}

func check(t *testing.T, c receiptsig.Checker, r verifier.Receipt) verifier.Authenticity {
	t.Helper()
	got, err := c.Check(context.Background(), r)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return got
}

func TestAGenuineReceiptIsValid(t *testing.T) {
	f := newFixture(t)
	if got := check(t, f.checker(receiptsig.Active), f.signed(t)); got != verifier.AuthValid {
		t.Fatalf("got %s", got)
	}
}

// D7: the signature binds the tenant, grant, attempt, idempotency key, state and the other fields. A genuine receipt is not
// valid for a row that says anything else.
func TestEveryBoundFieldIsBound(t *testing.T) {
	f := newFixture(t)
	c := f.checker(receiptsig.Active)
	columns := map[string]func(*verifier.Receipt){
		"tenant":          func(r *verifier.Receipt) { r.TenantID = "ten_bb" },
		"grant":           func(r *verifier.Receipt) { r.GrantID = "grt_0002" },
		"attempt":         func(r *verifier.Receipt) { r.Attempt = 2 },
		"idempotency key": func(r *verifier.Receipt) { r.IdempotencyKey = "idk_v1_other" },
		"state":           func(r *verifier.Receipt) { r.State = "ACKNOWLEDGED" },
		"receipt id":      func(r *verifier.Receipt) { r.ReceiptID = "rcp_0002" },
		"correlation":     func(r *verifier.Receipt) { r.CorrelationID = "corr_other1" },
		"logical action":  func(r *verifier.Receipt) { r.LogicalAction = "ROUTE_CASE" },
		"dispatched at":   func(r *verifier.Receipt) { r.DispatchedAt = r.DispatchedAt.Add(time.Second) },
	}
	for name, mutate := range columns {
		t.Run("a row column: "+name, func(t *testing.T) {
			r := f.signed(t)
			mutate(&r)
			if got := check(t, c, r); got != verifier.AuthInvalid {
				t.Fatalf("got %s", got)
			}
		})
	}
	for _, field := range []string{"tenant_id", "grant_id", "idempotency_key", "execution_state", "receipt_id", "correlation_id", "logical_action", "transport_result", "dispatched_at"} {
		t.Run("a document field: "+field, func(t *testing.T) {
			r := f.signed(t)
			var doc map[string]any
			if err := json.Unmarshal(r.Document, &doc); err != nil {
				t.Fatal(err)
			}
			doc[field] = "tampered"
			r.Document, _ = json.Marshal(doc)
			if got := check(t, c, r); got != verifier.AuthInvalid {
				t.Fatalf("got %s", got)
			}
		})
	}
}

// D6: the domain tag is part of the message. A signature over the bare JCS, or over another object's tag, is not a receipt's.
func TestTheDomainTagIsPartOfTheMessage(t *testing.T) {
	f := newFixture(t)
	c := f.checker(receiptsig.Active)
	jcs, _ := crypto.CanonicalJSON(f.doc)
	for name, msg := range map[string][]byte{
		"no tag":             jcs,
		"the verdict's tag":  f.message(t, "noema/outcome-verdict/v1\n", f.doc),
		"the event's tag":    f.message(t, "noema/record-event/v1\n", f.doc),
		"a tag without a LF": f.message(t, "noema/receipt/v1", f.doc),
	} {
		t.Run(name, func(t *testing.T) {
			if got := check(t, c, f.receipt(t, keyID, ed25519.Sign(f.priv, msg))); got != verifier.AuthInvalid {
				t.Fatalf("got %s", got)
			}
		})
	}
}

func TestOnlyExecutionsIdentityMaySignAReceipt(t *testing.T) {
	f := newFixture(t)
	other := "core-outcome.ed25519.1"
	c := receiptsig.Checker{Keys: keyTable{other: {Key: f.pub, Status: receiptsig.Active}}}
	r := f.receipt(t, other, ed25519.Sign(f.priv, f.message(t, tagRecpt, f.doc)))
	if got := check(t, c, r); got != verifier.AuthInvalid {
		t.Fatalf("a receipt signed under core-outcome's key: %s", got)
	}
}

func TestAMalformedKeyIDOrSignatureIsInvalid(t *testing.T) {
	f := newFixture(t)
	good := ed25519.Sign(f.priv, f.message(t, tagRecpt, f.doc))
	c := receiptsig.Checker{Keys: keyTable{keyID: {Key: f.pub, Status: receiptsig.Active}, "exec-hmac-1": {Key: f.pub, Status: receiptsig.Active}}}
	for name, r := range map[string]verifier.Receipt{
		"an HMAC key id":         f.receipt(t, "exec-hmac-1", good),
		"version zero":           f.receipt(t, "core-execution.ed25519.0", good),
		"a leading zero version": f.receipt(t, "core-execution.ed25519.01", good),
		"another algorithm":      f.receipt(t, "core-execution.hmac.1", good),
		"an empty key id":        f.receipt(t, "", good),
		"upper-case signature":   withValue(f.receipt(t, keyID, good), upper(hex.EncodeToString(good))),
		"a short signature":      withValue(f.receipt(t, keyID, good), hex.EncodeToString(good[:len(good)-1])),
		"a long signature":       withValue(f.receipt(t, keyID, good), hex.EncodeToString(append(append([]byte{}, good...), 0))),
		"a non-hex signature":    withValue(f.receipt(t, keyID, good), "zz"+hex.EncodeToString(good)[2:]),
		"an empty signature":     withValue(f.receipt(t, keyID, good), ""),
	} {
		t.Run(name, func(t *testing.T) {
			if got := check(t, c, r); got != verifier.AuthInvalid {
				t.Fatalf("got %s", got)
			}
		})
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// withValue sets the signature value in both the column and the stored document.
func withValue(r verifier.Receipt, value string) verifier.Receipt {
	var doc map[string]any
	_ = json.Unmarshal(r.Document, &doc)
	doc["sig"] = map[string]any{"key_id": r.SigKeyID, "value": value}
	r.Document, _ = json.Marshal(doc)
	r.SigValue = value
	return r
}

func TestAWrongOrUnknownOrRevokedKeyIsInvalid(t *testing.T) {
	f := newFixture(t)
	r := f.signed(t)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	for name, c := range map[string]receiptsig.Checker{
		"another public key":    {Keys: keyTable{keyID: {Key: otherPub, Status: receiptsig.Active}}},
		"an unknown key":        {Keys: keyTable{}},
		"a revoked key":         f.checker(receiptsig.Revoked),
		"a status not known":    f.checker(receiptsig.KeyStatus("PAUSED")),
		"a key of a bad length": {Keys: keyTable{keyID: {Key: f.pub[:len(f.pub)-1], Status: receiptsig.Active}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := check(t, c, r); got != verifier.AuthInvalid {
				t.Fatalf("got %s", got)
			}
		})
	}
}

// D5: a retired key still verifies the history it signed.
func TestARetiredKeyStillVerifiesItsHistory(t *testing.T) {
	f := newFixture(t)
	if got := check(t, f.checker(receiptsig.Retired), f.signed(t)); got != verifier.AuthValid {
		t.Fatalf("got %s", got)
	}
}

// A key source that cannot answer is not a finding about the receipt: the check cannot run (an error), and the verifier fails closed.
func TestAKeySourceThatCannotAnswerIsAnErrorNotInvalid(t *testing.T) {
	f := newFixture(t)
	got, err := receiptsig.Checker{Keys: failingTable{}}.Check(context.Background(), f.signed(t))
	if err == nil {
		t.Fatalf("no error; got %s", got)
	}
	if got == verifier.AuthValid {
		t.Fatal("a receipt was valid although no key could be read")
	}
}

func TestACheckerWithoutKeysRefusesToRun(t *testing.T) {
	f := newFixture(t)
	if got, err := (receiptsig.Checker{}).Check(context.Background(), f.signed(t)); err == nil || got == verifier.AuthValid {
		t.Fatalf("got %s, %v", got, err)
	}
}

// The signature the receipt carries in its document must be the one in the row's columns.
func TestTheDocumentsSigMustMatchTheColumns(t *testing.T) {
	f := newFixture(t)
	c := f.checker(receiptsig.Active)
	r := f.signed(t)
	r.SigKeyID = "core-execution.ed25519.2"
	if got := check(t, c, r); got != verifier.AuthInvalid {
		t.Fatalf("a different key id in the column: %s", got)
	}
	r = f.signed(t)
	r.SigValue = hex.EncodeToString(make([]byte, ed25519.SignatureSize))
	if got := check(t, c, r); got != verifier.AuthInvalid {
		t.Fatalf("a different value in the column: %s", got)
	}
}

// The stored document carries its own sig member. If that differs from the row's signature, the row and the document are two
// different receipts, even though the row's signature is valid over the document.
func TestTheSigMemberOfTheDocumentMustBeTheRowsSignature(t *testing.T) {
	f := newFixture(t)
	c := f.checker(receiptsig.Active)
	for name, sig := range map[string]map[string]any{
		"another value":  {"key_id": keyID, "value": hex.EncodeToString(make([]byte, ed25519.SignatureSize))},
		"another key id": {"key_id": "core-execution.ed25519.2", "value": f.signed(t).SigValue},
		"no sig object":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			r := f.signed(t)
			var doc map[string]any
			if err := json.Unmarshal(r.Document, &doc); err != nil {
				t.Fatal(err)
			}
			if sig == nil {
				delete(doc, "sig")
			} else {
				doc["sig"] = sig
			}
			r.Document, _ = json.Marshal(doc)
			if got := check(t, c, r); got != verifier.AuthInvalid {
				t.Fatalf("got %s", got)
			}
		})
	}
}

func TestADocumentThatIsNotAReceiptIsInvalid(t *testing.T) {
	f := newFixture(t)
	c := f.checker(receiptsig.Active)
	for name, doc := range map[string][]byte{"not JSON": []byte("{"), "a list": []byte("[]"), "empty": nil, "no sig member": []byte(`{"receipt_id":"rcp_0001"}`)} {
		t.Run(name, func(t *testing.T) {
			r := f.signed(t)
			r.Document = doc
			if got := check(t, c, r); got != verifier.AuthInvalid {
				t.Fatalf("got %s", got)
			}
		})
	}
}

// Malleability (the PERD amendment's test list): a signature with S replaced by S plus the group order L is the same point and must
// be refused.
func TestAMalleatedSignatureIsRefused(t *testing.T) {
	f := newFixture(t)
	sig := ed25519.Sign(f.priv, f.message(t, tagRecpt, f.doc))
	l, _ := new(big.Int).SetString("7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)
	s := new(big.Int).SetBytes(reverse(sig[32:]))
	s.Add(s, l)
	sb := s.Bytes()
	if len(sb) > 32 {
		t.Skip("S plus L does not fit in 32 bytes for this signature; the check is exercised by the others")
	}
	padded := append(make([]byte, 32-len(sb)), sb...)
	mal := append(append([]byte{}, sig[:32]...), reverse(padded)...)
	if got := check(t, f.checker(receiptsig.Active), f.receipt(t, keyID, mal)); got != verifier.AuthInvalid {
		t.Fatalf("a malleated signature: %s", got)
	}
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}
