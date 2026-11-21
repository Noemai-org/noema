package decide

// A275 (REQ-AMB-004, INV-12): the core refuses to emit a clarification whose none-of-these ID equals an option ID, and never emits one without a none ID. The ID the core mints
// cannot collide (options are opt_<n>), so the refusal is exercised through sealNone with IDs chosen to collide. All data is SYNTHETIC.

import (
	"errors"
	"strconv"
	"testing"
)

func optionsOf(n int) []Option {
	out := make([]Option, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, Option{ID: "opt_" + strconv.Itoa(i), Label: "option " + strconv.Itoa(i), CandidateID: "t" + strconv.Itoa(i)})
	}
	return out
}

func TestSealNoneRefusesAnIDThatEqualsAnOptionID(t *testing.T) {
	for n := 0; n <= 4; n++ {
		for k := 1; k <= n; k++ {
			q, err := sealNone(Clarification{Options: optionsOf(n)}, "opt_"+strconv.Itoa(k))
			if !errors.Is(err, ErrNoneOptionCollision) || q.Options != nil || q.NoneOptionID != "" {
				t.Fatalf("n=%d k=%d: %+v %v", n, k, q, err)
			}
		}
		q, err := sealNone(Clarification{Options: optionsOf(n)}, "opt_none")
		if err != nil || q.NoneOptionID != "opt_none" || len(q.Options) != n {
			t.Fatalf("n=%d: %+v %v", n, q, err)
		}
	}
}

func TestSealNoneNeverEmitsAQuestionWithoutANoneID(t *testing.T) {
	for n := 0; n <= 3; n++ {
		q, err := sealNone(Clarification{Options: optionsOf(n)}, "")
		if !errors.Is(err, ErrNoNoneOption) || q.Options != nil || q.NoneOptionID != "" {
			t.Fatalf("n=%d: %+v %v", n, q, err)
		}
	}
}
