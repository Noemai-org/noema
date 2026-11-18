package trustpack

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Scope implements intake.Scope (T5).
type Scope struct{}

var _ intake.Scope = Scope{}

// NewScope builds the adapter.
func NewScope(p *Pack) (Scope, error) { return Scope{}, errNotBuilt }

// InScope answers T5.
func (s Scope) InScope(ctx context.Context, tenantID, intentNode string) (bool, error) {
	return false, errNotBuilt
}

// Authority implements intake.Authority (T6).
type Authority struct{}

var _ intake.Authority = Authority{}

// NewAuthority builds the adapter.
func NewAuthority(p *Pack) (Authority, error) { return Authority{}, errNotBuilt }

// RoleAllowed answers T6.
func (a Authority) RoleAllowed(ctx context.Context, intentNode, role string) (bool, error) {
	return false, errNotBuilt
}
