package understanding

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// overwrite writes the session's identity over the object (REQ-INP-002: the principal and tenant come only from the session, whatever the model service
// returned) and returns the names of the members that differed. Names only, never values (AGENT-13).
func overwrite(o *contracts.IntentObjectV3, s Session) []string {
	var changed []string
	set := func(name, was, now string) {
		if was != now {
			changed = append(changed, name)
		}
	}
	set("tenant_id", string(o.TenantId), s.TenantID)
	set("principal_id", string(o.Principal.PrincipalId), s.PrincipalID)
	set("authn_ref", string(o.Principal.AuthnRef), s.AuthnRef)
	// principal_type and asserted_by are the session's alone (ruling A166, item 3): the gate has already refused any value the model sent, so there is no override to report.
	o.TenantId = contracts.CommonV1TenantId(s.TenantID)
	o.Principal = contracts.IntentObjectV3Principal{PrincipalId: contracts.CommonV1PrincipalId(s.PrincipalID), PrincipalType: s.PrincipalType,
		AuthnRef: contracts.CommonV1SessionId(s.AuthnRef), AssertedBy: s.AssertedBy}
	return changed
}

// canonical is the RFC 8785 form of the object. platform/crypto refuses every non-integer number, so an object that carries a slot confidence or an ASR
// confidence has no canonical form here and the gate refuses it (ErrHashUnavailable); see the PR for the conflict.
func canonical(o contracts.IntentObjectV3) ([]byte, error) {
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("%wmarshal: %v", ErrHashUnavailable, err)
	}
	var generic any
	if err := decodeNumbers(raw, &generic); err != nil {
		return nil, err
	}
	out, err := crypto.CanonicalJSON(generic)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHashUnavailable, err)
	}
	return out, nil
}

func hashOf(canon []byte) string {
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func decodeNumbers(raw []byte, v *any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(v)
}
