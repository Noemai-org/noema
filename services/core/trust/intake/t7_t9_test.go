package intake

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Steps T7 (target resolution), T8 (business ownership) and T9 (evidence) of PERD 23.3, one step at a time: the test builds the evaluation after T6 and runs the single step
// through decide.Evaluate, so the trace and the decision are the ones the fixed order produces. All data is SYNTHETIC. The numbers below are test data in a made-up fixed-point
// unit; the code under test holds none (REQ-OPS-003).
// Tests name the REQ, INV or EC they prove: REQ-TRU-002, REQ-TRU-003, REQ-AMB-001 to REQ-AMB-004, REQ-CTX-003, REQ-NFR-002, INV-06, INV-16, INV-19, INV-21, INV-23.

const (
	tTenant    = "ten_synth01"
	tPrincipal = "usr_synth01"
	tSnapshot  = "evs_synth0001"
	tVersion   = 2
)

var errFake = errors.New("injected fault")

// fake is every T7 to T9 port, recording each call with its arguments.
type fake struct {
	calls []string
	args  []string

	cands   TargetCandidates
	candErr error
	owned   bool
	ownErr  error
	evid    SnapshotEvidence
	evidErr error

	failAt, panicAt string
}

func (f *fake) hit(name, tenantID, other string) error {
	f.calls = append(f.calls, name)
	f.args = append(f.args, tenantID+"/"+other)
	if f.panicAt == name {
		panic("injected panic in " + name)
	}
	if f.failAt == name {
		return errFake
	}
	return nil
}

func (f *fake) ScopedCandidates(_ context.Context, tenantID, principalID string, _ AdmittedIntent) (TargetCandidates, error) {
	if err := f.hit("Candidates.ScopedCandidates", tenantID, principalID); err != nil {
		return TargetCandidates{}, err
	}
	return f.cands, f.candErr
}

func (f *fake) OwnsTarget(_ context.Context, tenantID, principalID string, _ AdmittedIntent) (bool, error) {
	if err := f.hit("Ownership.OwnsTarget", tenantID, principalID); err != nil {
		return false, err
	}
	return f.owned, f.ownErr
}

func (f *fake) SnapshotFreshness(_ context.Context, tenantID, snapshotID string) (SnapshotEvidence, error) {
	if err := f.hit("Evidence.SnapshotFreshness", tenantID, snapshotID); err != nil {
		return SnapshotEvidence{}, err
	}
	return f.evid, f.evidErr
}

func resolvedView() decide.IntentView {
	return decide.IntentView{Version: tVersion, RequiredSlots: map[string]decide.SlotStatus{"counterparty": decide.SlotResolved, "amount": decide.SlotResolved}}
}

func ambiguousView() decide.IntentView {
	return decide.IntentView{Version: tVersion, RequiredSlots: map[string]decide.SlotStatus{"counterparty": decide.SlotAmbiguous, "amount": decide.SlotResolved}}
}

func cand(id string, score int64, display, at string) RankedCandidate {
	return RankedCandidate{Candidate: decide.Candidate{ID: id, Display: display, Attrs: map[string]string{"time": at}}, Score: score}
}

func disambiguation() Disambiguation {
	return Disambiguation{
		Floor: 700, MaxCandidates: 4, MaxDigitRun: 4,
		AttributeOrder: []decide.AttributeSpec{{Name: "time", QuestionKey: "fin.q.which_time", LabelTemplate: "The one at {value}"}},
		OptionsKey:     "fin.q.which_transfer",
	}
}

func twoCandidates() []RankedCandidate {
	return []RankedCandidate{cand("cand_a", 900, "Transfer A", "08:00"), cand("cand_b", 800, "Transfer B", "09:00")}
}

func newFake() *fake {
	return &fake{
		cands: TargetCandidates{Disambiguation: disambiguation(), Ranked: twoCandidates()},
		owned: true,
		evid: SnapshotEvidence{Items: []SourceItem{
			item("evi_synth0001", "fin.src.ledger", 10, 60, contracts.EnumsV1FactStatusKNOWN),
			item("evi_synth0002", "fin.src.switch", 20, 30, contracts.EnumsV1FactStatusKNOWN),
		}},
	}
}

