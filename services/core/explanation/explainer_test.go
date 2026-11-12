package explanation_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// EXP-01 slice 4, ruling A155: the Explainer that the orchestrator calls at EXPLAINING (REQ-EXP-002, REQ-AUD-002; INV-13, EC-E16; PERD 28.2, 31.2). The facts come from a
// FactsSource (EL builds the real one in the orchestrator, A155; here a fake) and the Record is a Recorder port; SYNTHETIC data throughout.

type call struct {
	s          explanation.Subject
	decisionID string
}

type fakeFacts struct {
	f     contracts.ExplanationFactsV2
	err   error
	block bool // wait for the context
	calls []call
	log   *[]string
}

func (x *fakeFacts) Facts(ctx context.Context, s explanation.Subject, decisionID string) (contracts.ExplanationFactsV2, error) {
	x.calls = append(x.calls, call{s, decisionID})
	if x.log != nil {
		*x.log = append(*x.log, "facts")
	}
	if x.block {
		<-ctx.Done()
		return contracts.ExplanationFactsV2{}, ctx.Err()
	}
	return x.f, x.err
}

type fakeRecorder struct {
	events []explanation.Event
	subj   []explanation.Subject
	err    error
	log    *[]string
}

func (x *fakeRecorder) AppendExplanation(_ context.Context, s explanation.Subject, e explanation.Event) error {
	if x.log != nil {
		*x.log = append(*x.log, "record")
	}
	if x.err != nil {
		return x.err
	}
	x.events = append(x.events, e)
	x.subj = append(x.subj, s)
	return nil
}

var (
	subject  = explanation.Subject{TenantID: "ten_a", PrincipalID: "prn_cust1"}
	forbid   = map[string][]string{"en-NG": {`(?i)guarantee`}}
	ruleCode = regexp.MustCompile(`^[A-Z][A-Z_]*$`)
)

