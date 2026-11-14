package orchestrator_test

// G1 write path, US-01 write and US-06 (STM-01 / REQ-STM-001, REQ-EXE-001, REQ-VER-001, INV-03, INV-07, INV-17, AGENT-07, AGENT-08, AGENT-21, PERD 28.2, 32.4,
// EC-E16). Ruling A204 (Founder-approved interim, relayed, not yet committed): the investigation state is a projection and the Record is the truth. The runner
// records EXECUTING after Execute returns, reads GRANT_SPENT and DISPATCH_ATTEMPTED from the Record (the Journal port) and resumes from the Record after a
// crash. Grants are issued with the decision (Decision.GrantIDs), with no planner caller in the POC. Every identifier is SYNTHETIC.
//
// These are the eight tests of the G1 card, written before RunWrite exists, so the package does not compile. The ports here are plain doubles of services built
// elsewhere (Trust, the Execution Controller, the Outcome verifier, the Record journal); the real-stack run against mock-case is the separate end-to-end task
// (execution/e2e). The doubles are scripted by the answer each port gives; none of them fakes a target fault.

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// wfakes adds the three write-path ports to the read-path doubles. Each call to Decide, Execute and Verify takes the next scripted answer (the last one repeats),
// so a test says what each round returns and the call log says what the runner did.
type wfakes struct {
	*fakes
	decisions []orchestrator.Decision
	receipts  []orchestrator.Receipt
	execErr   []error
	verdicts  []contracts.EnumsV1Verdict
	progress  orchestrator.Progress
	journalEr error

	nDecide, nExec, nVerify int
	executed                []string // grant IDs handed to Execute
	snapshots               []string // evidence snapshots Trust decided on
}

func pick[T any](xs []T, n int) T {
	if n >= len(xs) {
		return xs[len(xs)-1]
	}
	return xs[n]
}

func (w *wfakes) Decide(_ context.Context, s orchestrator.Session, _ string, e orchestrator.EvidenceRef) (orchestrator.Decision, error) {
	w.calls, w.session = append(w.calls, "decide"), s
	w.snapshots = append(w.snapshots, e.SnapshotID)
	d := pick(w.decisions, w.nDecide)
	w.nDecide++
	return d, nil
}

// Read gives every evaluation its own snapshot: a re-evaluation after a failed attempt must see a new one (INV-17, AGENT-08).
func (w *wfakes) Read(_ context.Context, s orchestrator.Session, _ string) (orchestrator.EvidenceRef, error) {
	w.calls, w.session = append(w.calls, "evidence"), s
	return orchestrator.EvidenceRef{SnapshotID: "evs_" + string(rune('A'+w.nDecide)) + "A11"}, nil
}

func (w *wfakes) Execute(_ context.Context, _ orchestrator.Session, grantID string) (orchestrator.Receipt, error) {
	w.calls = append(w.calls, "execute:"+grantID)
	w.executed = append(w.executed, grantID)
	var err error
	if len(w.execErr) > 0 {
		err = pick(w.execErr, w.nExec)
	}
	r := pick(w.receipts, w.nExec)
	w.nExec++
	return r, err
}

func (w *wfakes) Verify(_ context.Context, _ orchestrator.Session, _, grantID string) (contracts.EnumsV1Verdict, error) {
	w.calls = append(w.calls, "verify:"+grantID)
	v := pick(w.verdicts, w.nVerify)
	w.nVerify++
	return v, nil
}

func (w *wfakes) Progress(_ context.Context, _ orchestrator.Session, _, grantID string) (orchestrator.Progress, error) {
	w.calls = append(w.calls, "journal:"+grantID)
	return w.progress, w.journalEr
}

func writeDecision(grantIDs ...string) orchestrator.Decision {
	return orchestrator.Decision{ID: "dec_AA11", Outcome: contracts.EnumsV1DecisionOutcomeALLOW, Read: false, GrantIDs: grantIDs}
}