func item(id, source string, age, limit int64, status contracts.EnumsV1FactStatus) SourceItem {
	return SourceItem{
		Freshness: decide.Freshness{EvidenceID: id, EffectiveAt: 1_700_000_000, Source: source, AgeSeconds: age, EffectiveLimitSeconds: limit, LimitSetBy: decide.LimitBySource},
		Status:    status,
	}
}

type setup func(e *evaluation, f *fake)

func newEval(t *testing.T, view decide.IntentView, f *fake, tweak setup) *evaluation {
	t.Helper()
	bb, err := decide.NewBasisBuilder("1.0.0", 1_700_000_100)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Ports: Ports{Candidates: f, Ownership: f, Evidence: f}, Clock: clock.NewManual(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)), PackVersion: "1.0.0"} // SYNTHETIC
	e := &evaluation{
		svc: svc, intentID: "int_synth0001", version: tVersion, basis: bb,
		req: Request{Session: Session{TenantID: tTenant, PrincipalID: tPrincipal, Role: "customer"}, IntentRef: "int_synth0001@2", EvidenceSnapshotID: tSnapshot},
		intent: &AdmittedIntent{TenantID: tTenant, IntentID: "int_synth0001", Version: tVersion, IntentNode: "fin.payments.exception.investigate", PrincipalID: tPrincipal,
			View: view},
	}
	if tweak != nil {
		tweak(e, f)
	}
	return e
}

var errAfter = errors.New("the step after the one under test")

// runStep runs the one step through decide.Evaluate: the steps before it continue and the step after it fails, so the trace says what the step did.
func runStep(t *testing.T, e *evaluation, step decide.GateStep) (decide.Decision, []decide.TraceEntry) {
	t.Helper()
	fns := map[decide.GateStep]decide.StepFunc{decide.T7TargetResolution: e.t7, decide.T8BusinessOwnership: e.t8, decide.T9Evidence: e.t9}
	var steps decide.Steps
	for i := range steps {
		switch s := decide.GateStep(i + 1); {
		case s < step:
			steps[i] = func(context.Context) decide.StepResult { return decide.Continue() }
		case s == step:
			steps[i] = fns[step]
		default:
			steps[i] = func(context.Context) decide.StepResult { return decide.FailStep(errAfter) }
		}
	}
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return decide.Evaluate(c, tVersion, steps)
}

func optionsOf(d decide.Decision) []decide.Option {
	if q := d.Clarification(); q != nil {
		return q.Options
	}
	return nil
}

func codes(d decide.Decision) []string {
	var out []string
	for _, r := range d.Reasons() {
		out = append(out, r.String())
	}
	return out
}

// continued asserts that the step passed (and so the next step ran and failed, as set up by runStep).
func continued(t *testing.T, name string, d decide.Decision, trace []decide.TraceEntry, step decide.GateStep) {
	t.Helper()
	if len(trace) != int(step)+1 || trace[step-1].Result != decide.StepPassed || trace[step].Step != step+1 || trace[step].Result != decide.StepFailed {
		t.Errorf("%s: trace %v, want T%d PASSED and the evaluation to go on", name, trace, step)
	}
	_ = d
}

// stopped asserts one terminal decision with its outcome and its one reason code.
func stopped(t *testing.T, name string, d decide.Decision, trace []decide.TraceEntry, step decide.GateStep, outcome decide.Outcome, reason string) {
	t.Helper()
	if d.Outcome() != outcome || !reflect.DeepEqual(codes(d), []string{reason}) {
		t.Errorf("%s: %s %v, want %s [%s]", name, d.Outcome(), codes(d), outcome, reason)
	}
	if len(trace) != int(step) || trace[step-1].Step != step || trace[step-1].Result != decide.StepTerminal {
		t.Errorf("%s: trace %v, want it to end at T%d terminal", name, trace, step)
	}
}