func newExplainer(t *testing.T, tpl explanation.Templates, ff *fakeFacts, rec *fakeRecorder) *explanation.Explainer {
	t.Helper()
	e, err := explanation.NewExplainer(explanation.Config{Templates: tpl, Forbidden: forbid, Facts: ff, Recorder: rec, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func slotFacts() contracts.ExplanationFactsV2 {
	return withFacts(fact("f1", "payment.amount", known, "NGN 5,000"))
}

// REQ-EXP-002, REQ-AUD-002: a rendered explanation is in the Record before the text is returned (EC-E16), asking the facts source with the session's subject and the decision.
func TestARenderedExplanationIsRecordedBeforeTheTextIsReturned(t *testing.T) {
	var log []string
	ff := &fakeFacts{f: slotFacts(), log: &log}
	rec := &fakeRecorder{log: &log}
	tpl := slotTemplates(t, "Your transfer of {fact:payment.amount} has settled.")
	got, err := newExplainer(t, tpl, ff, rec).Explain(context.Background(), subject, "dec_01J9X3")
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocked || got.Text != "Your transfer of NGN 5,000 has settled." || got.TemplateID != "en-NG/"+tplKey || !strings.HasPrefix(got.TemplateVersion, "sha256:") {
		t.Fatalf("explanation %+v", got)
	}
	if len(ff.calls) != 1 || ff.calls[0].s != subject || ff.calls[0].decisionID != "dec_01J9X3" {
		t.Fatalf("the facts source was asked %+v", ff.calls)
	}
	if strings.Join(log, ",") != "facts,record" {
		t.Fatalf("order %v: the facts are read, then the event is recorded, then the text is returned", log)
	}
	if len(rec.events) != 1 || rec.subj[0] != subject {
		t.Fatalf("events %+v for %+v", rec.events, rec.subj)
	}
	e := rec.events[0]
	if !e.Rendered || e.CorrelationID != "corr_t1" || e.Text != got.Text || strings.Join(e.FactIDs, ",") != "f1" || e.TemplateID != got.TemplateID || e.TemplateVersion != got.TemplateVersion || len(e.Rules) != 0 {
		t.Fatalf("event %+v", e)
	}
}

// EC-E16: if the Record refuses the event, no text is returned.
func TestWhenTheRecordRefusesTheEventNoTextIsReturned(t *testing.T) {
	for _, rendered := range []bool{true, false} {
		ff := &fakeFacts{f: slotFacts()}
		rec := &fakeRecorder{err: errors.New("record down")}
		text := "Your transfer of {fact:payment.amount} has settled."
		if !rendered {
			text = "We guarantee {fact:payment.amount} will arrive."
		}
		got, err := newExplainer(t, slotTemplates(t, text), ff, rec).Explain(context.Background(), subject, "dec_01J9X3")
		if !errors.Is(err, explanation.ErrRecord) {
			t.Errorf("rendered=%v: got %v, want ErrRecord", rendered, err)
		}
		if got.Text != "" || got.Blocked {
			t.Errorf("rendered=%v: returned %+v with a refused Record", rendered, got)
		}
	}
}

// REQ-EXP-002, PERD 28.2 (EXPLAINING_BLOCKED): a text the validator blocks is not returned; the Record gets EXPLANATION_BLOCKED with a rule code and no text.
func TestAnExplanationTheValidatorBlocksIsRecordedAsBlockedWithACodeAndNoText(t *testing.T) {
	ff := &fakeFacts{f: slotFacts()}
	rec := &fakeRecorder{}
	tpl := slotTemplates(t, "We guarantee {fact:payment.amount} will arrive.")
	got, err := newExplainer(t, tpl, ff, rec).Explain(context.Background(), subject, "dec_01J9X3")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blocked || got.Text != "" {
		t.Fatalf("explanation %+v", got)
	}
	if len(got.Rules) == 0 || got.Rules[0] != "FORBIDDEN_CLAIM" {
		t.Fatalf("rules %v, want FORBIDDEN_CLAIM first", got.Rules)
	}
	if len(rec.events) != 1 || rec.events[0].Rendered || rec.events[0].Text != "" || strings.Join(rec.events[0].Rules, ",") != strings.Join(got.Rules, ",") || rec.events[0].CorrelationID != "corr_t1" {
		t.Fatalf("event %+v", rec.events)
	}
	for _, r := range got.Rules {
		if !ruleCode.MatchString(r) {
			t.Errorf("rule %q is not an upper-case code", r)
		}
	}
}

// Every way the validator can block maps to its own code, and none of the codes carries text of the explanation.
func TestEachKindOfBlockHasItsOwnRuleCode(t *testing.T) {
	cases := map[string]struct {
		template string
		facts    contracts.ExplanationFactsV2
		want     string
	}{
		"no template for the locale": {"Your transfer has settled.", func() contracts.ExplanationFactsV2 {
			f := slotFacts()
			f.Locale = contracts.ExplanationFactsV2LocalePcm
			return f
		}(), "NO_TEMPLATE"},
		"an unfilled slot":  {"Your transfer of {fact:account} has settled.", slotFacts(), "SLOT_UNRESOLVED"},
		"a forbidden claim": {"We guarantee your transfer.", slotFacts(), "FORBIDDEN_CLAIM"},
		"a long digit run":  {"Your transfer of {fact:payment.amount} has settled.", withFacts(fact("f1", "payment.amount", known, "NGN 12345678")), "UNMASKED_DIGITS"},
	}
	for name, c := range cases {
		rec := &fakeRecorder{}
		got, err := newExplainer(t, slotTemplates(t, c.template), &fakeFacts{f: c.facts}, rec).Explain(context.Background(), subject, "dec_01J9X3")
		if err != nil || !got.Blocked || got.Text != "" {
			t.Errorf("%s: %+v, %v", name, got, err)
			continue
		}
		found := false
		for _, r := range got.Rules {
			found = found || r == c.want
		}
		if !found {
			t.Errorf("%s: rules %v, want %s among them", name, got.Rules, c.want)
		}
		if len(rec.events) != 1 || rec.events[0].Rendered {
			t.Errorf("%s: event %+v", name, rec.events)
		}
	}
}

// A facts source that cannot answer is an error, not a block: nothing is recorded and no text is returned.
func TestAFactsSourceThatFailsReturnsAnErrorAndRecordsNothing(t *testing.T) {
	rec := &fakeRecorder{}
	got, err := newExplainer(t, slotTemplates(t, "Your transfer has settled."), &fakeFacts{err: errors.New("no facts")}, rec).Explain(context.Background(), subject, "dec_01J9X3")
	if err == nil || got.Text != "" || got.Blocked || len(rec.events) != 0 {
		t.Fatalf("%+v, %v, %d events", got, err, len(rec.events))
	}
}

// The facts call has its own deadline from configuration (AGENTS: a context deadline on every call).
func TestTheFactsCallIsBoundedByTheConfiguredTimeout(t *testing.T) {
	rec := &fakeRecorder{}
	e, err := explanation.NewExplainer(explanation.Config{Templates: slotTemplates(t, "Your transfer has settled."), Forbidden: forbid, Facts: &fakeFacts{block: true}, Recorder: rec, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = e.Explain(context.Background(), subject, "dec_01J9X3")
	if err == nil || time.Since(start) > 2*time.Second || len(rec.events) != 0 {
		t.Fatalf("err %v after %v with %d events", err, time.Since(start), len(rec.events))
	}
}

// INV-12: a call without a tenant and a principal, or with a decision id that is not a reference, asks nothing and records nothing.
func TestACallWithoutASubjectOrWithABadDecisionAsksNothingAndRecordsNothing(t *testing.T) {
	ff := &fakeFacts{f: slotFacts()}
	rec := &fakeRecorder{}
	e := newExplainer(t, slotTemplates(t, "Your transfer has settled."), ff, rec)
	for name, s := range map[string]explanation.Subject{"no tenant": {PrincipalID: "prn_cust1"}, "no principal": {TenantID: "ten_a"}, "neither": {}} {
		if _, err := e.Explain(context.Background(), s, "dec_01J9X3"); !errors.Is(err, explanation.ErrNoSubject) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, id := range []string{"", "DEC 1", "dec_", "dec_01J9X3\n", strings.Repeat("a", 200)} {
		if _, err := e.Explain(context.Background(), subject, id); !errors.Is(err, explanation.ErrBadDecision) {
			t.Errorf("%q: %v", id, err)
		}
	}
	if len(ff.calls) != 0 || len(rec.events) != 0 {
		t.Fatalf("asked %d, recorded %d", len(ff.calls), len(rec.events))
	}
}

// A explainer that cannot do everything does nothing: a missing part, a locale without a forbidden-claim list, or no timeout is refused at construction.
func TestNewExplainerRefusesWhatItCannotRunWith(t *testing.T) {
	tpl := slotTemplates(t, "Your transfer has settled.")
	ok := explanation.Config{Templates: tpl, Forbidden: forbid, Facts: &fakeFacts{}, Recorder: &fakeRecorder{}, Timeout: time.Second}
	if _, err := explanation.NewExplainer(ok); err != nil {
		t.Fatalf("control: %v", err)
	}
	for name, edit := range map[string]func(c *explanation.Config){
		"no templates":                 func(c *explanation.Config) { c.Templates = explanation.Templates{} },
		"no facts source":              func(c *explanation.Config) { c.Facts = nil },
		"no recorder":                  func(c *explanation.Config) { c.Recorder = nil },
		"no timeout":                   func(c *explanation.Config) { c.Timeout = 0 },
		"no forbidden lists":           func(c *explanation.Config) { c.Forbidden = nil },
		"an empty list":                func(c *explanation.Config) { c.Forbidden = map[string][]string{"en-NG": {}} },
		"a list that does not compile": func(c *explanation.Config) { c.Forbidden = map[string][]string{"en-NG": {"("}} },
	} {
		c := ok
		edit(&c)
		if _, err := explanation.NewExplainer(c); !errors.Is(err, explanation.ErrNotWired) {
			t.Errorf("%s: %v, want ErrNotWired", name, err)
		}
	}
}

// No error and no recorded blocked event quotes the text of the template or of a fact.
func TestNoBlockedEventOrErrorQuotesTheText(t *testing.T) {
	rec := &fakeRecorder{}
	f := withFacts(fact("f1", "payment.amount", known, "NGN 5,000"))
	f.Facts = append(f.Facts, fact("f2", "account", known, "ending 0001"))
	got, err := newExplainer(t, slotTemplates(t, "We guarantee {fact:payment.amount} will arrive."), &fakeFacts{f: f}, rec).Explain(context.Background(), subject, "dec_01J9X3")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range append(append([]string{}, got.Rules...), rec.events[0].Rules...) {
		if strings.Contains(s, "guarantee") || strings.Contains(s, "5,000") {
			t.Errorf("rule %q quotes the text", s)
		}
	}
}
