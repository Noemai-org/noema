package orchestrator_test

// G1 slice, US-01 (read-only), STM-01 / REQ-STM-001, INV-12, INV-03, PERD 32.4 (the Record comes first). The runner walks one investigation through the
// PERD 28.2 table for a question that only reads: RECEIVED, INTERPRETING, EVALUATING, EXPLAINING, COMPLETED. Every move is applied by the Machine (so an
// unlisted arrow is refused), and its INVESTIGATION_STATE_CHANGED change is appended to the Record BEFORE the run goes on; if the append fails the run stops and no
// answer is returned. The runner decides nothing itself: interpretation, evidence, the decision and the explanation come through ports. Every identifier is SYNTHETIC.
// The ports here are plain test doubles of services that are built elsewhere (model service client, Evidence Layer, Trust, explanation renderer, the Record);
// the Record-first failures use a recorder double until the Record-backed recorder exists (STM-01 slice 2).

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
)

type fakes struct {
	interp   orchestrator.Interpretation
	interpEr error
	evid     orchestrator.EvidenceRef
	evidEr   error
	decision orchestrator.Decision
	decideEr error
	expl     orchestrator.Explanation
	explEr   error
	failAt   int // the nth AppendChange fails (1-based); 0 means never

	calls   []string
	changes []orchestrator.Change
	session orchestrator.Session // the session every port was called with
}

func (f *fakes) Interpret(_ context.Context, s orchestrator.Session, _ orchestrator.Turn) (orchestrator.Interpretation, error) {
	f.calls, f.session = append(f.calls, "interpret"), s
	return f.interp, f.interpEr
}
func (f *fakes) Read(_ context.Context, s orchestrator.Session, _ string) (orchestrator.EvidenceRef, error) {
	f.calls, f.session = append(f.calls, "evidence"), s
	return f.evid, f.evidEr
}
func (f *fakes) Decide(_ context.Context, s orchestrator.Session, _ string, _ orchestrator.EvidenceRef) (orchestrator.Decision, error) {
	f.calls, f.session = append(f.calls, "decide"), s
	return f.decision, f.decideEr
}
func (f *fakes) Explain(_ context.Context, s orchestrator.Session, _ string) (orchestrator.Explanation, error) {
	f.calls, f.session = append(f.calls, "explain"), s
	return f.expl, f.explEr
}
func (f *fakes) AppendChange(_ context.Context, s orchestrator.Session, c orchestrator.Change) error {
	f.calls, f.session = append(f.calls, "record:"+string(c.To)), s
	if f.failAt != 0 && len(f.changes)+1 == f.failAt {
		return errors.New("synthetic record failure")
	}
	f.changes = append(f.changes, c)
	return nil
}

func happy() *fakes {
	return &fakes{
		interp:   orchestrator.Interpretation{Kind: orchestrator.Admitted, IntentRef: "int_AA11"},
		evid:     orchestrator.EvidenceRef{SnapshotID: "evs_AA11"},
		decision: orchestrator.Decision{ID: "dec_AA11", Outcome: contracts.EnumsV1DecisionOutcomeALLOW, Read: true},
		expl:     orchestrator.Explanation{Text: "Your payment is pending at the destination."},
	}
}

var (
	sess = orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_01"}
	turn = orchestrator.Turn{InvestigationID: "inv_AA11", CorrelationID: "corr_AA11", Text: "Where is my money?", InputReceivedRef: "recev_AA11"}
)