func newWrite(t *testing.T, w *wfakes) *orchestrator.WriteRunner {
	t.Helper()
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := orchestrator.NewWriteRunner(&orchestrator.Machine{Table: tab},
		orchestrator.Ports{Interpreter: w.fakes, Evidence: w, Trust: w, Explainer: w.fakes, Recorder: w.fakes, Admissions: w.fakes},
		orchestrator.WritePorts{Executor: w, Verifier: w, Journal: w})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func newWfakes() *wfakes {
	f := happy()
	f.expl = orchestrator.Explanation{Text: "Your complaint has been escalated to the team."}
	return &wfakes{fakes: f,
		decisions: []orchestrator.Decision{writeDecision("grt_AA11")},
		receipts:  []orchestrator.Receipt{{GrantID: "grt_AA11", Class: orchestrator.ReceiptApplied}},
		verdicts:  []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictACHIEVED},
	}
}

func count(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

// Test 1 (US-01, write): an allowed write walks PLANNED, EXECUTING, AWAITING_VERIFICATION, EXPLAINING, COMPLETED; EXECUTING is recorded after Execute returns; the
// explanation is asked only after an ACHIEVED verdict (AGENT-21: an HTTP 200 is not an outcome).
func TestAnAllowedWriteExecutesVerifiesAndOnlyThenExplains(t *testing.T) {
	w := newWfakes()
	res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []orchestrator.State{"INTERPRETING", "EVALUATING", "PLANNED", "EXECUTING", "AWAITING_VERIFICATION", "EXPLAINING", "COMPLETED"}
	if !reflect.DeepEqual(states(w.changes), want) || res.Investigation.State != "COMPLETED" || res.Answer == "" {
		t.Fatalf("states %v, final %+v, answer %q; want %v and COMPLETED with an answer", states(w.changes), res.Investigation, res.Answer, want)
	}
	order := []string{"decide", "record:PLANNED", "execute:grt_AA11", "record:EXECUTING", "record:AWAITING_VERIFICATION", "verify:grt_AA11", "record:EXPLAINING", "explain", "record:COMPLETED"}
	if got := tail(w.calls, "decide"); !reflect.DeepEqual(got, order) {
		t.Fatalf("calls %v, want %v", got, order)
	}
	if w.nExec != 1 {
		t.Errorf("Execute ran %d times, want 1", w.nExec)
	}
}

func tail(calls []string, from string) []string {
	for i, c := range calls {
		if c == from {
			return calls[i:]
		}
	}
	return nil
}

// Test 2 (US-06, INV-07, AGENT-07, AGENT-08): the reply is lost. The receipt says OUTCOME_UNKNOWN; the runner verifies by key and never dispatches a second time. A
// verdict that then says ACHIEVED completes; no second Execute, no second grant.
func TestALostReplyIsVerifiedByKeyAndNeverDispatchedASecondTime(t *testing.T) {
	w := newWfakes()
	w.receipts = []orchestrator.Receipt{{GrantID: "grt_AA11", Class: orchestrator.ReceiptUnknown}}
	w.verdicts = []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictACHIEVED}
	res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if w.nExec != 1 || w.nVerify != 1 || res.Investigation.State != "COMPLETED" {
		t.Fatalf("executes %d, verifies %d, final %s; want 1, 1, COMPLETED", w.nExec, w.nVerify, res.Investigation.State)
	}
}

