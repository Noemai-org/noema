package understanding

import "github.com/Noemai-org/noema/services/core/internal/contracts"

// SubjectOf gives the function that takes the subject of an admitted intent: the resolved reference of the slot the pack names `subject_slot` on the intent's node (DEBT-54
// part 2, ruling A520, interim). The slot name comes from the pack's own view, never from this package (INV-14). An intent has no subject when its node declares none, when the
// slot is not a resolved Resource slot, or when the view does not know the node (the gate would have refused it). The result is a copy of what the verified document holds;
// the view is read only and nothing here reads a file, a network or a database (A551).
func SubjectOf(pack PackView) func(AdmittedIntent) (string, bool) {
	return func(a AdmittedIntent) (string, bool) {
		o := a.Object()
		node, ok := pack.Node(string(o.IntentNode))
		if !ok || node.SubjectSlot == "" {
			return "", false
		}
		raw, ok := o.Slots[node.SubjectSlot]
		if !ok {
			return "", false
		}
		var slot contracts.IntentSlotV2ResourceSlot
		if slot.UnmarshalJSON([]byte(raw)) != nil || slot.Status != contracts.IntentSlotV2SlotStatusRESOLVED || slot.ResolvedRef == nil {
			return "", false
		}
		return string(*slot.ResolvedRef), true
	}
}
