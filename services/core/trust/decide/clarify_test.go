package decide_test

import (
	"errors"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

var lim = decide.Limits{MaxOptions: 4, MaxDigitRun: 4}

var order = []decide.AttributeSpec{
	{Name: "last4", QuestionKey: "fin.q.which_account", LabelTemplate: "the one to the account ending {value}"},
	{Name: "local_time", QuestionKey: "fin.q.which_time", LabelTemplate: "the one at {value}"},
	{Name: "amount", QuestionKey: "fin.q.which_amount", LabelTemplate: "the one for {value}"},
}

func cand(id, display, last4, when, amount string) decide.Candidate {
	return decide.Candidate{ID: id, Display: display, Attrs: map[string]string{"last4": last4, "local_time": when, "amount": amount}}
}

func TestAttributeModeWhenOneAttributeSeparatesEveryCandidate(t *testing.T) {
	c := []decide.Candidate{cand("t1", "NGN 20,000 at 07:12 to 2144", "2144", "07:12", "NGN 20,000"), cand("t2", "NGN 20,000 at 08:03 to 8891", "8891", "08:03", "NGN 20,000")}
	got, err := decide.BuildClarification(c, order, "fin.q.which_transaction", lim)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != decide.ModeAttribute || got.Attribute != "last4" || got.QuestionKey != "fin.q.which_account" || !got.AllowNone {
		t.Fatalf("%+v", got)
	}
	want := []decide.Option{{ID: "opt_1", Label: "the one to the account ending 2144", CandidateID: "t1"}, {ID: "opt_2", Label: "the one to the account ending 8891", CandidateID: "t2"}}
	if !reflect.DeepEqual(got.Options, want) {
		t.Fatalf("%+v", got.Options)
	}
}

func TestFallsBackToOptionsWhenNoAttributeSeparates(t *testing.T) {
	c := []decide.Candidate{cand("t1", "first", "2144", "07:12", "NGN 20,000"), cand("t2", "second", "2144", "07:12", "NGN 20,000")}
	got, err := decide.BuildClarification(c, order, "fin.q.which_transaction", lim)
	if err != nil || got.Mode != decide.ModeOptions || got.Attribute != "" || got.QuestionKey != "fin.q.which_transaction" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Options[0].Label != "first" || got.Options[1].CandidateID != "t2" {
		t.Fatalf("%+v", got.Options)
	}
}

// Ties between attributes that separate every candidate go to the earlier attribute in the pack's order.
func TestTiesFollowThePackOrder(t *testing.T) {
	c := []decide.Candidate{cand("t1", "a", "1111", "07:12", "NGN 1"), cand("t2", "b", "2222", "08:03", "NGN 2")}
	first, _ := decide.BuildClarification(c, order, "k", lim)
	reversed := []decide.AttributeSpec{order[2], order[1], order[0]}
	second, _ := decide.BuildClarification(c, reversed, "k", lim)
	if first.Attribute != "last4" || second.Attribute != "amount" {
		t.Fatalf("first=%s second=%s", first.Attribute, second.Attribute)
	}
	for i := 0; i < 200; i++ { // replay: Go map iteration order never decides
		again, _ := decide.BuildClarification(c, order, "k", lim)
		if !reflect.DeepEqual(again, first) {
			t.Fatal("the same input gave a different question")
		}
	}
}

func TestLabelsNeverShowMoreThanFourDigitsInARow(t *testing.T) {
	// the only separating attribute would show a five-digit run, so it is not used
	c := []decide.Candidate{
		{ID: "t1", Display: "x", Attrs: map[string]string{"last4": "12345", "local_time": "07:12", "amount": "NGN 1"}},
		{ID: "t2", Display: "y", Attrs: map[string]string{"last4": "67890", "local_time": "07:12", "amount": "NGN 1"}},
	}
	got, err := decide.BuildClarification(c, order, "k", lim)
	if err != nil || got.Mode != decide.ModeOptions {
		t.Fatalf("%+v %v", got, err)
	}
	c[0].Display = "ending 12345"
	if _, err := decide.BuildClarification(c, order, "k", lim); !errors.Is(err, decide.ErrCannotDistinguish) {
		t.Fatalf("a five-digit label was shown: %v", err)
	}
}

func TestIdenticalDisplaysCannotBeAsked(t *testing.T) {
	c := []decide.Candidate{cand("t1", "same", "2144", "07:12", "NGN 1"), cand("t2", "same", "2144", "07:12", "NGN 1")}
	if _, err := decide.BuildClarification(c, order, "k", lim); !errors.Is(err, decide.ErrCannotDistinguish) {
		t.Fatalf("%v", err)
	}
}

func TestSizeLimits(t *testing.T) {
	one := []decide.Candidate{cand("t1", "a", "1111", "07:12", "NGN 1")}
	if _, err := decide.BuildClarification(nil, order, "k", lim); !errors.Is(err, decide.ErrNoCandidates) {
		t.Fatal(err)
	}
	if _, err := decide.BuildClarification(one, order, "k", lim); !errors.Is(err, decide.ErrNothingToAsk) {
		t.Fatal(err)
	}
	var five []decide.Candidate
	for i := 0; i < 5; i++ {
		five = append(five, cand("t"+strconv.Itoa(i), "d"+strconv.Itoa(i), "100"+strconv.Itoa(i), "0"+strconv.Itoa(i)+":00", "NGN "+strconv.Itoa(i)))
	}
	if _, err := decide.BuildClarification(five, order, "k", lim); !errors.Is(err, decide.ErrTooManyCandidates) {
		t.Fatal(err)
	}
}

// Only the pack's listed attributes and the candidate's own display are ever used: a field the pack did not list never reaches a label.
func TestUnlistedFieldsNeverAppear(t *testing.T) {
	secret := "OTHER-CUSTOMER-SECRET"
	c := []decide.Candidate{
		{ID: "t1", Display: "a", Attrs: map[string]string{"last4": "1111", "ssn": secret + "1", "last4x": secret}},
		{ID: "t2", Display: "b", Attrs: map[string]string{"last4": "1111", "ssn": secret + "2", "last4x": secret + "x"}},
	}
	got, err := decide.BuildClarification(c, order, "k", lim)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range got.Options {
		if strings.Contains(o.Label, "SECRET") {
			t.Fatalf("an unlisted field leaked into %q", o.Label)
		}
	}
	if got.Mode != decide.ModeOptions {
		t.Fatalf("an attribute the pack did not list separated the candidates: %+v", got)
	}
}

// Property (REQ-AMB-004): over generated candidate sets, ATTRIBUTE mode is used exactly when some listed attribute separates every
// candidate with valid labels; the attribute is the first such one in the pack's order; labels hold no long digit run.
func TestClarificationProperty(t *testing.T) {
	r := rand.New(rand.NewSource(20261020))
	values := []string{"1111", "2222", "3333", "44", "07:12", "08:03", "09:40", "NGN 1", "NGN 2", "123456"}
	attrMode := 0
	for i := 0; i < 3000; i++ {
		n := 2 + r.Intn(3)
		var cs []decide.Candidate
		for k := 0; k < n; k++ {
			cs = append(cs, decide.Candidate{ID: "t" + strconv.Itoa(k), Display: "d" + strconv.Itoa(r.Intn(4)), Attrs: map[string]string{
				"last4": values[r.Intn(len(values))], "local_time": values[4+r.Intn(3)], "amount": values[7+r.Intn(3)]}})
		}
		spec := append([]decide.AttributeSpec(nil), order...)
		r.Shuffle(len(spec), func(a, b int) { spec[a], spec[b] = spec[b], spec[a] })
		got, err := decide.BuildClarification(cs, spec, "k", lim)
		wantAttr := ""
		for _, s := range spec {
			seen, ok := map[string]bool{}, true
			for _, c := range cs {
				v := c.Attrs[s.Name]
				label := strings.Replace(s.LabelTemplate, "{value}", v, 1)
				ok = ok && !seen[v] && digitRun(label) <= lim.MaxDigitRun
				seen[v] = true
			}
			if ok {
				wantAttr = s.Name
				break
			}
		}
		if wantAttr != "" {
			attrMode++
			if err != nil || got.Mode != decide.ModeAttribute || got.Attribute != wantAttr {
				t.Fatalf("set %d: want attribute %s, got %+v %v", i, wantAttr, got, err)
			}
		} else if err == nil && got.Mode != decide.ModeOptions {
			t.Fatalf("set %d: ATTRIBUTE mode without a separating attribute: %+v", i, got)
		}
		if err == nil {
			for _, o := range got.Options {
				if digitRun(o.Label) > lim.MaxDigitRun {
					t.Fatalf("set %d: label %q has a long digit run", i, o.Label)
				}
			}
		}
	}
	if attrMode < 300 {
		t.Fatalf("only %d attribute-mode cases", attrMode)
	}
}

func digitRun(s string) int {
	best, run := 0, 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			run++
			if run > best {
				best = run
			}
		} else {
			run = 0
		}
	}
	return best
}
