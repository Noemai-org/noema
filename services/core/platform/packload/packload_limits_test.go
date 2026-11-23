package packload_test

// A544 (interim; REQ-TRU-004, REQ-TRU-006, REQ-RES-002; INV-14, INV-21; AGENT-16): the limits files of a pack are hash-pinned in the manifest like every other pack file (kind `limits`,
// additive, as A510 added `action_graph`), and the core hands back only the bytes a digest covers. A limit is data a Founder reviews, so a byte changed after the manifest was made refuses the
// whole pack. Every fault is a real file in a real directory. The packs are SYNTHETIC and made per test; they name no vertical the core knows.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

const (
	limitsOne  = "demo/limits/one_limits.json"
	limitsTwo  = "demo/limits/two_limits.json"
	limitsBody = `{"schema_version":"1.0","pack_id":"demo","label":"SYNTHETIC","one":1}`
	limitsTwoB = `{"schema_version":"1.0","pack_id":"demo","label":"SYNTHETIC","two":2}`
	kindLimits = "limits"
)

// newLimitsWorld is a pack of the three earlier files and two limits files, both pinned.
func newLimitsWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.write(limitsOne, limitsBody)
	w.write(limitsTwo, limitsTwoB)
	w.list = append(w.list, entry{kindLimits, limitsOne, sum(limitsBody)}, entry{kindLimits, limitsTwo, sum(limitsTwoB)})
	w.seal()
	return w
}

func refusedNoLimits(t *testing.T, what string, err error, kind error, p packload.Pack) {
	t.Helper()
	wantErr(t, what, err, kind, p)
	if len(p.Limits) != 0 {
		t.Errorf("%s: a refused pack returned %d limits files", what, len(p.Limits))
	}
}

// The limits files a manifest pins load with the bytes each digest covers, by the path the manifest pins.
func TestAPackWithItsLimitsPinnedLoadsThemVerified(t *testing.T) {
	p, err := newLimitsWorld(t).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Limits) != 2 || string(p.Limits[limitsOne]) != limitsBody || string(p.Limits[limitsTwo]) != limitsTwoB {
		t.Errorf("limits %v; want the two pinned files with their bytes", p.Limits)
	}
}

// The kind is additive: a pack that pins none loads with none.
func TestAPackThatPinsNoLimitsLoadsWithNone(t *testing.T) {
	p, err := newWorld(t).load()
	if err != nil || len(p.Limits) != 0 {
		t.Errorf("%v %v; want a pack with no limits files", p.Limits, err)
	}
}

// AGENT-16, INV-21: a byte of a pinned limits file changed after the manifest was made refuses the whole pack, naming the kind and no path or content.
func TestAChangedByteInAPinnedLimitsFileRefusesThePack(t *testing.T) {
	w := newLimitsWorld(t)
	w.write(limitsTwo, limitsTwoB+" ")
	p, err := w.load()
	refusedNoLimits(t, "a changed limits file", err, packload.ErrHash, p)
	if k := kindOf(t, err); k != kindLimits {
		t.Errorf("kind %q; want %q", k, kindLimits)
	}
	if strings.Contains(err.Error(), "two_limits") || strings.Contains(err.Error(), "demo") {
		t.Errorf("the error names pack content: %v", err)
	}
}

// A limits file of the folder that the manifest does not pin is a limit nobody reviewed: the pack is refused, whether it sits next to the pinned ones or deeper.
func TestALimitsFileTheManifestDoesNotPinRefusesThePack(t *testing.T) {
	for _, rel := range []string{"demo/limits/extra_limits.json", "demo/limits/deeper/extra_limits.json"} {
		w := newLimitsWorld(t)
		w.write(rel, limitsBody)
		p, err := w.load()
		refusedNoLimits(t, "an unpinned "+rel, err, packload.ErrUnlisted, p)
	}
}

// A pinned limits file that is missing, is a link or escapes the directory refuses the pack, as for every other kind.
func TestAMissingLinkedOrEscapingLimitsFileRefusesThePack(t *testing.T) {
	w := newLimitsWorld(t)
	if err := os.Remove(filepath.Join(w.dir, filepath.FromSlash(limitsOne))); err != nil {
		t.Fatal(err)
	}
	p, err := w.load()
	refusedNoLimits(t, "a missing limits file", err, packload.ErrFile, p)

	w = newLimitsWorld(t)
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte(limitsBody), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(w.dir, filepath.FromSlash(limitsOne))
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	p, err = w.load()
	refusedNoLimits(t, "a linked limits file", err, packload.ErrLink, p)

	w = newLimitsWorld(t)
	w.list[len(w.list)-1].Path = "../outside.json"
	w.seal()
	p, err = w.load()
	refusedNoLimits(t, "an escaping limits path", err, packload.ErrPath, p)
}
