package grants

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// recordDomain is the domain label of a Decision Record v2 signature (rulings A5 and A13, 6 Nov 2026). The signed bytes are the label, one 0x00 byte
// and the RFC 8785 form of the record without sig.value, so a record's signature can never be a grant's or a confirmation's, which are over the bare
// JCS form. The sig.key_id stays in the signed bytes: a signature cannot be re-attributed to another key.
const recordDomain = "noema/decision-record/v2"

var (
	// ErrRecordVersion is returned when a record is not contract major 2: only v2 records are signed.
	ErrRecordVersion = errors.New("grants: only a decision-record major 2 is signed")
	// ErrRecordExpired is returned when a record is checked at a time outside its window: before issued_at or from expires_at on (ruling A13).
	ErrRecordExpired = errors.New("grants: the decision record is outside its window")
	// ErrRecordIncomplete is returned when a bound field is missing or malformed, or the record's window is empty (ruling A13: a record missing any
	// bound field is invalid, and is not signed).
	ErrRecordIncomplete = errors.New("grants: the decision record lacks a bound field or has an empty window")
)

func recordMessage(r contracts.DecisionRecordV2, keyID string) ([]byte, error) {
	if !strings.HasPrefix(string(r.SchemaVersion), "2.") {
		return nil, ErrRecordVersion
	}
	if r.IntentId == "" || r.CorrelationId == "" || !shapes["policy"].MatchString(r.PolicyVersion) || keyID == "" {
		return nil, ErrRecordIncomplete
	}
	issued, err1 := time.Parse(time.RFC3339, string(r.IssuedAt))
	expires, err2 := time.Parse(time.RFC3339, string(r.ExpiresAt))
	if err1 != nil || err2 != nil || !expires.After(issued) {
		return nil, ErrRecordIncomplete
	}
	r.Sig = contracts.CommonV1Sig{KeyId: keyID, Value: "-"} // the key_id is bound; the value is not part of what is signed
	body, err := canonicalWithSig(r)
	if err != nil {
		return nil, fmt.Errorf("grants: canonical decision record: %w", err)
	}
	msg := append([]byte(recordDomain), 0)
	return append(msg, body...), nil
}

// SignRecord returns the signature of a Decision Record v2 under the signing key: HMAC-SHA-256 over the domain label, a newline and the JCS form
// of the record without its sig (decision-record.v2). It does not read or change r.Sig, so signing a signed record gives the same value.
func (k *Keys) SignRecord(r contracts.DecisionRecordV2) (contracts.CommonV1Sig, error) {
	msg, err := recordMessage(r, k.signing)
	if err != nil {
		return contracts.CommonV1Sig{}, err
	}
	sig, err := k.ring.SignWith(k.signing, msg)
	if err != nil {
		return contracts.CommonV1Sig{}, fmt.Errorf("grants: sign decision record: %w", err)
	}
	return contracts.CommonV1Sig{KeyId: k.signing, Value: sig}, nil
}

// VerifyRecord checks the signature of a Decision Record v2 under the key it names, which must be active. Any failure is ErrSignature.
func (k *Keys) VerifyRecord(r contracts.DecisionRecordV2) error {
	msg, err := recordMessage(r, r.Sig.KeyId)
	if err != nil || !k.VerifyBytes(r.Sig, msg) {
		return ErrSignature
	}
	return nil
}

// VerifyRecordAt is VerifyRecord, and also refuses the record before its issued_at and from its expires_at on, on the clock the caller supplies
// (the simulated clock; ruling A13). The signature is checked first: a tampered record is a signature failure whatever the time, so the window
// is never read from a document that was not signed.
func (k *Keys) VerifyRecordAt(r contracts.DecisionRecordV2, now time.Time) error {
	if err := k.VerifyRecord(r); err != nil {
		return err
	}
	issued, err1 := time.Parse(time.RFC3339, string(r.IssuedAt))
	expires, err2 := time.Parse(time.RFC3339, string(r.ExpiresAt))
	if err1 != nil || err2 != nil || now.Before(issued) || !now.Before(expires) {
		return ErrRecordExpired
	}
	return nil
}

// BoundPolicy is the policy_id@version a record or a grant binds from a decision's basis, by the same rule as Issue (ruling D1): the exact policy named, the
// newest version of that policy only; with one policy in the basis it may be left empty.
func BoundPolicy(b decide.Basis, policyID string) (string, bool) {
	p, ok := boundPolicy(b, policyID)
	if !ok || !shapes["policy"].MatchString(p) {
		return "", false
	}
	return p, true
}
