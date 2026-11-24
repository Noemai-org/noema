package trustpack_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

// Proves INV-16 (one source: the bytes an adapter reads are the bytes the pack was decoded from), INV-14 (the pack is data), STM-01 (A548, interim, relayed).

func TestPackBytesAreTheBytesTheFileHeldWhenItWasLoaded(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(packsDir(), packFileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := loadReal(t).PackBytes(); !bytes.Equal(got, want) || len(got) == 0 {
		t.Errorf("PackBytes is %d bytes; want the %d bytes of the pack file", len(got), len(want))
	}
}

func TestPackBytesAreACopyAndChangingThemChangesNothing(t *testing.T) {
	p := loadReal(t)
	first := p.PackBytes()
	for i := range first {
		first[i] = 'x'
	}
	if second := p.PackBytes(); len(second) == 0 || bytes.Equal(first, second) {
		t.Errorf("a change to a returned copy reached the Pack")
	}
}

func TestANilOrZeroPackHasNoBytes(t *testing.T) {
	var nilPack *trustpack.Pack
	if b := nilPack.PackBytes(); len(b) != 0 {
		t.Errorf("a nil Pack has bytes: %d", len(b))
	}
	if b := (&trustpack.Pack{}).PackBytes(); len(b) != 0 {
		t.Errorf("a zero Pack has bytes: %d", len(b))
	}
}
