package trustport

import (
	"context"

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

// Confirmations implements intake.Confirmations (stub).
type Confirmations struct {
	profile ConfirmationProfile
	reader  ConfirmationReader
	clk     clock.Clock
}

var _ intake.Confirmations = Confirmations{}

// NewConfirmations builds the adapter (stub).
func NewConfirmations(p ConfirmationProfile, r ConfirmationReader, c clock.Clock) (Confirmations, error) {
	return Confirmations{profile: p, reader: r, clk: c}, nil
}

// Status answers T13 (stub).
func (c Confirmations) Status(_ context.Context, _ intake.AdmittedIntent) (intake.Confirmation, error) {
	return intake.Confirmation{}, nil
}
