package trustpack

// OwnershipKinds is the pack's ownership data: for each intent node whose trust profile says the principal must own a slot, the resource kind of that slot. (STUB: the red commit.)
func (p *Pack) OwnershipKinds() (map[string]string, error) {
	return map[string]string{}, nil
}
