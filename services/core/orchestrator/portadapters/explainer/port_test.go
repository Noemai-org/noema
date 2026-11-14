package explainerport_test

// EXP-01 slice 4 (A155; REQ-EXP-002, REQ-AUD-002; INV-12, INV-13): the adapter from the Explainer (explanation) to the orchestrator's Explainer port. It converts the
// session into the explainer's subject, passes the decision ID untouched, and returns text only for an answer the explainer did not block. A block never carries text, and an
// error from the explainer is the answer. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	explainerport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/explainer"
)

type fakeExplainer struct {
	asked []string
	out   explanation.Explanation
	err   error
}

func (f *fakeExplainer) Explain(_ context.Context, s explanation.Subject, decisionID string) (explanation.Explanation, error) {
	f.asked = append(f.asked, s.TenantID+"|"+s.PrincipalID+"|"+decisionID)
	return f.out, f.err
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

var sess = orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_01"}

func TestItPassesTheSessionAsTheSubjectAndTheDecisionIDAndReturnsTheText(t *testing.T) {
	f := &fakeExplainer{out: explanation.Explanation{Text: "We found your transfer.", TemplateID: "t1", TemplateVersion: "1"}}
	got, err := explainerport.Port{Explainer: f}.Explain(ctx(t), sess, "dec_01ABC")
	if err != nil || got != (orchestrator.Explanation{Text: "We found your transfer."}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(f.asked) != 1 || f.asked[0] != "ten_aa|usr_01|dec_01ABC" {
		t.Fatalf("the explainer was asked %v", f.asked)
	}
}

// A blocked answer carries no text, even if the explainer's struct held some (the validator's contract is that it holds none; this port does not rely on it).
func TestABlockedAnswerNeverCarriesText(t *testing.T) {
	f := &fakeExplainer{out: explanation.Explanation{Blocked: true, Rules: []string{"DIGIT_RUN"}, Text: "must not leak"}}
	got, err := explainerport.Port{Explainer: f}.Explain(ctx(t), sess, "dec_01ABC")
	if err != nil || !got.Blocked || got.Text != "" {
		t.Fatalf("got %+v, %v; want Blocked and no text", got, err)
	}
}

func TestAnExplainerErrorIsTheAnswerAndNoTextComesWithIt(t *testing.T) {
	for _, e := range []error{explanation.ErrRecord, explanation.ErrBadDecision, explanation.ErrNoSubject, errors.New("boom")} {
		got, err := explainerport.Port{Explainer: &fakeExplainer{err: e, out: explanation.Explanation{Text: "must not leak"}}}.Explain(ctx(t), sess, "dec_01ABC")
		if !errors.Is(err, e) || got != (orchestrator.Explanation{}) {
			t.Errorf("explainer error %v: got %+v, %v; want that error and an empty Explanation", e, got, err)
		}
	}
}

func TestAnUnusableCallIsRefusedBeforeTheExplainerIsAsked(t *testing.T) {
	f := &fakeExplainer{out: explanation.Explanation{Text: "x"}}
	for name, c := range map[string]struct {
		p explainerport.Port
		s orchestrator.Session
		d string
	}{
		"no explainer": {explainerport.Port{}, sess, "dec_01ABC"},
		"no tenant":    {explainerport.Port{Explainer: f}, orchestrator.Session{PrincipalID: "usr_01"}, "dec_01ABC"},
		"no principal": {explainerport.Port{Explainer: f}, orchestrator.Session{TenantID: "ten_aa"}, "dec_01ABC"},
		"no decision":  {explainerport.Port{Explainer: f}, sess, ""},
	} {
		if _, err := c.p.Explain(ctx(t), c.s, c.d); !errors.Is(err, explainerport.ErrNotUsable) {
			t.Errorf("%s: got %v, want ErrNotUsable", name, err)
		}
	}
	if len(f.asked) != 0 {
		t.Errorf("the explainer was asked %v for an unusable call", f.asked)
	}
}
