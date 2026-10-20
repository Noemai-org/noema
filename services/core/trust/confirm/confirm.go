// Package confirm holds the confirmation contract (REQ-TRU-009, SoT §43.2a, INV-25): a CONFIRM decision creates a
// request bound by a hash to one exact action, and an approver's reply can lead to a fresh evaluation, never straight to a
// grant. An acceptance whose fresh binding differs from the request's produces a new decision, never a grant.
//
// Implements REQ-TRU-009.
//
// May import: the standard library.
//
// Must never: issue a grant, read the clock (the caller passes the event time), hold an expiry or a role name, or trust
// a principal that did not come from authentication.
package confirm

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

// BindingInput is the eight values a confirmation binds (SoT §43.2a). Every value is an identifier or a hash, so the
// canonical form needs no string escaping.
type BindingInput struct {
	PrincipalID     string
	IntentHash      string
	CapabilityID    string
	TargetRef       string
	ParamsHash      string
	EvidenceSetHash string
	PlanHash        string
	PolicyVersion   string
}

var safe = regexp.MustCompile(`^[A-Za-z0-9_.:@-]+$`)

// ErrUnsafeValue is returned when a bound value is empty or holds a character that would need escaping.
var ErrUnsafeValue = errors.New("confirm: a bound value is empty or holds a character outside A-Z a-z 0-9 _ . : @ -")

type pair struct{ key, value string }

// BindingHash is "sha256:" and the hex SHA-256 of the RFC 8785 canonical form of the eight values: an object with the
// keys in sorted order, no white space, string values. Restricting the values to a character set that JSON never
// escapes makes this canonical form byte-exact without a general canonicaliser.
func BindingHash(in BindingInput) (string, error) {
	pairs := []pair{
		{"capability_id", in.CapabilityID}, {"evidence_set_hash", in.EvidenceSetHash}, {"intent_hash", in.IntentHash},
		{"params_hash", in.ParamsHash}, {"plan_hash", in.PlanHash}, {"policy_version", in.PolicyVersion},
		{"principal_id", in.PrincipalID}, {"target_ref", in.TargetRef},
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range pairs {
		if !safe.MatchString(p.value) {
			return "", ErrUnsafeValue
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + p.key + `":"` + p.value + `"`)
	}
	b.WriteByte('}')
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Request is the bound, expiring record a CONFIRM decision creates.
type Request struct {
	ID          string
	Binding     BindingInput
	Requirement string // SELF, MAKER_CHECKER or ROLE:<role>
	RequesterID string
	ExpiresAt   int64
}

// Response is the approver's reply.
type Response string

// The two replies.
const (
	Accept  Response = "ACCEPT"
	Decline Response = "DECLINE"
)

// Event is a confirmation event. ApproverID and ApproverRoles come from authentication, never from the request body.
type Event struct {
	RequestID     string
	ApproverID    string
	ApproverRoles []string
	Response      Response
	At            int64
}

// Resolution is what the contract allows to happen next.
type Resolution string

// The resolutions. Only Proceed leads towards a grant, and only through a fresh evaluation.
const (
	Proceed        Resolution = "PROCEED_TO_FRESH_EVALUATION"
	Declined       Resolution = "DECLINED" // REJECT DECLINED_BY_APPROVER
	Expired        Resolution = "EXPIRED"  // CONFIRMATION_EXPIRED
	NotEligible    Resolution = "APPROVER_NOT_ELIGIBLE"
	BindingChanged Resolution = "BINDING_CHANGED" // a new decision, never a grant
	PolicyChanged  Resolution = "POLICY_VERSION_MISMATCH"
	WrongRequest   Resolution = "WRONG_REQUEST"
)

const (
	requirementSelf  = "SELF"
	requirementMaker = "MAKER_CHECKER"
	rolePrefix       = "ROLE:"
)

func eligible(req Request, ev Event) bool {
	switch {
	case req.Requirement == requirementSelf:
		return ev.ApproverID == req.RequesterID
	case req.Requirement == requirementMaker:
		return ev.ApproverID != req.RequesterID && ev.ApproverID != ""
	case strings.HasPrefix(req.Requirement, rolePrefix):
		role := strings.TrimPrefix(req.Requirement, rolePrefix)
		for _, r := range ev.ApproverRoles {
			if r == role {
				return true
			}
		}
	}
	return false
}

// Resolve applies a confirmation event to its request. fresh is the binding recomputed by the fresh evaluation that an
// ACCEPT triggers. The order is fixed: the request, then expiry, then eligibility, then the reply, then the binding.
func Resolve(req Request, ev Event, fresh BindingInput) (Resolution, error) {
	switch {
	case ev.RequestID != req.ID:
		return WrongRequest, nil
	case ev.At > req.ExpiresAt:
		return Expired, nil
	case !eligible(req, ev):
		return NotEligible, nil
	case ev.Response == Decline:
		return Declined, nil
	case ev.Response != Accept:
		return WrongRequest, nil
	}
	want, err := BindingHash(req.Binding)
	if err != nil {
		return "", err
	}
	got, err := BindingHash(fresh)
	if err != nil {
		return "", err
	}
	switch {
	case got == want:
		return Proceed, nil
	case fresh.PolicyVersion != req.Binding.PolicyVersion:
		return PolicyChanged, nil
	}
	return BindingChanged, nil
}

// MayLeadToGrant is true only for Proceed. Even then a grant comes only from a new ALLOW evaluation (INV-25).
func (r Resolution) MayLeadToGrant() bool { return r == Proceed }