// Test 3 (US-06, PERD 28.2): NOT_ACHIEVED and UNKNOWN take AWAITING_VERIFICATION to RECOVERING, then EVALUATING under a fresh evidence snapshot and a fresh
// decision (a new grant). The first grant is never executed again (AGENT-08). PENDING_EXTERNAL waits for the outcome instead.
func TestAVerdictOtherThanAchievedRecoversUnderAFreshSnapshotAndANewGrant(t *testing.T) {
	for _, v := range []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictNOTACHIEVED, contracts.EnumsV1VerdictUNKNOWN, contracts.EnumsV1VerdictPARTIALLYACHIEVED} {
		t.Run(string(v), func(t *testing.T) {
			w := newWfakes()
			w.decisions = []orchestrator.Decision{writeDecision("grt_AA11"), writeDecision("grt_BB22")}
			w.receipts = []orchestrator.Receipt{{GrantID: "grt_AA11", Class: orchestrator.ReceiptApplied}, {GrantID: "grt_BB22", Class: orchestrator.ReceiptApplied}}
			w.verdicts = []contracts.EnumsV1Verdict{v, contracts.EnumsV1VerdictACHIEVED}
			res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(w.executed, []string{"grt_AA11", "grt_BB22"}) || res.Investigation.State != "COMPLETED" {
				t.Fatalf("executed %v, final %s; want each grant once and COMPLETED", w.executed, res.Investigation.State)
			}
			if len(w.snapshots) != 2 || w.snapshots[0] == w.snapshots[1] {
				t.Errorf("snapshots %v: the second decision must be on a fresh snapshot", w.snapshots)
			}
			if !containsInOrder(states(w.changes), "AWAITING_VERIFICATION", "RECOVERING", "EVALUATING", "PLANNED") {
				t.Errorf("states %v: want AWAITING_VERIFICATION, RECOVERING, EVALUATING, PLANNED in that order", states(w.changes))
			}
		})
	}
	t.Run("PENDING_EXTERNAL", func(t *testing.T) {
		w := newWfakes()
		w.verdicts = []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictPENDINGEXTERNAL}
		res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
		if err != nil || res.Investigation.State != "AWAITING_EXTERNAL_OUTCOME" || w.nExec != 1 {
			t.Fatalf("final %s, executes %d, err %v; want AWAITING_EXTERNAL_OUTCOME after one execute", res.Investigation.State, w.nExec, err)
		}
	})
}

func containsInOrder(got []orchestrator.State, want ...orchestrator.State) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// Test 4 (INV-03, INV-17): a grant voided before it is spent (the E7 revalidation failed) takes PLANNED back to EVALUATING; the run decides again on a fresh
// snapshot and never dispatches the voided grant.
func TestAVoidedGrantReturnsToEvaluatingAndIsNeverDispatched(t *testing.T) {
	w := newWfakes()
	w.decisions = []orchestrator.Decision{writeDecision("grt_AA11"), writeDecision("grt_BB22")}
	w.receipts = []orchestrator.Receipt{{GrantID: "grt_AA11", Class: orchestrator.ReceiptVoided}, {GrantID: "grt_BB22", Class: orchestrator.ReceiptApplied}}
	res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !containsInOrder(states(w.changes), "PLANNED", "EVALUATING", "PLANNED", "EXECUTING") || res.Investigation.State != "COMPLETED" {
		t.Fatalf("states %v, final %s", states(w.changes), res.Investigation.State)
	}
	if count(w.calls, "verify:grt_AA11") != 0 {
		t.Errorf("a voided grant was verified: %v", w.calls)
	}
}

// Test 5 (AGENT-16, REQ-RES-004): the recovery budget is a number the caller gives, with no default. When it is spent the runner does not execute again: a Trust
// answer that allows another write is refused with ErrRecoveryBudgetSpent, and a Trust answer that escalates takes the ESCALATING arrow.
func TestAnExhaustedRecoveryBudgetNeverExecutesAgainAndEscalatesWhenTrustDoes(t *testing.T) {
	failing := func() *wfakes {
		w := newWfakes()
		w.decisions = []orchestrator.Decision{writeDecision("grt_AA11"), writeDecision("grt_BB22"), writeDecision("grt_CC33")}
		w.receipts = []orchestrator.Receipt{{Class: orchestrator.ReceiptApplied}}
		w.verdicts = []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictNOTACHIEVED}
		return w
	}
	t.Run("another write is refused", func(t *testing.T) {
		w := failing()
		res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
		if !errors.Is(err, orchestrator.ErrRecoveryBudgetSpent) || w.nExec != 2 || res.Investigation.State != "EVALUATING" {
			t.Fatalf("err %v, executes %d, final %s; want ErrRecoveryBudgetSpent, 2 executes (one first run and one recovery), EVALUATING", err, w.nExec, res.Investigation.State)
		}
	})
	t.Run("Trust escalates", func(t *testing.T) {
		w := failing()
		w.decisions = []orchestrator.Decision{writeDecision("grt_AA11"), writeDecision("grt_BB22"), {ID: "dec_ZZ99", Outcome: contracts.EnumsV1DecisionOutcomeESCALATE}}
		res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
		if err != nil || res.Investigation.State != "ESCALATING" {
			t.Fatalf("err %v, final %s; want ESCALATING", err, res.Investigation.State)
		}
	})
	t.Run("a negative budget does not build a run", func(t *testing.T) {
		if _, err := newWrite(t, newWfakes()).RunWrite(context.Background(), sess, turn, -1); !errors.Is(err, orchestrator.ErrBadTurn) {
			t.Fatalf("err %v, want ErrBadTurn", err)
		}
	})
}