// failed asserts UNKNOWN INTERNAL_ERROR with the step's result FAILED or PANICKED: never ALLOW.
func failed(t *testing.T, name string, d decide.Decision, trace []decide.TraceEntry, step decide.GateStep, result decide.TraceResult) {
	t.Helper()
	if d.Outcome() == decide.Allow {
		t.Fatalf("%s: ALLOW", name)
	}
	if d.Outcome() != decide.Unknown || !reflect.DeepEqual(codes(d), []string{"INTERNAL_ERROR"}) {
		t.Errorf("%s: %s %v, want UNKNOWN INTERNAL_ERROR", name, d.Outcome(), codes(d))
	}
	if len(trace) != int(step) || trace[step-1].Result != result {
		t.Errorf("%s: trace %v, want T%d %s", name, trace, step, result)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------------
// T7: target resolution (REQ-AMB-001, REQ-AMB-002, REQ-AMB-003, REQ-AMB-004, INV-06)

func TestT7AResolvedIntentPassesAndReadsNoCandidates(t *testing.T) {
	f := newFake()
	e := newEval(t, resolvedView(), f, nil)
	d, trace := runStep(t, e, decide.T7TargetResolution)
	continued(t, "resolved", d, trace, decide.T7TargetResolution)
	if len(f.calls) != 0 {
		t.Errorf("a resolved intent read %v", f.calls)
	}
}

func TestT7AnUnresolvedIntentAsksAQuestionAboutTheCandidatesAboveTheFloor(t *testing.T) {
	f := newFake()
	e := newEval(t, ambiguousView(), f, nil)
	d, trace := runStep(t, e, decide.T7TargetResolution)
	stopped(t, "unresolved", d, trace, decide.T7TargetResolution, decide.Clarify, "AMBIGUOUS_COUNTERPARTY")
	q := d.Clarification()
	if q == nil || !q.AllowNone || len(q.Options) != 2 || q.Options[0].CandidateID != "cand_a" || q.Options[1].CandidateID != "cand_b" {
		t.Fatalf("clarification %+v, want two options, cand_a then cand_b, with none-of-these allowed", q)
	}
	if q.Mode != decide.ModeAttribute || q.Attribute != "time" || q.QuestionKey != "fin.q.which_time" {
		t.Errorf("clarification %+v, want a question by the attribute that separates the candidates", q)
	}
	if want := (decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.NotEvaluated}); d.Authority() != want {
		t.Errorf("authority %+v, want %+v", d.Authority(), want)
	}
	if !slices.Equal(f.calls, []string{"Candidates.ScopedCandidates"}) || f.args[0] != tTenant+"/"+tPrincipal {
		t.Errorf("calls %v %v, want one candidates read scoped to the session's tenant and principal", f.calls, f.args)
	}
}

func TestT7AMaterialFlagMakesTheIntentUnresolvedEvenWithEverySlotResolved(t *testing.T) {
	f := newFake()
	v := resolvedView()
	v.MaterialFlags = []string{"AMBIGUOUS_ENTITY"}
	d, trace := runStep(t, newEval(t, v, f, nil), decide.T7TargetResolution)
	stopped(t, "material flag", d, trace, decide.T7TargetResolution, decide.Clarify, "AMBIGUOUS_COUNTERPARTY")
}

func TestT7NoQuestionIsAskedWhenNothingIsOfferable(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(f *fake)
	}{
		{"more candidates above the floor than the maximum", func(f *fake) {
			f.cands.Ranked = []RankedCandidate{cand("c1", 990, "T1", "01:00"), cand("c2", 980, "T2", "02:00"), cand("c3", 970, "T3", "03:00"), cand("c4", 960, "T4", "04:00"), cand("c5", 950, "T5", "05:00")}
		}},
		{"none above the floor", func(f *fake) {
			f.cands.Ranked = []RankedCandidate{cand("c1", 699, "T1", "01:00"), cand("c2", 650, "T2", "02:00")}
		}},
		{"no candidates", func(f *fake) { f.cands.Ranked = nil }},
		{"one candidate above the floor, which needs no question", func(f *fake) {
			f.cands.Ranked = []RankedCandidate{cand("c1", 900, "T1", "01:00"), cand("c2", 699, "T2", "02:00")}
		}},
		{"labels that cannot tell the options apart", func(f *fake) {
			f.cands.Ranked = []RankedCandidate{{Candidate: decide.Candidate{ID: "c1", Display: "Transfer"}, Score: 900}, {Candidate: decide.Candidate{ID: "c2", Display: "Transfer"}, Score: 800}}
		}},
		{"a label that would show more digits than the pack allows", func(f *fake) {
			f.cands.Ranked = []RankedCandidate{{Candidate: decide.Candidate{ID: "c1", Display: "Account 12345"}, Score: 900}, {Candidate: decide.Candidate{ID: "c2", Display: "Account 67890"}, Score: 800}}
		}},
	} {
		f := newFake()
		c.set(f)
		d, trace := runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
		stopped(t, c.name, d, trace, decide.T7TargetResolution, decide.Escalate, "AMBIGUOUS_COUNTERPARTY")
		if d.Clarification() != nil {
			t.Errorf("%s: an ESCALATE carries a question", c.name)
		}
	}
}

