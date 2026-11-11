package understanding

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// checkSlots holds each slot to the node and to the slot contract (intent-slot.v1): a slot the node does not name is refused, a slot of another type than the
// node says is refused, and the contract's rules that a Go type cannot carry (RESOLVED if and only if there is a resolved_ref, a ratio in [0, 1], a non-negative
// integer amount in range, a currency or a pack unit, a window of two instants) are checked here. The shape (closed, no repeated member) was
// checked when the object was decoded.
func (g *Gate) checkSlots(o *contracts.IntentObjectV1, node Node, known bool, r *reasonSet) {
	names := make([]string, 0, len(o.Slots))
	for n := range o.Slots {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if !lowerSnakeRe.MatchString(name) {
			r.add(ReasonSlotName)
			continue
		}
		raw := json.RawMessage(o.Slots[name])
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			r.add(ReasonSlotInvalid)
			continue
		}
		if known {
			want, ok := node.Slots[name]
			if !ok {
				r.add(ReasonUnknownSlot)
				continue
			}
			if want != head.Type {
				r.add(ReasonSlotType)
				continue
			}
		}
		if !slotValid(head.Type, raw) {
			r.add(ReasonSlotInvalid)
		}
	}
}

func slotValid(typ string, raw json.RawMessage) bool {
	switch typ {
	case "Resource":
		var s contracts.IntentSlotV1ResourceSlot
		if json.Unmarshal(raw, &s) != nil || s.Type != "Resource" || !statusOK(string(s.Status)) || !ratioPtr(s.Confidence) {
			return false
		}
		if s.CandidatesRef.Set && !s.CandidatesRef.Null && !candidateSetRe.MatchString(string(s.CandidatesRef.Value)) {
			return false
		}
		resolved := s.ResolvedRef != nil
		if resolved != (s.Status == "RESOLVED") { // RESOLVED only with a resolved_ref, and a resolved_ref only on a RESOLVED slot (INV-06)
			return false
		}
		return !resolved || resourceRefRe.MatchString(string(*s.ResolvedRef))
	case "Quantity":
		var s contracts.IntentSlotV1QuantitySlot
		if json.Unmarshal(raw, &s) != nil || s.Type != "Quantity" || !statusPtrOK(s.Status) || !ratioPtr(s.Confidence) {
			return false
		}
		unit := string(s.Unit)
		return int64(s.Value) >= 0 && int64(s.Value) <= maxSafeInt && (unit == "NGN" || packUnitRe.MatchString(unit))
	case "Party":
		var s contracts.IntentSlotV1PartySlot
		return json.Unmarshal(raw, &s) == nil && s.Type == "Party" && s.SurfaceForm != "" && statusPtrOK(s.Status) && ratioPtr(s.Confidence)
	case "TimeRef":
		var s contracts.IntentSlotV1TimeRefSlot
		if json.Unmarshal(raw, &s) != nil || s.Type != "TimeRef" || s.SurfaceForm == "" || !statusPtrOK(s.Status) || !ratioPtr(s.Confidence) {
			return false
		}
		return windowOK(s)
	case "Reference":
		var s contracts.IntentSlotV1ReferenceSlot
		return json.Unmarshal(raw, &s) == nil && s.Type == "Reference" && s.Value != "" && statusPtrOK(s.Status) && ratioPtr(s.Confidence)
	}
	return false
}

func statusOK(s string) bool {
	return s == "RESOLVED" || s == "UNRESOLVED" || s == "AMBIGUOUS" || s == "ABSENT"
}

func statusPtrOK(s *contracts.IntentSlotV1SlotStatus) bool { return s == nil || statusOK(string(*s)) }

func ratioPtr(n *contracts.CommonV1Ratio) bool { return n == nil || ratio(string(*n)) }

func windowOK(s contracts.IntentSlotV1TimeRefSlot) bool {
	if s.Window == nil {
		return true
	}
	w := *s.Window
	if len(w) != 2 || !timestampRe.MatchString(string(w[0])) || !timestampRe.MatchString(string(w[1])) {
		return false
	}
	_, e1 := time.Parse(time.RFC3339Nano, string(w[0]))
	_, e2 := time.Parse(time.RFC3339Nano, string(w[1]))
	return e1 == nil && e2 == nil // the contract fixes two instants; it does not say they are ordered, so the gate does not invent that
}
