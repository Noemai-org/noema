package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// Code is an error code of /contracts/schemas/errors.v2.json.
type Code string

// The codes this package returns. Callers match on the code through errors.As, never on the message.
const (
	CodeProhibited    Code = "CAPABILITY_PROHIBITED"
	CodeNotRegistered Code = "CAPABILITY_NOT_REGISTERED"
	CodeIncomplete    Code = "SCHEMA_INVALID"
	CodeSafetyBlock   Code = "INTERNAL_SAFETY_BLOCK"
)

// Error is the typed error of the registry. Reason is for people and logs; it names an ID or a field, never a payload.
type Error struct {
	Code   Code
	Reason string
}

func (e *Error) Error() string { return "registry: " + string(e.Code) + ": " + e.Reason }

func refuse(c Code, format string, args ...any) *Error {
	return &Error{Code: c, Reason: fmt.Sprintf(format, args...)}
}

// monetaryWriteClass is the risk-class prefix of a capability that moves money (SoT §34.2). A descriptor of this class
// that is not prohibited cannot be registered (INV-05, BD-08).
const monetaryWriteClass = "R4"

// Registry is the set of capability descriptors of one pack. It is built once, read-only after that, and safe to share.
type Registry struct {
	byID map[string]contracts.CapabilityDescriptorV1
	ids  []string
}

// Callable is a handle to an allowed capability. It can be obtained only from Registry.Callable, which refuses every
// prohibited and unregistered ID, so a prohibited capability has no value of this type (REQ-CAP-002, INV-18). The zero
// value names nothing.
type Callable struct{ id string }

// ID is the capability ID the handle was issued for; empty for the zero value.
func (c Callable) ID() string { return c.id }

// Load decodes a domain pack from r (strictly, through the generated contract type) and builds its registry. The pack
// is data; nothing in it is executed (SoT §51).
func Load(r io.Reader) (*Registry, error) {
	dec := json.NewDecoder(r)
	var p contracts.PackV1
	if err := dec.Decode(&p); err != nil {
		return nil, refuse(CodeIncomplete, "the pack does not decode: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, refuse(CodeIncomplete, "the pack has data after its end")
	}
	return New(p.Capabilities)
}

// New builds a registry from descriptors. It refuses the whole set, so that no partial registry exists, when:
//   - the set is empty or two descriptors share an ID;
//   - a descriptor lacks any field (REQ-CAP-001: complete descriptors only);
//   - a monetary-write descriptor is not marked prohibited (INV-05).
func New(ds []contracts.CapabilityDescriptorV1) (*Registry, error) {
	if len(ds) == 0 {
		return nil, refuse(CodeIncomplete, "no capability descriptors")
	}
	reg := &Registry{byID: make(map[string]contracts.CapabilityDescriptorV1, len(ds))}
	for _, d := range ds {
		id := string(d.CapabilityId)
		if _, dup := reg.byID[id]; dup {
			return nil, refuse(CodeIncomplete, "capability %s is registered twice", id)
		}
		if missing := missingField(d); missing != "" {
			return nil, refuse(CodeIncomplete, "capability %s has no %s", id, missing)
		}
		if strings.HasPrefix(d.RiskClass, monetaryWriteClass) && !d.Prohibited {
			return nil, refuse(CodeSafetyBlock, "capability %s is a monetary write and is not marked prohibited", id)
		}
		reg.byID[id] = d
		reg.ids = append(reg.ids, id)
	}
	sort.Strings(reg.ids)
	return reg, nil
}

// missingField names the first descriptor field that is absent, or returns "". Every field of the capability descriptor
// is required of a registered capability, prohibited or not (SoT §43.8).
func missingField(d contracts.CapabilityDescriptorV1) string {
	switch {
	case d.CapabilityId == "":
		return "capability_id"
	case d.BusinessMeaning == "":
		return "business_meaning"
	case d.RiskClass == "":
		return "risk_class"
	case d.InputSchema == nil:
		return "input_schema"
	case d.Preconditions == nil:
		return "preconditions"
	case d.Authority == nil:
		return "authority"
	case d.PolicyTemplates == nil:
		return "policy_templates"
	case d.SideEffectClass == nil:
		return "side_effect_class"
	case d.BlastRadius == nil:
		return "blast_radius"
	case d.Idempotency == nil:
		return "idempotency"
	case d.RetrySafety == nil:
		return "retry_safety"
	case d.ConcurrencyControl == nil:
		return "concurrency_control"
	case d.RevalidationReads == nil:
		return "revalidation_reads"
	case d.ConsequenceProfile == nil:
		return "consequence_profile"
	case d.ExpectedOutcome == nil:
		return "expected_outcome"
	case d.Verification == nil:
		return "verification"
	case d.Retry == nil:
		return "retry"
	case d.Recovery == nil:
		return "recovery"
	case d.Compensation == nil:
		return "compensation"
	case d.Audit == nil:
		return "audit"
	case d.Limits == nil:
		return "limits"
	}
	return ""
}

// Callable returns the handle for an allowed capability. A prohibited capability gives CAPABILITY_PROHIBITED and an
// unknown ID gives CAPABILITY_NOT_REGISTERED; in both cases the handle is the zero value. IDs match exactly: no case
// folding, no trimming.
func (r *Registry) Callable(id string) (Callable, error) {
	d, ok := r.byID[id]
	switch {
	case !ok:
		return Callable{}, refuse(CodeNotRegistered, "%q is not registered", id)
	case d.Prohibited:
		return Callable{}, refuse(CodeProhibited, "%s is prohibited", id)
	}
	return Callable{id: id}, nil
}

// Registered reports whether id is an allowed (registered, not prohibited) capability.
func (r *Registry) Registered(id string) bool {
	d, ok := r.byID[id]
	return ok && !d.Prohibited
}

// IsProhibited reports whether id is a registered prohibited capability. Trust uses it to reject at T4.
func (r *Registry) IsProhibited(id string) bool {
	d, ok := r.byID[id]
	return ok && d.Prohibited
}

// IDs returns every registered capability ID, allowed and prohibited, sorted.
func (r *Registry) IDs() []string { return append([]string(nil), r.ids...) }

// Prohibited returns the IDs of the prohibited capabilities, sorted.
func (r *Registry) Prohibited() []string {
	var out []string
	for _, id := range r.ids {
		if r.byID[id].Prohibited {
			out = append(out, id)
		}
	}
	return out
}
