package trustpack

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Remedies reads the capability an admitted intent's `requested_remedy` slot names.
type Remedies interface {
	RequestedRemedy(ctx context.Context, tenantID, intentID string, version int) (capabilityID string, present bool, err error)
}

// Capabilities implements intake.Capabilities.
type Capabilities struct{}

var _ intake.Capabilities = Capabilities{}

// NewCapabilities builds the adapter.
func NewCapabilities(p *Pack, r Remedies) (Capabilities, error) { return Capabilities{}, errNotBuilt }

// Requested answers T4.
func (c Capabilities) Requested(ctx context.Context, a intake.AdmittedIntent) (intake.Capability, bool, error) {
	return intake.Capability{}, false, errNotBuilt
}

// Check answers the registry's question for one capability and role.
func (c Capabilities) Check(ctx context.Context, capabilityID, role string) (intake.Capability, error) {
	return intake.Capability{}, errNotBuilt
}

// Authorized is the capability set of the action graph the intent's capability roots.
func (c Capabilities) Authorized(ctx context.Context, a intake.AdmittedIntent) ([]string, error) {
	return nil, errNotBuilt
}
