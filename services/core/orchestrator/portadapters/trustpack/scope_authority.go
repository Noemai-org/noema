package trustpack

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Scope implements intake.Scope (T5) over the pack's intent nodes. The zero value refuses every call with ErrConfig.
//
// Interim reading, for the Architect: the pack holds no per-tenant list of nodes and no tenant has another pack, so scope is the pack's: an intent node is in scope when the pack has
// the node and gives it a trust profile. A node with no trust profile (the pack's node for a request outside its scope) has no authority, no policy and no clocks for Trust to
// evaluate, and is out of scope. The answer is the same for every tenant; a tenant-specific narrowing needs data that does not exist yet.
type Scope struct{ pack *Pack }

var _ intake.Scope = Scope{}

// NewScope builds the adapter. A nil pack is ErrConfig.
func NewScope(p *Pack) (Scope, error) {
	if !p.usable() {
		return Scope{}, ErrConfig
	}
	return Scope{pack: p}, nil
}

// InScope answers T5. A question about no tenant is ErrSession; an unknown or empty node is not in scope.
func (s Scope) InScope(ctx context.Context, tenantID, intentNode string) (bool, error) {
	if !s.pack.usable() {
		return false, ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tenantID == "" {
		return false, ErrSession
	}
	return s.pack.nodes[intentNode].profiled, nil
}

// Authority implements intake.Authority (T6) over the pack's trust profiles: the session's role must be in the `roles_allowed` of the trust profile of the intent node. The zero value
// refuses every call with ErrConfig.
type Authority struct{ pack *Pack }

var _ intake.Authority = Authority{}

// NewAuthority builds the adapter. A nil pack is ErrConfig.
func NewAuthority(p *Pack) (Authority, error) {
	if !p.usable() {
		return Authority{}, ErrConfig
	}
	return Authority{pack: p}, nil
}

// RoleAllowed answers T6. The role is matched exactly (no case folding, no trimming) and an empty role is never allowed. A node with no trust profile, an unknown node and an empty
// node allow no role; a profile that lists no role allows none.
func (a Authority) RoleAllowed(ctx context.Context, intentNode, role string) (bool, error) {
	if !a.pack.usable() {
		return false, ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	n := a.pack.nodes[intentNode]
	if !n.profiled || role == "" {
		return false, nil
	}
	_, ok := n.roles[role]
	return ok, nil
}