// Test 6 (EC-E16, PERD 32.4): a failed Record append stops the run at that move. No grant is dispatched when PLANNED could not be recorded, and nothing is verified
// when EXECUTING could not be.
func TestAFailedRecordAppendStopsTheWritePathAtThatMove(t *testing.T) {
	cases := []struct {
		name        string
		failAt      int // the nth append fails
		wantExec    int
		wantVerify  int
		wantRecords int
	}{
		{"PLANNED cannot be recorded: no dispatch", 3, 0, 0, 2},
		{"EXECUTING cannot be recorded: no verify", 4, 1, 0, 3},
		{"AWAITING_VERIFICATION cannot be recorded: no verify", 5, 1, 0, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWfakes()
			w.failAt = c.failAt
			res, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1)
			if !errors.Is(err, orchestrator.ErrRecordFailed) || w.nExec != c.wantExec || w.nVerify != c.wantVerify || len(w.changes) != c.wantRecords || res.Answer != "" {
				t.Fatalf("err %v, executes %d, verifies %d, records %d, answer %q; want ErrRecordFailed, %d, %d, %d, no answer", err, w.nExec, w.nVerify, len(w.changes), res.Answer, c.wantExec, c.wantVerify, c.wantRecords)
			}
		})
	}
}

// Test 7 (A204, INV-07, AGENT-08): a crash at each boundary of the write path resumes from the Record. The Journal says what the Record holds for the grant. A grant
// that is not spent is executed; a grant whose GRANT_SPENT is on the Record is not spent again; a grant whose DISPATCH_ATTEMPTED is on the Record is never
// dispatched again and is verified by key. The investigation that ResumeWrite is given is the stored projection (state and version), not what the runner remembers.
func TestACrashAtEachBoundaryResumesFromTheRecordAndNeverDispatchesTwice(t *testing.T) {
	cases := []struct {
		name         string
		state        orchestrator.State // the stored projection when the process died
		progress     orchestrator.Progress
		wantExecutes int
		wantVerifies int
		verdict      contracts.EnumsV1Verdict // what the verifier finds by key
		wantFinal    orchestrator.State
	}{
		{"planned, nothing spent", "PLANNED", orchestrator.Progress{}, 1, 1, contracts.EnumsV1VerdictACHIEVED, "COMPLETED"},
		// Spent and never dispatched: the grant cannot be used again (AGENT-08); the run verifies by key, finds nothing done and recovers. The budget given to the resume
		// is 0, so the fresh decision that follows is not executed: the run stops in EVALUATING with ErrRecoveryBudgetSpent.
		{"planned, grant spent but not dispatched", "PLANNED", orchestrator.Progress{GrantSpent: true}, 0, 1, contracts.EnumsV1VerdictNOTACHIEVED, "EVALUATING"},
		{"planned, dispatch attempted and the reply lost", "PLANNED", orchestrator.Progress{GrantSpent: true, DispatchAttempted: true}, 0, 1, contracts.EnumsV1VerdictACHIEVED, "COMPLETED"},
		{"executing, dispatch attempted", "EXECUTING", orchestrator.Progress{GrantSpent: true, DispatchAttempted: true}, 0, 1, contracts.EnumsV1VerdictACHIEVED, "COMPLETED"},
		{"awaiting verification", "AWAITING_VERIFICATION", orchestrator.Progress{GrantSpent: true, DispatchAttempted: true}, 0, 1, contracts.EnumsV1VerdictACHIEVED, "COMPLETED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWfakes()
			w.progress = c.progress
			w.verdicts = []contracts.EnumsV1Verdict{c.verdict}
			inv := orchestrator.Investigation{ID: turn.InvestigationID, State: c.state, Version: 5}
			res, err := newWrite(t, w).ResumeWrite(context.Background(), sess, turn, inv, writeDecision("grt_AA11"), 0)
			if c.wantFinal == "EVALUATING" != errors.Is(err, orchestrator.ErrRecoveryBudgetSpent) || (err != nil && c.wantFinal != "EVALUATING") {
				t.Fatalf("err %v, want ErrRecoveryBudgetSpent only when the run ends in EVALUATING", err)
			}
			if w.nExec != c.wantExecutes || w.nVerify != c.wantVerifies || res.Investigation.State != c.wantFinal {
				t.Fatalf("executes %d, verifies %d, final %s; want %d, %d, %s (calls %v)", w.nExec, w.nVerify, res.Investigation.State, c.wantExecutes, c.wantVerifies, c.wantFinal, w.calls)
			}
			if res.Investigation.Version <= 5 {
				t.Errorf("version %d: a resumed run continues from the stored version, never from zero", res.Investigation.Version)
			}
		})
	}
	t.Run("a Journal that cannot be read does not execute", func(t *testing.T) {
		w := newWfakes()
		w.journalEr = errors.New("synthetic journal failure")
		inv := orchestrator.Investigation{ID: turn.InvestigationID, State: "PLANNED", Version: 5}
		if _, err := newWrite(t, w).ResumeWrite(context.Background(), sess, turn, inv, writeDecision("grt_AA11"), 1); err == nil || w.nExec != 0 {
			t.Fatalf("err %v, executes %d; want an error and no dispatch", err, w.nExec)
		}
	})
}

