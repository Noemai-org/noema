package trustpack

import (
	"context"
	"sort"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// HandoffRoute is one handoff route of the pack: a reason code the pack routes to an owner queue, for every intent node (IntentNodes empty) or for the nodes it lists (pack.v1
// handoff_routes). The values are the pack's data; this package names none of them (INV-14).
type HandoffRoute struct {
	ReasonCode  string
	OwnerQueue  string
	IntentNodes []string
}

// loadHandoffs reads the pack's handoff routes. It refuses the whole pack (INV-16) when a route names an intent node the pack does not have (it could never apply), or when one
// reason code is routed twice for the same intent node, which would leave two queues for one question: a route for every node overlaps any other route of the reason.
func (p *Pack) loadHandoffs(doc contracts.PackV1) error {
	if doc.HandoffRoutes == nil {
		return nil
	}
	for _, r := range *doc.HandoffRoutes {
		route := HandoffRoute{ReasonCode: string(r.ReasonCode), OwnerQueue: r.OwnerQueue}
		if r.IntentNodes != nil {
			seen := map[string]bool{}
			for _, n := range *r.IntentNodes {
				if _, ok := p.nodes[n]; !ok {
					return packErr("a handoff route names an intent node the pack does not have")
				}
				if !seen[n] {
					seen[n] = true
					route.IntentNodes = append(route.IntentNodes, n)
				}
			}
			sort.Strings(route.IntentNodes)
		}
		for _, other := range p.handoffs {
			if other.ReasonCode == route.ReasonCode && overlaps(other.IntentNodes, route.IntentNodes) {
				return packErr("a handoff reason is routed twice for the same intent node")
			}
		}
		p.handoffs = append(p.handoffs, route)
	}
	sort.Slice(p.handoffs, func(i, j int) bool {
		if p.handoffs[i].ReasonCode != p.handoffs[j].ReasonCode {
			return p.handoffs[i].ReasonCode < p.handoffs[j].ReasonCode
		}
		return p.handoffs[i].OwnerQueue < p.handoffs[j].OwnerQueue
	})
	return nil
}

// overlaps reports whether two node lists apply to a common node; an empty list applies to every node.
func overlaps(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	in := make(map[string]struct{}, len(a))
	for _, n := range a {
		in[n] = struct{}{}
	}
	for _, n := range b {
		if _, ok := in[n]; ok {
			return true
		}
	}
	return false
}

// HandoffRoutes is the pack's handoff routes (A551, A684; read-only), sorted by reason code and then queue, read from the pack bytes LoadPack verified. The slice and every list in it
// are copies: the pack is never exposed and never changed. A nil or zero Pack gives none and ErrConfig; a pack with no route gives none and no error (INV-16). It names no reason
// code and no queue (INV-14).
func (p *Pack) HandoffRoutes() ([]HandoffRoute, error) {
	if !p.usable() {
		return nil, ErrConfig
	}
	out := make([]HandoffRoute, 0, len(p.handoffs))
	for _, r := range p.handoffs {
		c := HandoffRoute{ReasonCode: r.ReasonCode, OwnerQueue: r.OwnerQueue}
		if len(r.IntentNodes) > 0 {
			c.IntentNodes = append([]string{}, r.IntentNodes...)
		}
		out = append(out, c)
	}
	return out, nil
}

// Handoffs implements intake.Handoffs over the pack's handoff routes (A531, A684): whether the pack routes a reason code to a handoff for an intent node. The zero value refuses every
// call with ErrConfig. The answer is the same for every tenant (the pack is the pack's); a question about no tenant is refused all the same.
type Handoffs struct{ pack *Pack }

var _ intake.Handoffs = Handoffs{}

// NewHandoffs builds the adapter. A nil pack is ErrConfig.
func NewHandoffs(p *Pack) (Handoffs, error) {
	if !p.usable() {
		return Handoffs{}, ErrConfig
	}
	return Handoffs{pack: p}, nil
}

// Routed answers whether the pack routes the reason to a handoff for the intent node. The reason is matched exactly (no case folding, no trimming). A reason the pack does not list is
// not routed and is not an error. A question that cannot be answered is an error and never "routed" or "not routed": no tenant is ErrSession, no node or no reason is
// ErrHandoffQuery, a node the pack does not have is ErrIntent, and a cancelled context is its error.
func (h Handoffs) Routed(ctx context.Context, q intake.HandoffQuery) (bool, error) {
	if !h.pack.usable() {
		return false, ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if q.TenantID == "" {
		return false, ErrSession
	}
	if q.IntentNode == "" || q.ReasonCode == "" {
		return false, ErrHandoffQuery
	}
	if _, ok := h.pack.nodes[q.IntentNode]; !ok {
		return false, ErrIntent
	}
	for _, r := range h.pack.handoffs {
		if r.ReasonCode != q.ReasonCode {
			continue
		}
		if len(r.IntentNodes) == 0 {
			return true, nil
		}
		for _, n := range r.IntentNodes {
			if n == q.IntentNode {
				return true, nil
			}
		}
	}
	return false, nil
}
