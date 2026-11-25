package trustpack

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// HandoffRoute is one handoff route of the pack: a reason code the pack routes to an owner queue, for every intent node or for the nodes it lists (pack.v1 handoff_routes).
type HandoffRoute struct {
	ReasonCode  string
	OwnerQueue  string
	IntentNodes []string
}

// HandoffRoutes is the pack's handoff routes (A551, A684; read-only). Stub: the implementation follows the red.
func (p *Pack) HandoffRoutes() ([]HandoffRoute, error) {
	return nil, nil
}

// Handoffs implements intake.Handoffs over the pack's handoff routes. Stub: the implementation follows the red.
type Handoffs struct{ pack *Pack }

var _ intake.Handoffs = Handoffs{}

// NewHandoffs builds the adapter. A nil pack is ErrConfig.
func NewHandoffs(p *Pack) (Handoffs, error) {
	if !p.usable() {
		return Handoffs{}, ErrConfig
	}
	return Handoffs{pack: p}, nil
}

// Routed answers whether the pack routes the reason to a handoff for the intent node. Stub.
func (h Handoffs) Routed(ctx context.Context, q intake.HandoffQuery) (bool, error) {
	return false, nil
}
