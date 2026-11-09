package receiptsig

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

const (
	errPrefix = "receiptsig: "
	// receiptTag is the domain tag of an execution receipt (DEC-2026-11-01-receipt-and-record-signing D6). It stops a signature
	// over another object type from verifying as a receipt.
	receiptTag = "noema/receipt/v1\n"
	// executionIdentity is the only identity whose key may sign a receipt (D2, D7).
	executionIdentity = "core-execution"
)

// keyIDPattern is the key ID of D4: <identity>.ed25519.<version>, the version a positive integer without a leading zero.
var keyIDPattern = regexp.MustCompile(`^([a-z][a-z-]*)\.ed25519\.([1-9][0-9]*)$`)

// sigPattern is a signature as sig.value carries it: lower-case hex of the 64 signature bytes.
var sigPattern = regexp.MustCompile(`^[0-9a-f]{128}$`)

// KeyStatus is the state of a signing key (D5).
type KeyStatus string

const (
	Active  KeyStatus = "ACTIVE"
	Retired KeyStatus = "RETIRED" // still verifies the history it signed
	Revoked KeyStatus = "REVOKED" // believed compromised: its objects are untrusted
)

// PublicKey is a public signing key and its state. It is all Outcome ever holds of Execution's key (section 8).
type PublicKey struct {
	Key    ed25519.PublicKey
	Status KeyStatus
}

// PublicKeys is the verify-only interface to Execution's public keys (section 8). It has no way to sign and none to change a key.
// found is false for a key it does not know; an error means it could not answer.
type PublicKeys interface {
	Lookup(ctx context.Context, keyID string) (key PublicKey, found bool, err error)
}

// Checker implements verifier.ReceiptAuthenticity with real Ed25519 verification of an Execution receipt (REQ-AUD-006). It
// returns INVALID for a receipt that is not genuine, and an error when it cannot tell (no key source, a lookup that failed): the
// verifier fails closed on both, and only an INVALID result is a security finding.
type Checker struct {
	Keys PublicKeys
}

var _ verifier.ReceiptAuthenticity = Checker{}

// Check verifies that r carries a valid signature of core-execution over its own document, and that the document is the row.
func (c Checker) Check(ctx context.Context, r verifier.Receipt) (verifier.Authenticity, error) {
	if c.Keys == nil {
		return verifier.AuthNotChecked, errors.New(errPrefix + "no source of public keys")
	}
	m := keyIDPattern.FindStringSubmatch(r.SigKeyID)
	if m == nil || m[1] != executionIdentity || !sigPattern.MatchString(r.SigValue) {
		return verifier.AuthInvalid, nil
	}
	doc, ok := parseDocument(r.Document)
	if !ok || !signatureInDocument(doc, r) || !documentIsTheRow(doc, r) {
		return verifier.AuthInvalid, nil
	}
	delete(doc, "sig")
	jcs, err := crypto.CanonicalJSON(doc)
	if err != nil {
		return verifier.AuthInvalid, nil // a document that cannot be put in canonical form was not signed in it
	}
	pk, found, err := c.Keys.Lookup(ctx, r.SigKeyID)
	if err != nil {
		return verifier.AuthNotChecked, fmt.Errorf("%slook up %s: %w", errPrefix, r.SigKeyID, err)
	}
	if !found || (pk.Status != Active && pk.Status != Retired) || !crypto.UsableEd25519PublicKey(pk.Key) {
		return verifier.AuthInvalid, nil
	}
	sig, err := hex.DecodeString(r.SigValue)
	if err != nil || !ed25519.Verify(pk.Key, append([]byte(receiptTag), jcs...), sig) {
		return verifier.AuthInvalid, nil
	}
	return verifier.AuthValid, nil
}

// parseDocument reads the stored receipt as an object, with integers kept exact.
func parseDocument(raw []byte) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil, false
	}
	return doc, true
}

// signatureInDocument holds when the sig member of the stored document is the signature in the row's columns.
func signatureInDocument(doc map[string]any, r verifier.Receipt) bool {
	sig, ok := doc["sig"].(map[string]any)
	return ok && sig["key_id"] == r.SigKeyID && sig["value"] == r.SigValue
}

// documentIsTheRow holds when every field the signature binds equals the row's own column (D7, check 1).
func documentIsTheRow(doc map[string]any, r verifier.Receipt) bool {
	for field, want := range map[string]string{
		"receipt_id": r.ReceiptID, "tenant_id": r.TenantID, "grant_id": r.GrantID, "idempotency_key": r.IdempotencyKey,
		"logical_action": r.LogicalAction, "execution_state": r.State, "correlation_id": r.CorrelationID,
	} {
		if got, ok := doc[field].(string); !ok || got != want {
			return false
		}
	}
	if !transportResultIsTheRows(doc, r) || !acknowledgementIsTheRows(doc, r) {
		return false
	}
	attempt, ok := doc["attempt"].(json.Number)
	if !ok {
		return false
	}
	if n, err := attempt.Int64(); err != nil || n != r.Attempt {
		return false
	}
	at, ok := doc["dispatched_at"].(string)
	if !ok {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return false
	}
	// Execution signs the time to the nanosecond; the row holds a timestamptz, which PostgreSQL keeps to the microsecond, truncated
	// or rounded by how it was sent. So the row's time must be the document's time at that precision. The signature still binds the
	// exact value in the document, so nothing is loosened beyond the stored precision.
	return parsed.Equal(r.DispatchedAt) || parsed.Truncate(time.Microsecond).Equal(r.DispatchedAt) || parsed.Round(time.Microsecond).Equal(r.DispatchedAt)
}

// transportResultIsTheRows holds when the row's raw_status is the document's transport_result, followed by a raw code when there is one.
func transportResultIsTheRows(doc map[string]any, r verifier.Receipt) bool {
	tr, ok := doc["transport_result"].(string)
	return ok && tr != "" && (r.RawStatus == tr || strings.HasPrefix(r.RawStatus, tr+":"))
}

// acknowledgementIsTheRows holds when the document's external_ack and the row's are both absent, or both present and equal.
func acknowledgementIsTheRows(doc map[string]any, r verifier.Receipt) bool {
	switch ack := doc["external_ack"].(type) {
	case nil:
		return r.ExternalAck == nil
	case string:
		return r.ExternalAck != nil && *r.ExternalAck == ack
	default:
		return false
	}
}