// REQ-AMB-003: the floor is inclusive, and a candidate below it is never offered.
func TestT7OnlyCandidatesAtOrAboveTheFloorAreOffered(t *testing.T) {
	f := newFake()
	f.cands.Ranked = []RankedCandidate{cand("cand_a", 900, "Transfer A", "08:00"), cand("cand_b", 700, "Transfer B", "09:00"), cand("cand_c", 699, "Transfer C", "10:00")}
	d, trace := runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
	stopped(t, "floor", d, trace, decide.T7TargetResolution, decide.Clarify, "AMBIGUOUS_COUNTERPARTY")
	var ids []string
	for _, o := range optionsOf(d) {
		ids = append(ids, o.CandidateID)
	}
	if !slices.Equal(ids, []string{"cand_a", "cand_b"}) {
		t.Errorf("offered %v, want cand_a and cand_b (the candidate at the floor is in, the one below is out)", ids)
	}
}

// REQ-AMB-004: a question never lists more options than the port's maximum, for any maximum and any number of candidates.
func TestT7TheOptionsNeverExceedThePortsMaximum(t *testing.T) {
	for max := 1; max <= 5; max++ {
		for n := 0; n <= 8; n++ {
			f := newFake()
			f.cands.Disambiguation.MaxCandidates = max
			f.cands.Ranked = nil
			for i := 0; i < n; i++ {
				f.cands.Ranked = append(f.cands.Ranked, cand(fmt.Sprintf("c%d", i), int64(990-i), fmt.Sprintf("Transfer %c", 'A'+i), fmt.Sprintf("0%d:00", i)))
			}
			d, _ := runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
			name := fmt.Sprintf("max %d, %d candidates", max, n)
			if n >= 2 && n <= max {
				if q := d.Clarification(); d.Outcome() != decide.Clarify || q == nil || len(q.Options) != n {
					t.Errorf("%s: %s %+v, want a question listing every candidate", name, d.Outcome(), q)
				}
			} else if d.Outcome() != decide.Escalate || d.Clarification() != nil {
				t.Errorf("%s: %s, want ESCALATE", name, d.Outcome())
			}
		}
	}
}

// INV-21: the order the port lists the candidates in changes nothing; Trust sorts by score, then by ID.
func TestT7TheDecisionDoesNotDependOnTheOrderOfTheCandidates(t *testing.T) {
	base := []RankedCandidate{cand("c3", 800, "Transfer C", "10:00"), cand("c1", 900, "Transfer A", "08:00"), cand("c2", 800, "Transfer B", "09:00")}
	var first []decide.Option
	for _, perm := range [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {1, 0, 2}} {
		f := newFake()
		f.cands.Ranked = nil
		for _, i := range perm {
			f.cands.Ranked = append(f.cands.Ranked, base[i])
		}
		d, _ := runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
		if d.Outcome() != decide.Clarify {
			t.Fatalf("outcome %s", d.Outcome())
		}
		opts := optionsOf(d)
		if len(opts) != len(base) {
			t.Fatalf("options %+v, want %d", opts, len(base))
		}
		if first == nil {
			first = opts
			if opts[0].CandidateID != "c1" || opts[1].CandidateID != "c2" || opts[2].CandidateID != "c3" {
				t.Fatalf("options %+v, want c1 (900) then the tie by ID, c2 then c3", opts)
			}
		}
		if !reflect.DeepEqual(opts, first) {
			t.Errorf("permutation %v: options %+v, want %+v", perm, opts, first)
		}
	}
}

