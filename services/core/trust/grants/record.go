package grants

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// recordDomain is the domain label of a Decision Record v2 signature (ruling A5, 6 Nov 2026). The signed bytes are the label, a newline and the RFC 8785
// form of the record without its sig, so a record's signature can never be a grant's or a confirmation's, which are over the bare JCS form.
const recordDomain = "noema/decision-record/v2"

// ErrRecordVersion is returned when a record is not contract major 2: only v2 records are signed.
var ErrRecordVersion = errors.New("grants: only a decision-record major 2 is signed")

func recordMessage(r contracts.DecisionRecordV2) ([]byte, error) {
	if !strings.HasPrefix(string(r.SchemaVersion), "2.") {
		return nil, ErrRecordVersion
	}
	body, err := canonicalOf(r)
	if err != nil {
		return nil, fmt.Errorf("grants: canonical decision record: %w", err)
	}
	return append([]byte(recordDomain+"\n"), body...), nil
}

// SignRecord returns the signature of a Decision Record v2 under the signing key: HMAC-SHA-256 over the domain label, a newline and the JCS form
// of the record without its sig (decision-record.v2). It does not read or change r.Sig, so signing a signed record gives the same value.
func (k *Keys) SignRecord(r contracts.DecisionRecordV2) (contracts.CommonV1Sig, error) {
	msg, err := recordMessage(r)
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
	msg, err := recordMessage(r)
	if err != nil || !k.VerifyBytes(r.Sig, msg) {
		return ErrSignature
	}
	return nil
}
