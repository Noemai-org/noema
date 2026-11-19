package recorder

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Confirmation is an accepted confirmation that resolved to a fresh evaluation (trust/confirm: PROCEED_TO_FRESH_EVALUATION), as the confirmation store records it. BindingHash is the hash the
// request was given for: confirm.BindingHash of the eight values of SoT 43.2a, computed when the request was created. ExpiresAt is the request's expiry. A declined, expired or mismatched
// answer is never recorded here: it does not lead to an ALLOW, so there is nothing to spend.
type Confirmation struct {
	TenantID    string
	EventID     string // cfe_...
	RequestID   string // cfr_...
	BindingHash string // sha256:...
	AcceptedAt  time.Time
	ExpiresAt   time.Time
}

var requestRe = regexp.MustCompile(`^cfr_[0-9A-Za-z]{2,32}$`)

// RecordConfirmation inserts c in tx, as the Trust login of c's tenant (the database refuses another tenant). Each confirmation event, and each request, is recorded once, and the row is never
// changed. It never commits or rolls back; on any error the caller must roll back. ErrRequest is returned, before anything is written, for a field that is not in the contract's form.
func RecordConfirmation(ctx context.Context, tx pgx.Tx, c Confirmation) error {
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("%sthe context needs a deadline", errPrefix)
	}
	switch {
	case !tenantRe.MatchString(c.TenantID), !confirmRe.MatchString(c.EventID), !requestRe.MatchString(c.RequestID), !hashRe.MatchString(c.BindingHash),
		c.AcceptedAt.IsZero(), c.ExpiresAt.IsZero(), c.ExpiresAt.Before(c.AcceptedAt):
		return ErrRequest
	}
	if _, err := tx.Exec(ctx, `INSERT INTO confirmation_resolution (tenant_id, confirmation_event_id, confirmation_request_id, binding_hash, accepted_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, c.TenantID, c.EventID, c.RequestID, c.BindingHash, c.AcceptedAt.UTC(), c.ExpiresAt.UTC()); err != nil {
		return fmt.Errorf("%srecord the confirmation: %w", errPrefix, err)
	}
	return nil
}

// bindingOf is the binding the ALLOW's own step has, in the eight values of SoT 43.2a. A confirmation approves one action, so there is exactly one step (checked by the caller). The policy
// version is the composite of every policy version of the decision's basis (decide.CompositePolicyVersion, A341 interim), the same value the request was created with; the policy a grant
// binds (grants.BoundPolicy) is not it.
func bindingOf(r Request, basis decide.Basis) (string, error) {
	policy, err := basis.CompositePolicyVersion()
	if err != nil {
		return "", err
	}
	s := r.Steps[0]
	return confirm.BindingHash(confirm.BindingInput{PrincipalID: s.PrincipalID, IntentHash: string(r.IntentHash), CapabilityID: s.CapabilityID, TargetRef: s.TargetRef,
		ParamsHash: string(s.ParamsHash), EvidenceSetHash: string(s.EvidenceSetHash), PlanHash: string(s.PlanHash), PolicyVersion: policy})
}