func TestT7AnUnusableAnswerFromThePortIsUnknown(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(f *fake)
	}{
		{"a maximum of zero", func(f *fake) { f.cands.Disambiguation.MaxCandidates = 0 }},
		{"a negative floor", func(f *fake) { f.cands.Disambiguation.Floor = -1 }},
		{"a negative digit run", func(f *fake) { f.cands.Disambiguation.MaxDigitRun = -1 }},
		{"a candidate with no ID", func(f *fake) { f.cands.Ranked = append(f.cands.Ranked, cand("", 800, "Transfer Z", "11:00")) }},
		{"the same candidate twice", func(f *fake) { f.cands.Ranked = append(f.cands.Ranked, cand("cand_a", 800, "Transfer Z", "11:00")) }},
	} {
		f := newFake()
		c.set(f)
		d, trace := runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
		failed(t, c.name, d, trace, decide.T7TargetResolution, decide.StepFailed)
	}
}

func TestT7AnIntentViewForAnotherVersionOrNoIntentIsUnknown(t *testing.T) {
	v := ambiguousView()
	v.Version = tVersion + 1
	d, trace := runStep(t, newEval(t, v, newFake(), nil), decide.T7TargetResolution)
	failed(t, "view of another version", d, trace, decide.T7TargetResolution, decide.StepFailed)
	d, trace = runStep(t, newEval(t, ambiguousView(), newFake(), func(e *evaluation, _ *fake) { e.intent = nil }), decide.T7TargetResolution)
	failed(t, "no intent", d, trace, decide.T7TargetResolution, decide.StepFailed)
}

func TestT7AnErrorOrAPanicFromTheCandidatesPortIsUnknownNeverAllow(t *testing.T) {
	f := newFake()
	f.candErr = errFake
	d, trace := runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
	failed(t, "error", d, trace, decide.T7TargetResolution, decide.StepFailed)
	f = newFake()
	f.failAt = "Candidates.ScopedCandidates"
	d, trace = runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
	failed(t, "failure", d, trace, decide.T7TargetResolution, decide.StepFailed)
	f = newFake()
	f.panicAt = "Candidates.ScopedCandidates"
	d, trace = runStep(t, newEval(t, ambiguousView(), f, nil), decide.T7TargetResolution)
	failed(t, "panic", d, trace, decide.T7TargetResolution, decide.StepPanicked)
}

