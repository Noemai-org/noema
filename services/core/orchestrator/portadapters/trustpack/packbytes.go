package trustpack

import "bytes"

// PackBytes is the pack file exactly as LoadPack read and decoded it (STM-01, A548 interim relayed; INV-16): the one source of the pack's bytes for an adapter that reads the pack
// document itself, so that no caller holds a second copy that could disagree with this Pack. It is a copy: changing it changes nothing in the Pack. A nil or zero Pack has none.
func (p *Pack) PackBytes() []byte {
	if p == nil {
		return nil
	}
	return bytes.Clone(p.raw)
}
