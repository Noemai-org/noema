package trustpack

// TRU-02 red stub: the accessor is not built yet.

// WriteSlot is one slot declaration of an intent node, as the pack writes it.
type WriteSlot struct {
	Name       string
	Type       string
	Required   bool
	Vocabulary []string
}

// WriteWhen is an intent node's write_when predicate ("" when it has none) and its slot declarations.
type WriteWhen struct {
	Predicate string
	Slots     []WriteSlot
}

// WriteWhens is not built yet.
func (p *Pack) WriteWhens() (map[string]WriteWhen, error) { return nil, nil }
