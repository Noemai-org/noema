package reconcile

import "errors"

// ErrFrozen is returned when a capability is not allowed on a frozen target (REQ-RES-002).
var ErrFrozen = errors.New("reconcile: the target is frozen for this capability")

// FreezeGate knows which capabilities a pack allows while a target is frozen by CONTRADICTORY_STATE. The list is pack
// data (the descriptor's allowed_under_freeze); this package holds none.
type FreezeGate struct{ allowed map[string]bool }

// NewFreezeGate builds a gate from the capability IDs the pack allows under a freeze.
func NewFreezeGate(allowed []string) FreezeGate {
	g := FreezeGate{allowed: map[string]bool{}}
	for _, id := range allowed {
		g.allowed[id] = true
	}
	return g
}

// Permission is the proof that a grant may be built for a capability on a target. Its only constructor is
// FreezeGate.Permit, its fields are unexported, and its zero value is invalid, so a grant constructor that requires a
// Permission cannot be called for a frozen target and a non-allowed capability.
type Permission struct {
	capability string
	valid      bool
}

// Permit returns a Permission unless the target is frozen and the capability is not allowed under a freeze.
func (g FreezeGate) Permit(frozen bool, capability string) (Permission, error) {
	if capability == "" || (frozen && !g.allowed[capability]) {
		return Permission{}, ErrFrozen
	}
	return Permission{capability: capability, valid: true}, nil
}

// Valid reports whether the Permission came from Permit.
func (p Permission) Valid() bool { return p.valid }

// Capability is the capability the Permission was issued for.
func (p Permission) Capability() string { return p.capability }
