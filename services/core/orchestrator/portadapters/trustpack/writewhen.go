package trustpack

import "github.com/Noemai-org/noema/services/core/internal/contracts"

// WriteSlot is one slot declaration of an intent node, as the pack file writes it: its name, its type, whether it is required and, for a Reference with a closed vocabulary, that
// vocabulary.
type WriteSlot struct {
	Name       string
	Type       string
	Required   bool
	Vocabulary []string
}

// WriteWhen is an intent node's `write_when` predicate, as the pack file writes it, and the node's slot declarations (the scope the predicate is read over). Predicate is "" when the
// node has no write_when: such a node never leads to a write (A364, interim, relayed).
type WriteWhen struct {
	Predicate string
	Slots     []WriteSlot
}

func readWriteWhen(n contracts.IntentNodeV1) WriteWhen {
	var w WriteWhen
	if n.WriteWhen != nil {
		w.Predicate = string(*n.WriteWhen)
	}
	for _, s := range n.Slots {
		slot := WriteSlot{Name: string(s.Name), Type: string(s.Type), Required: s.Required}
		if s.Vocabulary != nil {
			slot.Vocabulary = append([]string{}, *s.Vocabulary...)
		}
		w.Slots = append(w.Slots, slot)
	}
	return w
}

// WriteWhens is the pack's `write_when` data (A551, interim, relayed; read-only): for every intent node, its predicate and its slot declarations, read from the pack bytes LoadPack
// verified. The map and every slice in it are copies: the pack is never exposed and never changed. A nil or zero Pack, and a pack that did not load (it is no Pack), give none and
// ErrConfig (INV-16). The predicate is data: this package does not parse or evaluate it, and names no node or slot (INV-14).
func (p *Pack) WriteWhens() (map[string]WriteWhen, error) {
	if !p.usable() {
		return nil, ErrConfig
	}
	out := make(map[string]WriteWhen, len(p.writeNodes))
	for id, w := range p.writeNodes {
		c := WriteWhen{Predicate: w.Predicate}
		for _, s := range w.Slots {
			if s.Vocabulary != nil {
				s.Vocabulary = append([]string{}, s.Vocabulary...)
			}
			c.Slots = append(c.Slots, s)
		}
		out[id] = c
	}
	return out, nil
}
