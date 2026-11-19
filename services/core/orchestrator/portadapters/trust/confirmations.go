package trustport

import (
	"context"
	"fmt"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// ConfirmationProfile says whether the trust profile asks for an approver confirmation for the request (T13).
type ConfirmationProfile interface {
	Requires(ctx context.Context, a intake.AdmittedIntent) (bool, error)
}

// ConfirmationRecord is the newest resolved confirmation the store holds for one intent of one principal, as plain values.
type ConfirmationRecord struct {
	TenantID string
	EventID  string // cfe_...
	Request  confirm.Request
	Event    confirm.Event
	Fresh    confirm.BindingInput
}

// ConfirmationReader reads the confirmation store as the tenant. found is false when there is none.
type ConfirmationReader interface {
	Latest(ctx context.Context, tenantID, principalID, intentID string, version int) (rec ConfirmationRecord, found bool, err error)
}

// Confirmations implements intake.Confirmations over trust/confirm. It decides nothing the contract does not: Given only for confirm.Proceed (INV-25), and every doubt is
// Pending or an error, never Given (INV-17).
type Confirmations struct {
	profile ConfirmationProfile
	reader  ConfirmationReader
	clk     clock.Clock
}

var _ intake.Confirmations = Confirmations{}

// NewConfirmations builds the adapter. A missing profile, reader or clock is ErrConfig.
func NewConfirmations(p ConfirmationProfile, r ConfirmationReader, c clock.Clock) (Confirmations, error) {
	if p == nil || r == nil || c == nil {
		return Confirmations{}, ErrConfig
	}
	return Confirmations{profile: p, reader: r, clk: c}, nil
}

// Status answers T13. Tenant, principal and intent hash are the admitted intent's (INV-12): the store is asked with them, and the fresh binding is built with them.
// The fresh binding's plan hash is the request's own: T14 compares the plan it builds with the Given answer's PlanHash (A230).
func (c Confirmations) Status(ctx context.Context, a intake.AdmittedIntent) (intake.Confirmation, error) {
	if c.profile == nil || c.reader == nil || c.clk == nil {
		return intake.Confirmation{}, ErrConfig
	}
	if a.TenantID == "" || a.PrincipalID == "" || a.IntentID == "" {
		return intake.Confirmation{}, ErrSession
	}
	required, err := c.profile.Requires(ctx, a)
	if err != nil {
		return intake.Confirmation{}, fmt.Errorf("trustport: read the confirmation profile: %w", err)
	}
	if !required {
		return intake.Confirmation{Status: intake.ConfirmationNotRequired}, nil
	}
	rec, found, err := c.reader.Latest(ctx, a.TenantID, a.PrincipalID, a.IntentID, a.Version)
	if err != nil {
		return intake.Confirmation{}, fmt.Errorf("trustport: read the confirmation store: %w", err)
	}
	if !found {
		return pending(), nil
	}
	if rec.TenantID != a.TenantID {
		return intake.Confirmation{}, fmt.Errorf("%w: a confirmation of another tenant", ErrBadAnswer)
	}
	if rec.EventID == "" {
		return intake.Confirmation{}, fmt.Errorf("%w: a confirmation with no event", ErrBadAnswer)
	}
	b := rec.Request.Binding
	if b.PrincipalID != a.PrincipalID || rec.Request.RequesterID != a.PrincipalID || b.IntentHash != a.IntentHash {
		return pending(), nil
	}
	if c.clk.Now().Unix() > rec.Request.ExpiresAt {
		return pending(), nil
	}
	fresh := rec.Fresh
	fresh.PrincipalID, fresh.IntentHash, fresh.PlanHash = a.PrincipalID, a.IntentHash, b.PlanHash
	res, err := confirm.Resolve(rec.Request, rec.Event, fresh)
	if err != nil {
		return intake.Confirmation{}, fmt.Errorf("%w: resolve the confirmation: %v", ErrBadAnswer, err)
	}
	if !res.MayLeadToGrant() {
		return pending(), nil
	}
	return intake.Confirmation{Status: intake.ConfirmationGiven, EventID: rec.EventID, PlanHash: b.PlanHash}, nil
}

func pending() intake.Confirmation { return intake.Confirmation{Status: intake.ConfirmationPending} }