// Test 8 (INV-17): a re-evaluation seals a new snapshot. The first and the recovery decisions are made on different snapshots, and the decision never carries the
// grant of an earlier round. This is the read side of Test 3, asserted on its own so a runner that reused a snapshot fails here by name.
func TestAReEvaluationAfterRecoveryReadsEvidenceAgain(t *testing.T) {
	w := newWfakes()
	w.decisions = []orchestrator.Decision{writeDecision("grt_AA11"), writeDecision("grt_BB22")}
	w.receipts = []orchestrator.Receipt{{Class: orchestrator.ReceiptApplied}}
	w.verdicts = []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictNOTACHIEVED, contracts.EnumsV1VerdictACHIEVED}
	if _, err := newWrite(t, w).RunWrite(context.Background(), sess, turn, 1); err != nil {
		t.Fatal(err)
	}
	if got := count(w.calls, "evidence"); got != 2 {
		t.Fatalf("evidence read %d times, want 2 (once per evaluation): %v", got, w.calls)
	}
}

// The write path needs every write port, and the read path's tests are untouched by it.
func TestAWriteRunnerWithAMissingWritePortDoesNotBuild(t *testing.T) {
	w := newWfakes()
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	ports := orchestrator.Ports{Interpreter: w.fakes, Evidence: w, Trust: w, Explainer: w.fakes, Recorder: w.fakes, Admissions: w.fakes}
	for name, wp := range map[string]orchestrator.WritePorts{
		"no executor": {Verifier: w, Journal: w},
		"no verifier": {Executor: w, Journal: w},
		"no journal":  {Executor: w, Verifier: w},
	} {
		if _, err := orchestrator.NewWriteRunner(&orchestrator.Machine{Table: tab}, ports, wp); !errors.Is(err, orchestrator.ErrNotWired) {
			t.Errorf("%s: err %v, want ErrNotWired", name, err)
		}
	}
}
