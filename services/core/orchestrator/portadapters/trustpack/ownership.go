package trustpack

import (
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// nodeOwnership is what the pack says about the ownership of one intent node: the slots its trust profile says the principal must own (`authority.principal_must_own`), and, for
// each slot of the node, its type and resource kind. It is read at load and judged by OwnershipKinds, so the load rules are unchanged.
type nodeOwnership struct {
	mustOwn []string
	slots   map[string]ownedSlot
}

type ownedSlot struct {
	isResource bool
	kind       string
}

func readOwnership(mustOwn []string, slots []contracts.IntentNodeV1SlotsItem) nodeOwnership {
	o := nodeOwnership{mustOwn: append([]string{}, mustOwn...), slots: make(map[string]ownedSlot, len(slots))}
	for _, s := range slots {
		slot := ownedSlot{isResource: s.Type == contracts.IntentNodeV1SlotsItemTypeResource}
		if s.ResourceKind != nil {
			slot.kind = string(*s.ResourceKind)
		}
		o.slots[string(s.Name)] = slot
	}
	return o
}

// OwnershipKinds is the pack's ownership data (A475, interim, relayed): for each intent node whose trust profile says the principal must own a slot, the resource kind of that slot.
// A node whose profile owns nothing, or that has no profile, has no entry: it has no ownership slot. The map is a copy; the pack is never exposed and never changed.
//
// It refuses the whole answer, with ErrPack, when it cannot be read soundly (INV-16): an owned slot the node does not have, one that is not a Resource or has no resource kind, or a
// node whose owned slots have two different kinds (the Ownership adapter holds one kind per node). A nil or zero Pack is ErrConfig.
func (p *Pack) OwnershipKinds() (map[string]string, error) {
	if !p.usable() {
		return nil, ErrConfig
	}
	out := make(map[string]string, len(p.owned))
	for id, o := range p.owned {
		kind := ""
		for _, name := range o.mustOwn {
			s, ok := o.slots[name]
			if !ok || !s.isResource || s.kind == "" {
				return nil, packErr("an intent node owns a slot that is not a resource slot with a kind")
			}
			if kind != "" && kind != s.kind {
				return nil, packErr("an intent node owns slots of two resource kinds")
			}
			kind = s.kind
		}
		out[id] = kind
	}
	return out, nil
}
