package packload_test

// A645 (interim; EVD-06, PCK-02; REQ-CTX-007, REQ-SEC-007, REQ-OPS-003; INV-14, INV-21; AGENT-13, AGENT-16): the injection-screen patterns are pack data, hash-pinned in the manifest like every other
// pack file (kind `injection_patterns`, with its expected id), and the core serves only verified bytes. A664 (interim): the kind is required from manifest version 4 and optional below it (the A579 pattern);
// a manifest below the gate that lists none still loads, and its pack then carries no pattern data. Every fault is a real file in a real directory. All packs are SYNTHETIC.

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

const kindPatterns = "injection_patterns"

func TestAPackWithItsInjectionPatternsPinnedLoadsThemVerified(t *testing.T) {
	p, err := newWorld(t).load()
	if err != nil {
		t.Fatal(err)
	}
	if string(p.InjectionPatterns) != patternsBody || p.InjectionPatternsID != patternsID {
		t.Errorf("patterns %q id %q; want the pinned bytes and the expected id", p.InjectionPatterns, p.InjectionPatternsID)
	}
}

func TestAChangedByteInThePinnedPatternsRefusesThePackNamingOnlyTheKind(t *testing.T) {
	w := newWorld(t)
	w.write(patternsRel, patternsBody+" ")
	p, err := w.load()
	if !errors.Is(err, packload.ErrHash) || len(p.InjectionPatterns) != 0 || len(p.Pack) != 0 {
		t.Fatalf("got %v %+v; want ErrHash and an empty pack", err, p)
	}
	if k := kindOf(t, err); k != kindPatterns {
		t.Errorf("kind %q; want %q", k, kindPatterns)
	}
	if strings.Contains(err.Error(), "demo") || strings.Contains(err.Error(), "context") {
		t.Errorf("the error names pack content: %v", err)
	}
}

func TestAPatternFileNextToThePinnedOneThatIsNotPinnedIsRefused(t *testing.T) {
	w := newWorld(t)
	w.write("demo/context/injection_patterns.v2.json", patternsBody)
	_, err := w.load()
	if !errors.Is(err, packload.ErrUnlisted) || kindOf(t, err) != kindPatterns {
		t.Fatalf("got %v; want ErrUnlisted of the patterns kind", err)
	}
}

func TestTwoPatternEntriesOrNoPatternEntryOrNoExpectedIdAreRefused(t *testing.T) {
	two := newWorld(t)
	two.write("demo/context/other.json", patternsBody)
	two.list = append(two.list, entry{kindPatterns, "demo/context/other.json", sum(patternsBody)})
	two.seal()
	if _, err := two.load(); !errors.Is(err, packload.ErrManifest) {
		t.Errorf("two entries: %v; want ErrManifest", err)
	}
	noFile := newWorld(t)
	noFile.list = noFile.list[:3]
	noFile.seal()
	if _, err := noFile.load(); !errors.Is(err, packload.ErrManifest) {
		t.Errorf("no pattern file pinned: %v; want ErrManifest", err)
	}
	noID := newWorld(t)
	delete(noID.exp, "injection_patterns_id")
	noID.seal()
	if _, err := noID.load(); !errors.Is(err, packload.ErrManifest) {
		t.Errorf("a pinned file without its expected id: %v; want ErrManifest", err)
	}
}

func TestAPatternFileThatCarriesAnotherIdThanTheManifestExpectsIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"other pack id": strings.Replace(patternsBody, `"demo.ctx.injection_patterns"`, `"demo.ctx.other"`, 1),
		"other version": strings.Replace(patternsBody, `"version":1`, `"version":2`, 1),
		"no id":         `{"version":1,"label":"SYNTHETIC","patterns":[]}`,
	} {
		w := newWorld(t)
		w.write(patternsRel, body)
		w.list[3].Sha = sum(body)
		w.seal()
		p, err := w.load()
		if !errors.Is(err, packload.ErrID) || len(p.InjectionPatterns) != 0 {
			t.Errorf("%s: got %v; want ErrID and nothing served", name, err)
		}
	}
}

// patternsFromVersion is the first manifest version that must list the injection patterns (A664, interim; the version after the limits files' 3, A579). A manifest of version 3 or later lists its
// limits files too, so a world of those versions carries them.
const patternsFromVersion = 4

func versioned(t *testing.T, v int) *world {
	t.Helper()
	w := newWorld(t)
	if v >= 3 {
		w = newLimitsWorld(t)
	}
	w.ver = v
	w.seal()
	return w
}

// without removes the patterns entry and its expected id and leaves the file on disk, which is what a deleted manifest entry looks like.
func without(w *world) *world {
	kept := w.list[:0:0]
	for _, e := range w.list {
		if e.Kind != kindPatterns {
			kept = append(kept, e)
		}
	}
	w.list = kept
	delete(w.exp, "injection_patterns_id")
	w.seal()
	return w
}