func runner(t *testing.T, f *fakes) *orchestrator.Runner {
	t.Helper()
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := orchestrator.NewRunner(&orchestrator.Machine{Table: tab}, orchestrator.Ports{Interpreter: f, Evidence: f, Trust: f, Explainer: f, Recorder: f})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func states(cs []orchestrator.Change) []orchestrator.State {
	var out []orchestrator.State
	for _, c := range cs {
		out = append(out, c.To)
	}
	return out
}

func TestAReadWalksTheTableAndRecordsEveryMoveBeforeTheNextStep(t *testing.T) {
	f := happy()
	res, err := runner(t, f).RunRead(context.Background(), sess, turn)
	if err != nil {
		t.Fatal(err)
	}
	want := []orchestrator.State{"INTERPRETING", "EVALUATING", "EXPLAINING", "COMPLETED"}
	if !reflect.DeepEqual(states(f.changes), want) || res.Investigation.State != "COMPLETED" || res.Investigation.Version != 4 {
		t.Fatalf("states %v, final %+v; want %v and COMPLETED at version 4", states(f.changes), res.Investigation, want)
	}
	if res.Answer != "Your payment is pending at the destination." {
		t.Errorf("answer %q", res.Answer)
	}
	// Record first: each move is recorded before the port work that follows it.
	order := []string{"record:INTERPRETING", "interpret", "record:EVALUATING", "evidence", "decide", "record:EXPLAINING", "explain", "record:COMPLETED"}
	if !reflect.DeepEqual(f.calls, order) {
		t.Errorf("calls %v, want %v", f.calls, order)
	}
}

// INV-12: every port is called with the session of the authenticated caller, and a session without a tenant or a principal is refused before anything runs.
func TestEveryPortSeesTheSessionAndAnIncompleteSessionIsRefusedFirst(t *testing.T) {
	f := happy()
	if _, err := runner(t, f).RunRead(context.Background(), sess, turn); err != nil || f.session != sess {
		t.Fatalf("session seen by the ports %+v, err %v; want %+v", f.session, err, sess)
	}
	for name, s := range map[string]orchestrator.Session{"no tenant": {PrincipalID: "usr_01"}, "no principal": {TenantID: "ten_aa"}, "empty": {}} {
		f := happy()
		_, err := runner(t, f).RunRead(context.Background(), s, turn)
		if !errors.Is(err, orchestrator.ErrNoSession) || len(f.calls) != 0 {
			t.Errorf("%s: err %v, calls %v; want ErrNoSession and nothing run", name, err, f.calls)
		}
	}
}

func TestATurnWithoutItsIdentifiersIsRefusedBeforeAnythingRuns(t *testing.T) {
	for name, tr := range map[string]orchestrator.Turn{
		"no investigation": {CorrelationID: "corr_AA11", Text: "x", InputReceivedRef: "recev_AA11"}, "no correlation": {InvestigationID: "inv_AA11", Text: "x", InputReceivedRef: "recev_AA11"}, "no text": {InvestigationID: "inv_AA11", CorrelationID: "corr_AA11", InputReceivedRef: "recev_AA11"},
		"a control character in the id": {InvestigationID: "inv_AA\n11", CorrelationID: "corr_AA11", Text: "x", InputReceivedRef: "recev_AA11"},
	} {
		f := happy()
		_, err := runner(t, f).RunRead(context.Background(), sess, tr)
		if !errors.Is(err, orchestrator.ErrBadTurn) || len(f.calls) != 0 {
			t.Errorf("%s: err %v, calls %v; want ErrBadTurn and nothing run", name, err, f.calls)
		}
	}
}

// A161 (relayed, to be committed): the input gateway writes INPUT_RECEIVED; the orchestrator does not. It refuses a turn that does not carry the reference of
// that recorded event, before it records or runs anything, because a reconstruction would otherwise begin at a move from a state with no event (PERD 28.2:
// RECEIVED is entered by INPUT_RECEIVED; PERD 32.4: the Record comes first). The reference is a Record event ID (common.v1 RecordEventId: recev_ and 2 to 32 letters or digits).
func TestATurnWithoutTheRecordedInputReceivedIsRefusedBeforeAnythingIsRecordedOrRun(t *testing.T) {
	for name, ref := range map[string]string{
		"none": "", "another prefix": "evt_AA11", "an intent id": "int_AA11", "too short": "recev_A", "too long": "recev_" + "A123456789B123456789C123456789D12",
		"a control character": "recev_AA\n11", "a space": "recev_AA 11", "a symbol": "recev_AA-11", "a trailing newline": "recev_AA11\n",
	} {
		f := happy()
		tr := turn
		tr.InputReceivedRef = ref
		res, err := runner(t, f).RunRead(context.Background(), sess, tr)
		if !errors.Is(err, orchestrator.ErrNoInputReceived) {
			t.Errorf("%s: err %v, want ErrNoInputReceived", name, err)
		}
		if len(f.calls) != 0 || len(f.changes) != 0 || len(res.Changes) != 0 || res.Answer != "" {
			t.Errorf("%s: calls %v, recorded %v, result %+v; want nothing recorded or run", name, f.calls, f.changes, res)
		}
	}
	f := happy()
	tr := turn
	tr.InputReceivedRef = "recev_" + "A123456789B123456789C123456789D1"
	if _, err := runner(t, f).RunRead(context.Background(), sess, tr); err != nil {
		t.Errorf("a reference of 32 characters is valid: %v", err)
	}
}

// Record first (PERD 32.4, EC-E16): when the append of move n fails, the run stops there, no later port is called, and no answer is returned.
func TestARecordFailureStopsTheRunAtThatMoveAndWithholdsTheAnswer(t *testing.T) {
	for n := 1; n <= 4; n++ {
		f := happy()
		f.failAt = n
		res, err := runner(t, f).RunRead(context.Background(), sess, turn)
		if !errors.Is(err, orchestrator.ErrRecordFailed) {
			t.Fatalf("append %d failing: err %v; want ErrRecordFailed", n, err)
		}
		if res.Answer != "" || len(f.changes) != n-1 {
			t.Errorf("append %d failing: answer %q, %d changes recorded; want no answer and %d changes", n, res.Answer, len(f.changes), n-1)
		}
		// The investigation the caller gets back has not advanced past what the Record holds: n-1 moves, none unrecorded.
		if res.Investigation.Version != int64(n-1) || len(res.Changes) != n-1 || (n > 1 && res.Investigation.State != want4()[n-2]) {
			t.Errorf("append %d failing: returned %+v with %d changes; want version %d, %d changes and the state of the last recorded move", n, res.Investigation, len(res.Changes), n-1, n-1)
		}
		if got := f.calls[len(f.calls)-1]; got != "record:"+string(want4()[n-1]) {
			t.Errorf("append %d failing: the last call was %q; nothing may run after the failed append", n, got)
		}
	}
}

func want4() []orchestrator.State {
	return []orchestrator.State{"INTERPRETING", "EVALUATING", "EXPLAINING", "COMPLETED"}
}

func TestInterpretationOutcomesTakeTheTableArrows(t *testing.T) {
	for name, c := range map[string]struct {
		kind orchestrator.InterpretationKind
		want orchestrator.State
	}{"the schema gate failed": {orchestrator.GateFailed, "REJECTED_AT_GATE"}, "a clarification is needed": {orchestrator.NeedsClarification, "AWAITING_CLARIFICATION"}} {
		f := happy()
		f.interp = orchestrator.Interpretation{Kind: c.kind, IntentRef: "int_AA11"}
		res, err := runner(t, f).RunRead(context.Background(), sess, turn)
		if err != nil || res.Investigation.State != c.want || res.Answer != "" {
			t.Errorf("%s: %+v, %v; want %s and no answer", name, res.Investigation, err, c.want)
		}
		for _, call := range f.calls {
			if call == "evidence" || call == "decide" || call == "explain" {
				t.Errorf("%s: %s ran after the interpretation was refused", name, call)
			}
		}
	}
	f := happy()
	f.interp = orchestrator.Interpretation{Kind: "SURPRISE"}
	if _, err := runner(t, f).RunRead(context.Background(), sess, turn); !errors.Is(err, orchestrator.ErrBadPortAnswer) {
		t.Errorf("an unknown interpretation kind: %v; want ErrBadPortAnswer", err)
	}
}

func TestEveryDecisionOutcomeTakesItsTableArrowAndOnlyAnAllowedReadExplains(t *testing.T) {
	for outcome, want := range map[contracts.EnumsV1DecisionOutcome]orchestrator.State{
		contracts.EnumsV1DecisionOutcomeCLARIFY: "AWAITING_CLARIFICATION", contracts.EnumsV1DecisionOutcomeCONFIRM: "AWAITING_CONFIRMATION", contracts.EnumsV1DecisionOutcomeWAIT: "WAITING",
		contracts.EnumsV1DecisionOutcomeESCALATE: "ESCALATING", contracts.EnumsV1DecisionOutcomeREJECT: "REJECTED", contracts.EnumsV1DecisionOutcomeUNKNOWN: "BLOCKED_UNKNOWN",
	} {
		f := happy()
		f.decision = orchestrator.Decision{ID: "dec_BB22", Outcome: outcome}
		res, err := runner(t, f).RunRead(context.Background(), sess, turn)
		if err != nil || res.Investigation.State != want || res.Answer != "" {
			t.Errorf("%s: %+v, %v; want %s and no answer", outcome, res.Investigation, err, want)
		}
		for _, call := range f.calls {
			if call == "explain" {
				t.Errorf("%s: the explainer ran for a decision that is not an allowed read", outcome)
			}
		}
	}
	// An ALLOW that is not a read is an action: this run only reads, so it stops in EVALUATING and executes nothing (INV-03).
	f := happy()
	f.decision = orchestrator.Decision{ID: "dec_BB22", Outcome: contracts.EnumsV1DecisionOutcomeALLOW, Read: false}
	res, err := runner(t, f).RunRead(context.Background(), sess, turn)
	if !errors.Is(err, orchestrator.ErrNotARead) || res.Investigation.State != "EVALUATING" {
		t.Errorf("an ALLOW that is an action: %+v, %v; want ErrNotARead in EVALUATING", res.Investigation, err)
	}
	// An outcome outside the contract's enum is refused, never guessed.
	f = happy()
	f.decision = orchestrator.Decision{ID: "dec_BB22", Outcome: "MAYBE"}
	if _, err := runner(t, f).RunRead(context.Background(), sess, turn); !errors.Is(err, orchestrator.ErrBadPortAnswer) {
		t.Errorf("an unknown outcome: %v; want ErrBadPortAnswer", err)
	}
}

func TestABlockedExplanationEndsInExplainingBlockedAndNeverInCompleted(t *testing.T) {
	f := happy()
	f.expl = orchestrator.Explanation{Blocked: true}
	res, err := runner(t, f).RunRead(context.Background(), sess, turn)
	if err != nil || res.Investigation.State != "EXPLAINING_BLOCKED" || res.Answer != "" {
		t.Fatalf("%+v, %v; want EXPLAINING_BLOCKED and no answer", res.Investigation, err)
	}
}

// A port error is an infrastructure failure, not an answer: the run stops where it is, returns the error, and does not make up the next move.
func TestAPortFailureStopsTheRunInItsStateAndMakesNoMove(t *testing.T) {
	boom := errors.New("synthetic port failure")
	for name, c := range map[string]struct {
		set  func(*fakes)
		want orchestrator.State
	}{
		"interpreter": {func(f *fakes) { f.interpEr = boom }, "INTERPRETING"}, "evidence": {func(f *fakes) { f.evidEr = boom }, "EVALUATING"},
		"trust": {func(f *fakes) { f.decideEr = boom }, "EVALUATING"}, "explainer": {func(f *fakes) { f.explEr = boom }, "EXPLAINING"},
	} {
		f := happy()
		c.set(f)
		res, err := runner(t, f).RunRead(context.Background(), sess, turn)
		if !errors.Is(err, boom) || res.Investigation.State != c.want || res.Answer != "" {
			t.Errorf("%s failing: %+v, %v; want the port error, state %s and no answer", name, res.Investigation, err, c.want)
		}
	}
}

func TestARunnerThatIsNotFullyWiredDoesNotBuild(t *testing.T) {
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	f := happy()
	full := orchestrator.Ports{Interpreter: f, Evidence: f, Trust: f, Explainer: f, Recorder: f}
	for name, p := range map[string]orchestrator.Ports{
		"no interpreter": {Evidence: f, Trust: f, Explainer: f, Recorder: f}, "no evidence": {Interpreter: f, Trust: f, Explainer: f, Recorder: f},
		"no trust": {Interpreter: f, Evidence: f, Explainer: f, Recorder: f}, "no explainer": {Interpreter: f, Evidence: f, Trust: f, Recorder: f},
		"no recorder": {Interpreter: f, Evidence: f, Trust: f, Explainer: f},
	} {
		if _, err := orchestrator.NewRunner(&orchestrator.Machine{Table: tab}, p); !errors.Is(err, orchestrator.ErrNotWired) {
			t.Errorf("%s: %v; want ErrNotWired", name, err)
		}
	}
	if _, err := orchestrator.NewRunner(nil, full); !errors.Is(err, orchestrator.ErrNotWired) {
		t.Errorf("no machine: %v; want ErrNotWired", err)
	}
	if _, err := orchestrator.NewRunner(&orchestrator.Machine{}, full); !errors.Is(err, orchestrator.ErrNotWired) {
		t.Errorf("a machine with no table: %v; want ErrNotWired", err)
	}
}
