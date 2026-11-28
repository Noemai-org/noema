package packload_test

// A710 (A695, OD-P-117; the A579 and A664 pattern; INV-14, INV-21, AGENT-16): the recipient matcher's data is pack data, hash-pinned in the manifest as kind `recipient_matcher`. It is required from manifest
// version 5 (the version after the injection patterns' 4) and optional below it: a manifest below the gate that lists none still loads with no matcher data, a gated manifest without it is refused (no opt-in),
// and never two. The verified bytes come back as Pack.RecipientMatcher; a refused pack returns none. The core names no folder or file: the unlisted-file rule reads the listed file's own name stem.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

const (
	kindMatcher        = "recipient_matcher"
	matcherRel         = "demo/lexicons/recipient_matcher.json"
	matcherBody        = `{"matcher_id":"demo.matcher","version":"1.0.0","mark":"[synthetic seed]"}`
	matcherFromVersion = 5
)

func noMatcher(w *world) *world {
	kept := w.list[:0:0]
	for _, e := range w.list {
		if e.Kind != kindMatcher {
			kept = append(kept, e)
		}
	}
	w.list = kept
	w.seal()
	return w
}

func TestAGatedManifestWithoutTheMatcherIsRefusedAndAnOlderOneLoadsWithNone(t *testing.T) {
	for v := 1; v < matcherFromVersion; v++ {
		p, err := noMatcher(versioned(t, v)).load()
		if err != nil || len(p.RecipientMatcher) != 0 || len(p.Pack) == 0 {
			t.Errorf("version %d without the kind: %v, matcher %d bytes; want it loaded with no matcher data", v, err, len(p.RecipientMatcher))
		}
	}
	for _, v := range []int{matcherFromVersion, matcherFromVersion + 1, 99} {
		w := versioned(t, v)
		w.write(matcherRel, matcherBody) // the entry is deleted, the file is left on disk
		p, err := noMatcher(w).load()
		if !errors.Is(err, packload.ErrManifest) || len(p.Pack) != 0 || len(p.RecipientMatcher) != 0 {
			t.Errorf("version %d without the kind: %v; want ErrManifest and an empty pack (no opt-in)", v, err)
		}
	}
}

func TestAManifestThatListsTheMatcherLoadsItsVerifiedBytesAtEveryVersion(t *testing.T) {
	for _, v := range []int{1, 3, 4, matcherFromVersion, matcherFromVersion + 2} {
		w := versioned(t, v)
		if v < matcherFromVersion {
			w.write(matcherRel, matcherBody)
			w.list = append(w.list, entry{kindMatcher, matcherRel, sum(matcherBody)})
			w.seal()
		}
		p, err := w.load()
		if err != nil || string(p.RecipientMatcher) != matcherBody {
			t.Errorf("version %d: %v, matcher %q; want the pinned bytes", v, err, p.RecipientMatcher)
		}
	}
}

func TestTwoMatcherFilesAreRefusedAtEveryVersion(t *testing.T) {
	for _, v := range []int{1, 4, matcherFromVersion, matcherFromVersion + 1} {
		w := versioned(t, v)
		if v < matcherFromVersion {
			w.write(matcherRel, matcherBody)
			w.list = append(w.list, entry{kindMatcher, matcherRel, sum(matcherBody)})
		}
		w.write("demo/lexicons/recipient_matcher.other.json", matcherBody)
		w.list = append(w.list, entry{kindMatcher, "demo/lexicons/recipient_matcher.other.json", sum(matcherBody)})
		w.seal()
		if p, err := w.load(); !errors.Is(err, packload.ErrManifest) || len(p.RecipientMatcher) != 0 {
			t.Errorf("version %d: two matcher files: %v; want ErrManifest", v, err)
		}
	}
}

func TestAChangedByteInThePinnedMatcherRefusesThePackNamingOnlyTheKind(t *testing.T) {
	w := versioned(t, matcherFromVersion)
	w.write(matcherRel, matcherBody+" ")
	p, err := w.load()
	if !errors.Is(err, packload.ErrHash) || len(p.RecipientMatcher) != 0 || len(p.Pack) != 0 {
		t.Fatalf("got %v; want ErrHash and an empty pack", err)
	}
	if k := kindOf(t, err); k != kindMatcher {
		t.Errorf("kind %q; want %q", k, kindMatcher)
	}
}

// The folder a matcher lives in may hold other files (the pack's other lexicons live beside it): only a second file of the matcher's own name stem is a second matcher. The stem is read from the
// listed file's name, so the core names no file.
func TestAFileBesideTheMatcherIsNotAMatcherUnlessItSharesItsStem(t *testing.T) {
	w := versioned(t, matcherFromVersion)
	w.write("demo/lexicons/other_lexicon.json", `{"x":1}`)
	if _, err := w.load(); err != nil {
		t.Errorf("another lexicon beside the matcher: %v; want it loaded", err)
	}
	w.write("demo/lexicons/recipient_matcher.v2.json", matcherBody)
	if _, err := w.load(); !errors.Is(err, packload.ErrUnlisted) || kindOf(t, err) != kindMatcher {
		t.Errorf("a second matcher beside the pinned one, unlisted: %v; want ErrUnlisted of the matcher kind", err)
	}
}
