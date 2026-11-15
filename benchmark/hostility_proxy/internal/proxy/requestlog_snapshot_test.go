package proxy

import (
	"sync"
	"testing"
)

// A22 (Codex review of noema#209): the generation and the entries are one claim. If a reset could happen between reading the generation and
// reading the log, the response could pair the old generation with the new log, and a reader holding the old generation would accept the new
// log as continuous. snapshot reads both under one lock. The invariant checked here: while the generation a reader sees does not change, the
// log it sees only grows (the earlier entries are a prefix of the later ones). SYNTHETIC (AGENT-25).
func TestASnapshotNeverPairsAnOldGenerationWithANewLog(t *testing.T) {
	l := newRequestLog(0)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // a writer that resets often, so a reset lands between any two steps of a reader
		defer wg.Done()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			l.add(RequestEntry{Target: "t", Operation: "o", Method: "GET", Path: "/p"})
			if i%3 == 0 {
				l.reset()
			}
		}
	}()
	var prevGen string
	var prev []RequestEntry
	for i := 0; i < 30000; i++ {
		gen, entries, _, _ := l.snapshot("", "")
		if gen == prevGen {
			if len(entries) < len(prev) || (len(prev) > 0 && entries[len(prev)-1].Seq != prev[len(prev)-1].Seq) {
				close(stop)
				wg.Wait()
				t.Fatalf("generation %s: the log went from %d entries to %d; the generation does not describe the log it was returned with", gen, len(prev), len(entries))
			}
		}
		prevGen, prev = gen, entries
	}
	close(stop)
	wg.Wait()
}

func TestASnapshotCarriesTheGenerationThatResetChangesAndAddDoesNot(t *testing.T) {
	l := newRequestLog(0)
	g1, _, _, _ := l.snapshot("", "")
	l.add(RequestEntry{Target: "t", Operation: "o"})
	if g2, e, _, _ := l.snapshot("", ""); g2 != g1 || len(e) != 1 {
		t.Fatalf("an added entry changed the generation or was lost: %s %s %d", g1, g2, len(e))
	}
	l.reset()
	if g3, e, _, _ := l.snapshot("", ""); g3 == g1 || len(e) != 0 {
		t.Fatalf("a reset must change the generation and clear the log: %s %d", g3, len(e))
	}
}
