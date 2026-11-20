package recorder

import (
	"fmt"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// NewConfirmationRequest is the confirmation request a CONFIRM decision creates (SoT 43.2a, REQ-TRU-009). The first eight values are the binding (ConfirmationBindingEvidenceHash is the
// content digest of A435, which the payload and the contract call evidence_set_hash); the writer computes the binding
// hash from them with trust/confirm.BindingHash and mints the cfr_ ID itself, so neither is ever taken from the caller. Requirement is the contract's approver requirement (SELF,
// MAKER_CHECKER or ROLE:<role>), RequesterID the principal that asked (the admitted intent's, INV-12) and ExpiresAt the expiry in Unix seconds.
type NewConfirmationRequest struct {
	PrincipalID                     string
	IntentHash                      string
	CapabilityID                    string
	TargetRef                       string
	ParamsHash                      string
	ConfirmationBindingEvidenceHash string
	PlanHash                        string
	PolicyVersion                   string
	Requirement                     string
	RequesterID                     string
	ExpiresAt                       int64
}

var (
	// requirementRe is the contract's ApproverRequirement (enums.v1.json): SELF, MAKER_CHECKER or ROLE:<role>.
	requirementRe = regexp.MustCompile(`^(?:SELF|MAKER_CHECKER|ROLE:[a-z][a-z0-9_]*)$`)
	// requesterRe is the character set of a bound identifier (the one trust/confirm accepts); the requester is not bound but it is recorded, so it holds the same.
	requesterRe = regexp.MustCompile(`^[A-Za-z0-9_.:@-]+$`)
)

// bindingOfRequest is the binding hash of the request's eight values (trust/confirm.BindingHash, SoT 43.2a). The writer computes it; a caller never supplies one.
func bindingOfRequest(c *NewConfirmationRequest) (string, error) {
	return confirm.BindingHash(confirm.BindingInput{PrincipalID: c.PrincipalID, IntentHash: c.IntentHash, CapabilityID: c.CapabilityID, TargetRef: c.TargetRef, ParamsHash: c.ParamsHash,
		EvidenceSetHash: c.ConfirmationBindingEvidenceHash, PlanHash: c.PlanHash, PolicyVersion: c.PolicyVersion})
}

// checkRequest refuses, before anything is written, a request that cannot be honoured: one on a decision that is not a CONFIRM (only a CONFIRM creates a request); a hash that is
// not in the contract's form; an intent hash other than the decision's own, or a policy version other than the composite of every policy version of the decision's basis
// (decide.CompositePolicyVersion, A341 interim; the later ALLOW recomputes the binding from those, so a request bound to others could never be accepted); a value the binding cannot carry; a requirement outside the contract's enum; no requester; and an expiry that is not
// after the writer's own clock (REQ-TRU-009).
func (w *Writer) checkRequest(r Request, basis decide.Basis) error {
	c := r.ConfirmationRequest
	composite, err := basis.CompositePolicyVersion()
	if err != nil {
		return fmt.Errorf("%w: the policy versions of the basis cannot be bound: %v", ErrRequest, err)
	}
	if r.Decision.Outcome() != decide.Confirm {
		return fmt.Errorf("%w: only a CONFIRM creates a confirmation request", ErrRequest)
	}
	switch {
	case !hashRe.MatchString(c.IntentHash), !hashRe.MatchString(c.ParamsHash), !hashRe.MatchString(c.ConfirmationBindingEvidenceHash), !hashRe.MatchString(c.PlanHash):
		return fmt.Errorf("%w: a bound hash of the confirmation request is not in the contract's form", ErrRequest)
	case c.IntentHash != string(r.IntentHash):
		return fmt.Errorf("%w: the confirmation request binds another intent hash than the decision", ErrRequest)
	case c.PolicyVersion != composite:
		return fmt.Errorf("%w: the confirmation request binds another policy version than the composite of the decision's policy versions", ErrRequest)
	case !requirementRe.MatchString(c.Requirement):
		return fmt.Errorf("%w: the approver requirement is not one of the contract's", ErrRequest)
	case !requesterRe.MatchString(c.RequesterID):
		return fmt.Errorf("%w: the requester is missing or not an identifier", ErrRequest)
	case c.ExpiresAt <= w.Clock.Now().Unix():
		return fmt.Errorf("%w: the confirmation request does not expire after now", ErrRequest)
	}
	if _, err := bindingOfRequest(c); err != nil {
		return fmt.Errorf("%w: the binding cannot be computed: %v", ErrRequest, err)
	}
	return nil
}

// confirmationRequested is the payload of CONFIRMATION_REQUESTED: everything the request holds, as identifiers, hashes and the expiry as a timestamp, so that the confirmation store
// can rebuild the request from this event and recompute the binding of an answer (SoT 43.2a). No display text and no amount is carried (REQ-AUD-003).
//
// The policy_version member is the policy version of the binding: the composite "sha256:<hex>" (decide.CompositePolicyVersion, A341 and A367), checked equal to the basis's by
// checkRequest. It is a content-hash typed value, which the payload guard (A184) exempts whole although its hex holds a run of more than four digits; the guard is not changed.
func confirmationRequested(c *NewConfirmationRequest, requestID, decisionID, binding string) map[string]any {
	return map[string]any{"confirmation_request_id": requestID, "decision_id": decisionID, "binding_hash": binding, "principal_id": c.PrincipalID, "intent_hash": c.IntentHash,
		"capability_id": c.CapabilityID, "target_ref": c.TargetRef, "params_hash": c.ParamsHash, "evidence_set_hash": c.ConfirmationBindingEvidenceHash, "plan_hash": c.PlanHash,
		"policy_version": c.PolicyVersion, "approver_requirement": c.Requirement, "requester_id": c.RequesterID, "expires_at": string(stamp(time.Unix(c.ExpiresAt, 0)))}
}
