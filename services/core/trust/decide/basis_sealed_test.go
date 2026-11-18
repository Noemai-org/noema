package decide_test

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// REQ-TRU-003, INV-16 (the Security Reviewer's note 5 on noema#572): Sealed is an independent copy with the same evaluated set and the same completeness, to which nothing can
// be added; the original keeps working.
func TestASealedBuilderIsACopyWithTheSameSetThatNothingCanBeAddedTo(t *testing.T) {
	b, err := decide.NewBasisBuilder("1.0.0", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RecordEvidence("evs_synth01", []decide.Freshness{{EvidenceID: "evd_synth01", EffectiveAt: 1699999990, Source: "src.synth", AgeSeconds: 10, EffectiveLimitSeconds: 60, LimitSetBy: "SOURCE"}}); err != nil {
		t.Fatal(err)
	}
	s := b.Sealed()
	if s == b {
		t.Fatal("Sealed returned the live builder")
	}
	da, errA := b.Digest()
	ds, errS := s.Digest()
	if errA != nil || errS != nil || da != ds {
		t.Fatalf("digests %q (%v) and %q (%v) differ", da, errA, ds, errS)
	}
	if err := s.RecordPolicy(nil); !errors.Is(err, decide.ErrBasisRecorded) {
		t.Errorf("a policy was added to a sealed builder: %v", err)
	}
	if err := s.RecordEvidence("evs_synth02", nil); !errors.Is(err, decide.ErrBasisRecorded) {
		t.Errorf("evidence was added to a sealed builder: %v", err)
	}
	// the original is unaffected and can still record what it has not recorded
	if err := b.RecordPolicy(nil); err != nil {
		t.Errorf("the original could not record its policy after it was sealed: %v", err)
	}
	if d2, _ := s.Digest(); d2 != da {
		t.Errorf("the sealed copy changed when the original did: %q then %q", da, d2)
	}
}