func TestAGatedManifestWithoutThePatternsIsRefusedAndAnOlderOneLoadsWithNoPatternData(t *testing.T) {
	for v := 1; v < patternsFromVersion; v++ {
		p, err := without(versioned(t, v)).load()
		if err != nil {
			t.Errorf("version %d without the kind: %v; want it to load (below the gate)", v, err)
		}
		if len(p.InjectionPatterns) != 0 || p.InjectionPatternsID != "" || len(p.Pack) == 0 {
			t.Errorf("version %d: patterns %q id %q pack %d bytes; want the pack served with no pattern data at all", v, p.InjectionPatterns, p.InjectionPatternsID, len(p.Pack))
		}
	}
	for _, v := range []int{patternsFromVersion, patternsFromVersion + 1, 99} {
		// The entry is deleted and the file is left on disk: that is refused at the gate, never loaded as "no patterns".
		p, err := without(versioned(t, v)).load()
		if !errors.Is(err, packload.ErrManifest) || len(p.Pack) != 0 || len(p.InjectionPatterns) != 0 {
			t.Errorf("version %d without the kind: got %v; want ErrManifest and an empty pack (no opt-in)", v, err)
		}
	}
}

// SR condition 2 (A664): the expected id is read only behind the gate. A version-3 manifest (the version before the gate, which lists its limits files) that names no
// `expected.injection_patterns_id` and lists no patterns file loads, whatever else is on disk; it neither panics nor reads a missing id.
func TestAVersionThreeManifestWithNoExpectedPatternsIdLoadsAndDoesNotReadIt(t *testing.T) {
	w := versioned(t, patternsFromVersion-1)
	without(w)
	if _, present := w.exp["injection_patterns_id"]; present {
		t.Fatal("the world still names the id")
	}
	p, err := w.load()
	if err != nil || len(p.InjectionPatterns) != 0 || p.InjectionPatternsID != "" || len(p.Limits) == 0 {
		t.Errorf("got %v, patterns %q id %q, %d limits files; want it loaded with its limits and no pattern data", err, p.InjectionPatterns, p.InjectionPatternsID, len(p.Limits))
	}
}

func TestAManifestThatListsThePatternsButNamesNoIdOrTheIdWithoutTheFileIsRefusedAtEveryVersion(t *testing.T) {
	for _, v := range []int{1, patternsFromVersion - 1, patternsFromVersion, patternsFromVersion + 1} {
		noID := versioned(t, v)
		delete(noID.exp, "injection_patterns_id")
		noID.seal()
		if _, err := noID.load(); !errors.Is(err, packload.ErrManifest) {
			t.Errorf("version %d: a pinned file without its expected id: %v; want ErrManifest", v, err)
		}
		noFile := versioned(t, v)
		kept := noFile.list[:0:0]
		for _, e := range noFile.list {
			if e.Kind != kindPatterns {
				kept = append(kept, e)
			}
		}
		noFile.list = kept
		noFile.seal()
		if _, err := noFile.load(); !errors.Is(err, packload.ErrManifest) {
			t.Errorf("version %d: an expected id without a pinned file: %v; want ErrManifest", v, err)
		}
		twoNoID := versioned(t, v) // two files and no id: the pairing alone would accept it, the count must not
		twoNoID.write("demo/context/other.json", patternsBody)
		twoNoID.list = append(twoNoID.list, entry{kindPatterns, "demo/context/other.json", sum(patternsBody)})
		delete(twoNoID.exp, "injection_patterns_id")
		twoNoID.seal()
		if _, err := twoNoID.load(); !errors.Is(err, packload.ErrManifest) {
			t.Errorf("version %d: two pinned pattern files and no expected id: %v; want ErrManifest", v, err)
		}
		two := versioned(t, v)
		two.write("demo/context/other.json", patternsBody)
		two.list = append(two.list, entry{kindPatterns, "demo/context/other.json", sum(patternsBody)})
		two.seal()
		if _, err := two.load(); !errors.Is(err, packload.ErrManifest) {
			t.Errorf("version %d: two pinned pattern files: %v; want ErrManifest (none or one below the gate, exactly one from it)", v, err)
		}
	}
}

func TestAGatedManifestWithItsPatternsLoadsThemVerified(t *testing.T) {
	for _, v := range []int{patternsFromVersion, patternsFromVersion + 3} {
		p, err := versioned(t, v).load()
		if err != nil || string(p.InjectionPatterns) != patternsBody || p.InjectionPatternsID != patternsID {
			t.Errorf("version %d: %v, patterns %q id %q; want the pinned bytes and the expected id", v, err, p.InjectionPatterns, p.InjectionPatternsID)
		}
	}
}

func TestAnOlderManifestThatDoesNotListThePatternsServesNoUnpinnedPatternFile(t *testing.T) {
	w := without(versioned(t, patternsFromVersion-1))
	// the file is still on disk (newWorld wrote it) but nobody pinned it
	p, err := w.load()
	if err != nil || len(p.InjectionPatterns) != 0 || p.InjectionPatternsID != "" {
		t.Fatalf("got %v, patterns %q id %q; want the pack loaded and no pattern bytes (a file nobody pinned is never served)", err, p.InjectionPatterns, p.InjectionPatternsID)
	}
}
