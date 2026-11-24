package packload_test

// A645 (interim; EVD-06, PCK-02; REQ-CTX-007, REQ-SEC-007, REQ-OPS-003; INV-14, INV-21; AGENT-13, AGENT-16): the injection-screen patterns are pack data, hash-pinned in the manifest like every other
// pack file (kind `injection_patterns`, at most one, with its expected id), and the core serves only verified bytes. Every fault is a real file in a real directory. All packs are SYNTHETIC.

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

const (
	patternsRel  = "demo/context/injection_patterns.v1.json"
	patternsBody = `{"pack_id":"demo.ctx.injection_patterns","version":1,"label":"SYNTHETIC","note":"n","patterns":[]}`
	patternsID   = "demo.ctx.injection_patterns@1"
	kindPatterns = "injection_patterns"
)

func patternsWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.write(patternsRel, patternsBody)
	w.list = append(w.list, entry{kindPatterns, patternsRel, sum(patternsBody)})
	w.exp["injection_patterns_id"] = patternsID
	w.seal()
	return w
}

func TestAPackWithItsInjectionPatternsPinnedLoadsThemVerified(t *testing.T) {
	p, err := patternsWorld(t).load()
	if err != nil {
		t.Fatal(err)
	}
	if string(p.InjectionPatterns) != patternsBody || p.InjectionPatternsID != patternsID {
		t.Errorf("patterns %q id %q; want the pinned bytes and the expected id", p.InjectionPatterns, p.InjectionPatternsID)
	}
	if p2, err := newWorld(t).load(); err != nil || p2.InjectionPatterns != nil || p2.InjectionPatternsID != "" {
		t.Errorf("a pack that pins no patterns must load with none: %v %v", p2.InjectionPatterns, err)
	}
}

func TestAChangedByteInThePinnedPatternsRefusesThePackNamingOnlyTheKind(t *testing.T) {
	w := patternsWorld(t)
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
	w := patternsWorld(t)
	w.write("demo/context/injection_patterns.v2.json", patternsBody)
	_, err := w.load()
	if !errors.Is(err, packload.ErrUnlisted) || kindOf(t, err) != kindPatterns {
		t.Fatalf("got %v; want ErrUnlisted of the patterns kind", err)
	}
}

func TestTwoPatternEntriesOrAPatternFileWithoutItsExpectedIdOrAnIdWithoutAFileAreRefused(t *testing.T) {
	two := patternsWorld(t)
	two.write("demo/context/other.json", patternsBody)
	two.list = append(two.list, entry{kindPatterns, "demo/context/other.json", sum(patternsBody)})
	two.seal()
	if _, err := two.load(); !errors.Is(err, packload.ErrManifest) {
		t.Errorf("two entries: %v; want ErrManifest", err)
	}
	noID := patternsWorld(t)
	delete(noID.exp, "injection_patterns_id")
	noID.seal()
	if _, err := noID.load(); !errors.Is(err, packload.ErrManifest) {
		t.Errorf("a pinned file without its expected id: %v; want ErrManifest", err)
	}
	idOnly := newWorld(t)
	idOnly.exp["injection_patterns_id"] = patternsID
	idOnly.seal()
	if _, err := idOnly.load(); !errors.Is(err, packload.ErrManifest) {
		t.Errorf("an expected id with no file: %v; want ErrManifest", err)
	}
}

func TestAPatternFileThatCarriesAnotherIdThanTheManifestExpectsIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"other pack id": strings.Replace(patternsBody, `"demo.ctx.injection_patterns"`, `"demo.ctx.other"`, 1),
		"other version": strings.Replace(patternsBody, `"version":1`, `"version":2`, 1),
		"no id":         `{"version":1,"label":"SYNTHETIC","patterns":[]}`,
	} {
		w := patternsWorld(t)
		w.write(patternsRel, body)
		w.list[len(w.list)-1].Sha = sum(body)
		w.seal()
		p, err := w.load()
		if !errors.Is(err, packload.ErrID) || len(p.InjectionPatterns) != 0 {
			t.Errorf("%s: got %v; want ErrID and nothing served", name, err)
		}
	}
}