func TestT7TheSameInputsGiveTheSameDecision(t *testing.T) {
	d1, t1 := runStep(t, newEval(t, ambiguousView(), newFake(), nil), decide.T7TargetResolution)
	d2, t2 := runStep(t, newEval(t, ambiguousView(), newFake(), nil), decide.T7TargetResolution)
	if !reflect.DeepEqual(d1, d2) || !reflect.DeepEqual(t1, t2) {
		t.Errorf("two runs differ: %+v / %+v", d1, d2)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------------
// T8: business ownership (REQ-TRU-003, PERD 23.3: identical to not-found)

func TestT8AnOwnedTargetPassesAfterOneOwnershipRead(t *testing.T) {
	f := newFake()
	d, trace := runStep(t, newEval(t, resolvedView(), f, nil), decide.T8BusinessOwnership)
	continued(t, "owned", d, trace, decide.T8BusinessOwnership)
	if !slices.Equal(f.calls, []string{"Ownership.OwnsTarget"}) || f.args[0] != tTenant+"/"+tPrincipal {
		t.Errorf("calls %v %v, want one ownership read for the session's tenant and principal", f.calls, f.args)
	}
}

type observed struct {
	outcome   decide.Outcome
	reasons   []string
	authority decide.Authority
	version   int
	trace     []decide.TraceEntry
	calls     []string
	args      []string
}

func observe(t *testing.T, f *fake) observed {
	t.Helper()
	d, trace := runStep(t, newEval(t, resolvedView(), f, nil), decide.T8BusinessOwnership)
	return observed{d.Outcome(), codes(d), d.Authority(), d.IntentVersion(), trace, f.calls, f.args}
}

// The one decision for a target the principal does not own and for a target that does not exist, with the same trace and the same port calls, so that nothing about the
// target's existence can be read from the answer.
func TestT8NotOwnedAndNotFoundAreTheSameDecisionTheSameTraceAndTheSameCalls(t *testing.T) {
	notOwned := newFake()
	notOwned.owned = false
	notFound := newFake()
	notFound.owned, notFound.ownErr = false, ErrNotFound
	wrapped := newFake()
	wrapped.owned, wrapped.ownErr = true, fmt.Errorf("adapter: %w", ErrNotFound)

	a, b, c := observe(t, notOwned), observe(t, notFound), observe(t, wrapped)
	if a.outcome != decide.Reject || !reflect.DeepEqual(a.reasons, []string{"NOT_FOUND_OR_NOT_AUTHORIZED"}) {
		t.Fatalf("not owned: %s %v, want REJECT NOT_FOUND_OR_NOT_AUTHORIZED", a.outcome, a.reasons)
	}
	if want := (decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.Fail}); a.authority != want {
		t.Errorf("authority %+v, want %+v", a.authority, want)
	}
	if len(a.trace) != int(decide.T8BusinessOwnership) || a.trace[len(a.trace)-1].Result != decide.StepTerminal {
		t.Errorf("trace %v, want it to end at T8 terminal", a.trace)
	}
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(a, c) {
		t.Errorf("not owned, not found and a wrapped not found differ:\n%+v\n%+v\n%+v", a, b, c)
	}
}

func TestT8AnErrorOrAPanicFromTheOwnershipPortIsUnknownNeverAllow(t *testing.T) {
	f := newFake()
	f.ownErr = errFake
	d, trace := runStep(t, newEval(t, resolvedView(), f, nil), decide.T8BusinessOwnership)
	failed(t, "error", d, trace, decide.T8BusinessOwnership, decide.StepFailed)
	f = newFake()
	f.failAt = "Ownership.OwnsTarget"
	d, trace = runStep(t, newEval(t, resolvedView(), f, nil), decide.T8BusinessOwnership)
	failed(t, "failure", d, trace, decide.T8BusinessOwnership, decide.StepFailed)
	f = newFake()
	f.panicAt = "Ownership.OwnsTarget"
	d, trace = runStep(t, newEval(t, resolvedView(), f, nil), decide.T8BusinessOwnership)
	failed(t, "panic", d, trace, decide.T8BusinessOwnership, decide.StepPanicked)
}

// INV-06: a step after T7 never sees an unresolved intent; if one arrives, the step fails closed and reads nothing.
func TestT8AndT9AnUnresolvedIntentIsUnknownAndReadsNothing(t *testing.T) {
	for _, step := range []decide.GateStep{decide.T8BusinessOwnership, decide.T9Evidence} {
		f := newFake()
		d, trace := runStep(t, newEval(t, ambiguousView(), f, nil), step)
		failed(t, fmt.Sprintf("T%d", step), d, trace, step, decide.StepFailed)
		if len(f.calls) != 0 {
			t.Errorf("T%d read %v for an unresolved intent", step, f.calls)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------------
// T9: evidence (REQ-CTX-003, INV-16, INV-23)

func TestT9FreshEvidenceIsRecordedOnceFromTheReadAndTheStepContinues(t *testing.T) {
	f := newFake()
	e := newEval(t, resolvedView(), f, nil)
	d, trace := runStep(t, e, decide.T9Evidence)
	continued(t, "fresh", d, trace, decide.T9Evidence)
	if !slices.Equal(f.calls, []string{"Evidence.SnapshotFreshness"}) || f.args[0] != tTenant+"/"+tSnapshot {
		t.Errorf("calls %v %v, want one read of the snapshot the request names, for the session's tenant", f.calls, f.args)
	}
	b, err := e.basis.Basis()
	if err != nil {
		t.Fatal(err)
	}
	if b.EvidenceSnapshotID != tSnapshot {
		t.Errorf("recorded snapshot %q, want %q", b.EvidenceSnapshotID, tSnapshot)
	}
	var want []decide.Freshness
	for _, it := range f.evid.Items {
		want = append(want, it.Freshness)
	}
	if !reflect.DeepEqual(b.EvidenceFreshness, want) {
		t.Errorf("recorded freshness %+v, want the items the evidence port reported %+v", b.EvidenceFreshness, want)
	}
	if err := e.basis.RecordEvidence("evs_other", nil); !errors.Is(err, decide.ErrBasisRecorded) {
		t.Errorf("a second record: %v, want ErrBasisRecorded (the evidence was recorded once)", err)
	}
}

func TestT9EvidenceAtItsLimitIsFresh(t *testing.T) {
	f := newFake()
	f.evid.Items = []SourceItem{item("evi_synth0001", "fin.src.ledger", 60, 60, contracts.EnumsV1FactStatusKNOWN)}
	d, trace := runStep(t, newEval(t, resolvedView(), f, nil), decide.T9Evidence)
	continued(t, "at the limit", d, trace, decide.T9Evidence)
}

func TestT9AnUnavailableRequiredSourceEscalatesWithInsufficientEvidence(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(e *evaluation, f *fake)
		read bool
	}{
		{"the orchestrator reports an unavailable source", func(e *evaluation, f *fake) { e.req.EvidenceUnavailable = true }, false},
		{"the orchestrator reports it and names no snapshot", func(e *evaluation, f *fake) { e.req.EvidenceUnavailable, e.req.EvidenceSnapshotID = true, "" }, false},
		{"the evidence port reports a required source unavailable", func(e *evaluation, f *fake) { f.evid.RequiredUnavailable = true }, true},
		{"an item is UNAVAILABLE and young", func(e *evaluation, f *fake) {
			f.evid.Items[1] = item("evi_synth0002", "fin.src.switch", 0, 30, contracts.EnumsV1FactStatusUNAVAILABLE)
		}, true},
	} {
		f := newFake()
		e := newEval(t, resolvedView(), f, c.set)
		d, trace := runStep(t, e, decide.T9Evidence)
		stopped(t, c.name, d, trace, decide.T9Evidence, decide.Escalate, "INSUFFICIENT_EVIDENCE")
		if got := len(f.calls) == 1; got != c.read {
			t.Errorf("%s: calls %v, want a read: %v", c.name, f.calls, c.read)
		}
		if b, err := e.basis.Basis(); err != nil || b.EvidenceSnapshotID != "" || len(b.EvidenceFreshness) != 0 {
			t.Errorf("%s: a stopped step recorded evidence %+v (%v)", c.name, b, err)
		}
	}
}

func TestT9EvidencePastItsLimitEscalatesWithStaleEvidence(t *testing.T) {
	for _, c := range []struct {
		name  string
		items []SourceItem
	}{
		{"one second past the limit, status KNOWN", []SourceItem{item("evi_synth0001", "fin.src.ledger", 61, 60, contracts.EnumsV1FactStatusKNOWN)}},
		{"status STALE, age within the limit", []SourceItem{item("evi_synth0001", "fin.src.ledger", 5, 60, contracts.EnumsV1FactStatusSTALE)}},
		{"one fresh item and one stale", []SourceItem{item("evi_synth0001", "fin.src.ledger", 5, 60, contracts.EnumsV1FactStatusKNOWN), item("evi_synth0002", "fin.src.switch", 31, 30, contracts.EnumsV1FactStatusKNOWN)}},
	} {
		f := newFake()
		f.evid.Items = c.items
		e := newEval(t, resolvedView(), f, nil)
		d, trace := runStep(t, e, decide.T9Evidence)
		stopped(t, c.name, d, trace, decide.T9Evidence, decide.Escalate, "STALE_EVIDENCE")
		if b, _ := e.basis.Basis(); b.EvidenceSnapshotID != "" {
			t.Errorf("%s: a stopped step recorded evidence", c.name)
		}
	}
}

// Whichever order the items come in, unavailable wins over stale: the reason does not depend on the order (INV-21).
func TestT9UnavailableWinsOverStaleInAnyOrder(t *testing.T) {
	stale := item("evi_synth0001", "fin.src.ledger", 90, 60, contracts.EnumsV1FactStatusKNOWN)
	gone := item("evi_synth0002", "fin.src.switch", 0, 30, contracts.EnumsV1FactStatusUNAVAILABLE)
	for _, items := range [][]SourceItem{{stale, gone}, {gone, stale}} {
		f := newFake()
		f.evid.Items = items
		d, trace := runStep(t, newEval(t, resolvedView(), f, nil), decide.T9Evidence)
		stopped(t, "both", d, trace, decide.T9Evidence, decide.Escalate, "INSUFFICIENT_EVIDENCE")
	}
}

func TestT9EvidenceThatTheRecordCannotHoldOrACallerCannotNameIsUnknown(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(e *evaluation, f *fake)
		read bool
	}{
		{"no snapshot and no report that a source is unavailable", func(e *evaluation, f *fake) { e.req.EvidenceSnapshotID = "" }, false},
		{"a fact status outside the enum", func(e *evaluation, f *fake) { f.evid.Items[0].Status = "FRESH" }, true},
		{"an empty fact status", func(e *evaluation, f *fake) { f.evid.Items[0].Status = "" }, true},
		{"a limit of zero", func(e *evaluation, f *fake) {
			f.evid.Items[0] = item("evi_synth0001", "fin.src.ledger", 0, 0, contracts.EnumsV1FactStatusKNOWN)
		}, true},
		{"an item with no source", func(e *evaluation, f *fake) { f.evid.Items[0].Freshness.Source = "" }, true},
		{"a limit set by nobody known", func(e *evaluation, f *fake) { f.evid.Items[0].Freshness.LimitSetBy = "ANYONE" }, true},
		{"no basis builder", func(e *evaluation, f *fake) { e.basis = nil }, false},
		{"evidence already recorded for this evaluation", func(e *evaluation, f *fake) { _ = e.basis.RecordEvidence("evs_earlier", nil) }, true},
	} {
		f := newFake()
		d, trace := runStep(t, newEval(t, resolvedView(), f, c.set), decide.T9Evidence)
		failed(t, c.name, d, trace, decide.T9Evidence, decide.StepFailed)
		if got := len(f.calls) == 1; got != c.read {
			t.Errorf("%s: calls %v, want a read: %v", c.name, f.calls, c.read)
		}
	}
}

func TestT9AnErrorOrAPanicFromTheEvidencePortIsUnknownNeverAllow(t *testing.T) {
	f := newFake()
	f.evidErr = errFake
	e := newEval(t, resolvedView(), f, nil)
	d, trace := runStep(t, e, decide.T9Evidence)
	failed(t, "error", d, trace, decide.T9Evidence, decide.StepFailed)
	if b, _ := e.basis.Basis(); b.EvidenceSnapshotID != "" {
		t.Error("an error recorded evidence")
	}
	f = newFake()
	f.failAt = "Evidence.SnapshotFreshness"
	d, trace = runStep(t, newEval(t, resolvedView(), f, nil), decide.T9Evidence)
	failed(t, "failure", d, trace, decide.T9Evidence, decide.StepFailed)
	f = newFake()
	f.panicAt = "Evidence.SnapshotFreshness"
	d, trace = runStep(t, newEval(t, resolvedView(), f, nil), decide.T9Evidence)
	failed(t, "panic", d, trace, decide.T9Evidence, decide.StepPanicked)
}

func TestT9TheSameInputsGiveTheSameDecisionAndBasis(t *testing.T) {
	var digests []string
	for i := 0; i < 2; i++ {
		e := newEval(t, resolvedView(), newFake(), nil)
		_, trace := runStep(t, e, decide.T9Evidence)
		if trace[len(trace)-2].Result != decide.StepPassed {
			t.Fatalf("trace %v", trace)
		}
		dg, err := e.basis.Digest()
		if err != nil {
			t.Fatal(err)
		}
		digests = append(digests, dg)
	}
	if digests[0] != digests[1] {
		t.Errorf("two runs recorded different sets: %v", digests)
	}
}
