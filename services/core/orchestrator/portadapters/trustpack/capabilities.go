package trustpack

import (
	"context"
	"fmt"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Remedies reads the capability an admitted intent's `requested_remedy` slot names, from the intent store, as the intent's own tenant. present is false when the intent names none
// (the slot is absent). The value is returned as the store holds it; whether it names a registered capability is this adapter's question, not the store's. The intake's
// AdmittedIntent carries no slot value, so the composition root implements this over the same store as intake.Intents (EL's lane; not built here).
type Remedies interface {
	RequestedRemedy(ctx context.Context, tenantID, intentID string, version int) (capabilityID string, present bool, err error)
}

// Capabilities implements intake.Capabilities over the pack: the registry answers whether a capability exists and is prohibited, the descriptor's roles answer whether a role may
// use it (A255), and the pack's action graphs answer which capabilities an intent's capability authorizes (A306). The zero value refuses every call with ErrConfig.
type Capabilities struct {
	pack     *Pack
	remedies Remedies
}

var _ intake.Capabilities = Capabilities{}

// NewCapabilities builds the adapter. main.go passes the Pack from LoadPack and a Remedies over the intent store. A nil pack or a nil port is ErrConfig.
func NewCapabilities(p *Pack, r Remedies) (Capabilities, error) {
	if !p.usable() || r == nil {
		return Capabilities{}, ErrConfig
	}
	return Capabilities{pack: p, remedies: r}, nil
}

func (c Capabilities) usable(ctx context.Context) error {
	if !c.pack.usable() || c.remedies == nil {
		return ErrConfig
	}
	return ctx.Err()
}

func intentUsable(a intake.AdmittedIntent) error {
	if a.TenantID == "" || a.IntentID == "" || a.Version < 1 {
		return ErrIntent
	}
	return nil
}

func (c Capabilities) describe(id string) intake.Capability {
	known, prohibited := c.pack.capability(id)
	return intake.Capability{ID: id, Registered: known, Prohibited: prohibited}
}

// requested reads the remedy of the admitted intent from the store, for its own tenant, ID and version and no other.
func (c Capabilities) requested(ctx context.Context, a intake.AdmittedIntent) (id string, present bool, err error) {
	if err := intentUsable(a); err != nil {
		return "", false, err
	}
	id, present, err = c.remedies.RequestedRemedy(ctx, a.TenantID, a.IntentID, a.Version)
	if err != nil {
		return "", false, fmt.Errorf("trustpack: read the requested remedy: %w", err)
	}
	return id, present, nil
}

// Requested answers T4: the capability the intent's `requested_remedy` slot names, with whether the registry knows it and whether it is prohibited. A prohibited capability is
// registered and prohibited; a capability the registry does not know is not registered (it has no adapter, INV-18). Both are refused by the intake at T4. RoleAllowed is not set
// here: T6 has checked the role for the intent node. A failed read is an error, never "none".
func (c Capabilities) Requested(ctx context.Context, a intake.AdmittedIntent) (intake.Capability, bool, error) {
	if err := c.usable(ctx); err != nil {
		return intake.Capability{}, false, err
	}
	id, present, err := c.requested(ctx, a)
	if err != nil || !present {
		return intake.Capability{}, false, err
	}
	return c.describe(id), true, nil
}

// Check is the registry's answer for one capability ID and the session's role (A255). RoleAllowed is true only for a registered capability that is not prohibited and whose
// descriptor lists the role, exactly (no case folding, no trimming; an empty role is never listed). A prohibited capability is never allowed, whatever its descriptor lists
// (BD-08: no adapter, no callable path); an unknown capability is not registered.
func (c Capabilities) Check(ctx context.Context, capabilityID, role string) (intake.Capability, error) {
	if err := c.usable(ctx); err != nil {
		return intake.Capability{}, err
	}
	cp := c.describe(capabilityID)
	if cp.Registered && !cp.Prohibited && role != "" {
		_, cp.RoleAllowed = c.pack.capRoles[capabilityID][role]
	}
	return cp, nil
}

// Authorized is the set of capability IDs of the pack action graphs the intent's capability roots: every node that depends, directly or not, on a root node whose capability is the
// intent's, reads included, over every graph that declares such a root, sorted (A306, A319: pack data, no ID in the core). The intake adds the intent's own capability.
//
// Which answers are an error and which are empty (interim, for the Architect, the safer reading):
//   - a capability the registry does not know, or a prohibited one, roots no authorization: ErrCapability, so the intake ends in UNKNOWN. It is never "none" and never a set.
//     T4 refuses such a capability first (REJECT), so this is the second wall.
//   - a registered capability that roots no graph, and an intent that requests no capability, authorize nothing beyond the intent's own capability: an empty set and no error.
//     That is the narrowest answer. T14 then refuses any plan step outside the intent's own capability (PLAN_INVALID), so nothing is widened.
//   - a failed read of the remedy, or a nil pack, is an error.
//
// The caller gets its own copy.
func (c Capabilities) Authorized(ctx context.Context, a intake.AdmittedIntent) ([]string, error) {
	if err := c.usable(ctx); err != nil {
		return nil, err
	}
	id, present, err := c.requested(ctx, a)
	if err != nil {
		return nil, err
	}
	if !present {
		return []string{}, nil
	}
	if known, prohibited := c.pack.capability(id); !known || prohibited {
		return nil, fmt.Errorf("%w: the intent's capability is unknown or prohibited", ErrCapability)
	}
	return append([]string{}, c.pack.rooted[id]...), nil
}
