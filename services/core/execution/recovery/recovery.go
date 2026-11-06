package recovery

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

const errPrefix = "recovery: "

var reasonRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Request is a RecoveryRequest (recovery-request.v1): a verdict other than ACHIEVED, or a voided grant, addressed to Trust.
// Exactly one of VerdictID and GrantID is set.
type Request struct {
	TenantID, CorrelationID, RecoveryRequestID string
	Reason                                     string // an upper-case code such as POSTCONDITION_UNVERIFIED
	VerdictID, GrantID                         string
	EvidenceRefs                               []string
	AttemptCount                               int64
}

func (r Request) validate() error {
	switch {
	case r.TenantID == "" || r.CorrelationID == "" || r.RecoveryRequestID == "":
		return errors.New(errPrefix + "tenant, correlation and recovery request IDs are required")
	case !reasonRe.MatchString(r.Reason):
		return fmt.Errorf("%sreason %q is not an upper-case code", errPrefix, r.Reason)
	case (r.VerdictID == "") == (r.GrantID == ""):
		return errors.New(errPrefix + "exactly one of verdict_id and grant_id is required")
	case r.AttemptCount < 0:
		return errors.New(errPrefix + "attempt_count is negative")
	}
	return nil
}

// Decision is what Trust answers: the ID of the Decision Record it wrote and its outcome (the seven outcomes of REQ-TRU-001).
type Decision struct{ DecisionID, Outcome string }

// Trust is the Trust evaluation of a RecoveryRequest, a fresh evaluation that re-reads evidence, reconciles, checks policy and decides
// with the recovery table (PERD 30.2). The production implementation is the Temporal recovery activity that calls Trust; this package
// never imports Trust or Outcome (INV-15). It has no method that dispatches.
type Trust interface {
	Evaluate(ctx context.Context, r Request) (Decision, error)
}

// Alert is an ops alert raised because Trust could not decide. It carries identifiers and codes only, never evidence.
type Alert struct {
	TenantID, CorrelationID, RecoveryRequestID string
	Reason                                     string
	Cause                                      string // TRUST_FAILED, TRUST_UNKNOWN or TRUST_UNRECOGNISED
}

// Alerts delivers an ops alert. Where it goes in the POC is an open question for the Ops Lead.
type Alerts interface {
	OpsAlert(ctx context.Context, a Alert) error
}

// Action is what recovery tells its caller to do next. Recovery starts no write: a RetryAuthorised result is only Trust's ALLOW, and
// the caller still needs a new grant for it (REQ-EXE-006, AGENT-08).
type Action string

const (
	RetryAuthorised Action = "RETRY_AUTHORISED"
	Escalate        Action = "ESCALATE"
)

// Result is the outcome of one recovery evaluation.
type Result struct {
	Action     Action
	DecisionID string // Trust's Decision Record; empty when Trust gave none that can be relied on
}

// Handler evaluates RecoveryRequests through Trust.
type Handler struct {
	Trust  Trust
	Alerts Alerts
}

// Handle asks Trust to evaluate r afresh. Trust's ALLOW is the only way to RetryAuthorised. A Trust error, panic, UNKNOWN, an
// outcome that is not one of the seven, or an ALLOW with no decision is UNKNOWN: ESCALATE with an ops alert (REQ-RCV-001, PERD 30.2).
// Any other Trust outcome escalates without an alert, because Trust recorded why. If the alert cannot be delivered the result is still
// ESCALATE and the delivery error is returned. ctx needs a deadline.
func (h Handler) Handle(ctx context.Context, r Request) (res Result, err error) {
	if _, ok := ctx.Deadline(); !ok {
		return Result{}, errors.New(errPrefix + "the context needs a deadline")
	}
	if h.Trust == nil || h.Alerts == nil {
		return Result{}, errors.New(errPrefix + "Trust and Alerts are required")
	}
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	d, cause := h.evaluate(ctx, r)
	switch {
	case cause != "":
		return h.escalateWithAlert(ctx, r, cause)
	case d.Outcome == "ALLOW":
		return Result{Action: RetryAuthorised, DecisionID: d.DecisionID}, nil
	default:
		return Result{Action: Escalate, DecisionID: d.DecisionID}, nil
	}
}

// known are the seven decision outcomes of REQ-TRU-001.
var known = map[string]bool{"ALLOW": true, "CONFIRM": true, "CLARIFY": true, "WAIT": true, "REJECT": true, "ESCALATE": true, "UNKNOWN": true}

// evaluate calls Trust and returns its decision, or the cause of UNKNOWN: a failure, a panic, UNKNOWN itself, or an answer that cannot be trusted.
func (h Handler) evaluate(ctx context.Context, r Request) (d Decision, cause string) {
	defer func() {
		if recover() != nil {
			d, cause = Decision{}, "TRUST_FAILED"
		}
	}()
	d, err := h.Trust.Evaluate(ctx, r)
	switch {
	case err != nil:
		return Decision{}, "TRUST_FAILED"
	case d.Outcome == "UNKNOWN":
		return Decision{}, "TRUST_UNKNOWN"
	case !known[d.Outcome] || d.DecisionID == "":
		return Decision{}, "TRUST_UNRECOGNISED"
	}
	return d, ""
}

func (h Handler) escalateWithAlert(ctx context.Context, r Request, cause string) (Result, error) {
	res := Result{Action: Escalate}
	if err := h.Alerts.OpsAlert(ctx, Alert{TenantID: r.TenantID, CorrelationID: r.CorrelationID, RecoveryRequestID: r.RecoveryRequestID, Reason: r.Reason, Cause: cause}); err != nil {
		return res, fmt.Errorf("%sthe ops alert for %s was not delivered: %w", errPrefix, r.RecoveryRequestID, err)
	}
	return res, nil
}
