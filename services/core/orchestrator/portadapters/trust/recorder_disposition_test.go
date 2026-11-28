package trustport

// TRU-10, ruling A503 (interim; A507, A508): an ESCALATE disposition is its own Trust decision (no step, no grant) recorded first through the Recorder, and each escalation action
// decision links it. The intake's RecordRequest carries the optional DispositionDecisionID and this adapter hands it to the Writer as it is; the Writer's own rules (an existing
// same-tenant ESCALATE decision, ErrDispositionNotValid) are not repeated here. REQ-TRU-010; INV-25, EC-E16. All data is SYNTHETIC. The fake-port tests run anywhere; the testpg test
// runs in CI only (pgsodium).

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// The request the Writer gets names the disposition the intake's request names, and names none when the request names none.
func TestRecordHandsTheWriterTheDispositionDecisionIDOfTheRequest(t *testing.T) {
	for name, c := range map[string]struct {
		steps       int
		disposition string
	}{"a disposition with a step": {1, "dec_01OTHER"}, "no step and none": {0, ""}} {
		t.Run(name, func(t *testing.T) {
			disposition := c.disposition
			r := recRequest(t, c.steps)
			r.DispositionDecisionID = disposition
			w := &recWriter{res: recGrants(c.steps)}
			if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), w).Record(recCtx(t), r); err != nil {
				t.Fatal(err)
			}
			if len(w.got) != 1 || w.got[0].DispositionDecisionID != disposition {
				t.Fatalf("the Writer got %d requests, the first naming %q, want one naming %q", len(w.got), disp(w.got), disposition)
			}
		})
	}
}

// disp is the disposition the first request names, for a failure message that does not print the whole request.
func disp(got []recorder.Request) string {
	if len(got) == 0 {
		return ""
	}
	return got[0].DispositionDecisionID
}

// escalateDisposition is a record request of an ESCALATE decision with no step and no plan hash, as the escalation path records its disposition (A503); n is its intent hash (recHash(n)), so
// n = 1 is the intent hash of recRequest and the disposition of an action decision built by it (A542). Its sealed snapshot is its own, so it is another input than the action decision.
func escalateDisposition(t *testing.T, n byte) intake.RecordRequest {
	t.Helper()
	r := rnaRequest(t, "REJECT", false)
	b := recBuilderAt(t, "evs_01DSP")
	reason, err := decide.Core("NOT_AUTHORIZED")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	if !ok {
		t.Fatal("not unresolved")
	}
	d, err := u.Escalate(reason)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	digest, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	r.Decision, r.Evaluated, r.BasisDigest, r.IntentHash = d, b.Sealed(), digest, recHash(n)
	return r
}

// The real path: the disposition is recorded first through the adapter and the real Writer, which returns its decision ID; an action decision that names it is stored with the link,
// and the chain verifies (A503: no INSERT, no seeded row).
func TestAnActionDecisionIsStoredWithTheDispositionRecordedFirstThroughTheAdapter(t *testing.T) {
	w := newRecWorldWith(t, recorder.NewEscalationSet(recCap))
	disp, err := w.a.Record(recCtx(t), escalateDisposition(t, 1))
	if err != nil || disp.DecisionID == "" || len(disp.GrantIDs) != 0 {
		t.Fatalf("the disposition: %+v, %v; want a decision ID and no grant", disp, err)
	}
	r := recRequest(t, 1)
	r.DispositionDecisionID = disp.DecisionID
	act, err := w.a.Record(recCtx(t), r)
	if err != nil || act.DecisionID == "" || len(act.GrantIDs) != 1 {
		t.Fatalf("the action decision: %+v, %v", act, err)
	}
	var linked string
	if err := w.admin.QueryRow(recCtx(t), `SELECT disposition_decision_id FROM decision_record WHERE decision_id = $1`, act.DecisionID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != disp.DecisionID {
		t.Fatalf("the stored action decision links %q, want the disposition %q", linked, disp.DecisionID)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record WHERE disposition_decision_id IS NULL AND outcome = 'ALLOW'`); n != 0 {
		t.Fatalf("%d ALLOW decisions with no disposition", n)
	}
	if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n == 0 {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}

// Steps of the escalation plan if and only if a disposition (A503 and the Founder's order of 22 Nov 2026, rescoped by A534): a decision with a step of the plan and no disposition, and a decision with no step that names one, are refused
// before any transaction opens, as an unusable request (SR on noema#883).
func TestAStepsAndDispositionThatDoNotGoTogetherAreRefusedBeforeAnyTransaction(t *testing.T) {
	cases := map[string]func(t *testing.T) intake.RecordRequest{
		"steps and no disposition": func(t *testing.T) intake.RecordRequest {
			r := recRequest(t, 1)
			r.DispositionDecisionID = ""
			return r
		},
		"a disposition and no steps": func(t *testing.T) intake.RecordRequest {
			r := recRequest(t, 0)
			r.DispositionDecisionID = "dec_01DISP"
			return r
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			b, w := &recBegin{tx: &recTx{}}, &recWriter{}
			if _, err := recAdapterWith(t, b, newRecFacts(), w, recorder.NewEscalationSet(recCap)).Record(recCtx(t), build(t)); !errors.Is(err, ErrRecordRequest) {
				t.Fatalf("err %v, want ErrRecordRequest", err)
			}
			if len(b.tenants) != 0 || len(w.got) != 0 {
				t.Fatal("a refused request reached the database")
			}
		})
	}
}

// A534 (interim, relayed, not yet committed): the disposition rule applies only to a decision with a step of the escalation plan. Before any transaction opens, a step of the plan with no
// disposition is refused, and a step outside the plan with no disposition is not (the complaint write). A decision with a disposition and no step is refused either way.
func TestTheRuleBeforeTheTransactionAppliesOnlyToStepsOfTheEscalationPlan(t *testing.T) {
	plan := recorder.NewEscalationSet(recCap)
	other := recorder.NewEscalationSet("fin.cap.some_other_plan_step") // recCap is outside this plan
	for name, c := range map[string]struct {
		set     recorder.EscalationSet
		steps   int
		disp    string
		refused bool
	}{
		"a step of the plan, no disposition":        {plan, 1, "", true},
		"a step of the plan, its disposition":       {plan, 1, "dec_01DISP", false},
		"a step outside the plan, no disposition":   {other, 1, "", false},
		"no step and a disposition":                 {plan, 0, "dec_01DISP", true},
		"no step, no disposition":                   {plan, 0, "", false},
		"a step outside the plan and a disposition": {other, 1, "dec_01DISP", false}, // the Writer refuses it (ErrStepOutsideEscalation), not the adapter
		"the empty plan, a step and no disposition": {recorder.EscalationSet{}, 1, "", false},
		"the empty plan, no step and a disposition": {recorder.EscalationSet{}, 0, "dec_01DISP", true},
	} {
		r := recRequest(t, c.steps)
		r.DispositionDecisionID = c.disp
		b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(c.steps)}
		_, err := recAdapterWith(t, b, newRecFacts(), w, c.set).Record(recCtx(t), r)
		if c.refused {
			if !errors.Is(err, ErrRecordRequest) || len(b.tenants) != 0 || len(w.got) != 0 {
				t.Errorf("%s: err %v, a transaction opened or the Writer was reached; want ErrRecordRequest before any transaction", name, err)
			}
			continue
		}
		if err != nil || len(w.got) != 1 {
			t.Errorf("%s: err %v, the Writer got %d requests; want the request to reach the Writer", name, err, len(w.got))
		}
	}
}

// Through the real adapter and the real Writer: a write outside the escalation plan is recorded with its grant and no disposition (A534).
func TestAWriteOutsideThePlanNeedsNoDispositionThroughTheAdapter(t *testing.T) {
	outside := newRecWorldWith(t, recorder.NewEscalationSet("fin.cap.some_other_plan_step"))
	r := recRequest(t, 1)
	r.DispositionDecisionID = ""
	res, err := outside.a.Record(recCtx(t), r)
	if err != nil || res.DecisionID == "" || len(res.GrantIDs) != 1 {
		t.Fatalf("a step outside the plan with no disposition: %+v %v; want a decision and one grant", res, err)
	}
}

// A disposition of another intent hash (the same tenant's ESCALATE, an existing one) is not the action decision's (A542), through the real adapter.
func TestADispositionOfAnotherIntentIsRefusedThroughTheAdapter(t *testing.T) {
	w := newRecWorldWith(t, recorder.NewEscalationSet(recCap))
	disp, err := w.a.Record(recCtx(t), escalateDisposition(t, 5)) // an ESCALATE for the intent hash recHash(5)
	if err != nil {
		t.Fatal(err)
	}
	r := recRequest(t, 1) // the intent hash recHash(1)
	r.DispositionDecisionID = disp.DecisionID
	before := w.count(t, `SELECT count(*) FROM decision_record`)
	if _, err := w.a.Record(recCtx(t), r); !errors.Is(err, recorder.ErrDispositionNotValid) {
		t.Fatalf("a disposition of another intent hash: %v, want ErrDispositionNotValid", err)
	}
	if after := w.count(t, `SELECT count(*) FROM decision_record`); after != before {
		t.Errorf("the refusal changed the decisions from %d to %d", before, after)
	}
}

// A534 (interim, relayed, not yet committed): an ESCALATE disposition replayed is the stored disposition, by the same decision key (A186: one decision per full input tuple). The second
// Record of the same ESCALATE returns the first's decision ID, writes no row and no event, and the action decision that names it is stored once with its link. REQ-TRU-010; INV-21, INV-25.
func TestAReplayedEscalateReturnsTheStoredDispositionByTheSameDecisionKey(t *testing.T) {
	w := newRecWorldWith(t, recorder.NewEscalationSet(recCap))
	first, err := w.a.Record(recCtx(t), escalateDisposition(t, 1))
	if err != nil || first.DecisionID == "" || len(first.GrantIDs) != 0 {
		t.Fatalf("the first record: %+v, %v", first, err)
	}
	r0, g0, e0 := w.rows(t)
	second, err := w.a.Record(recCtx(t), escalateDisposition(t, 1))
	if err != nil || second.DecisionID != first.DecisionID || len(second.GrantIDs) != 0 {
		t.Fatalf("the replay: %+v, %v; want the stored decision %q", second, err, first.DecisionID)
	}
	if r1, g1, e1 := w.rows(t); r1 != r0 || g1 != g0 || e1 != e0 {
		t.Errorf("a replay changed the rows: records %d to %d, grants %d to %d, events %d to %d", r0, r1, g0, g1, e0, e1)
	}
	// another input is another decision: a different intent hash is not the same key
	other, err := w.a.Record(recCtx(t), escalateDisposition(t, 41))
	if err != nil || other.DecisionID == first.DecisionID {
		t.Errorf("another input returned %+v, %v; want a new decision", other, err)
	}
	r := recRequest(t, 1)
	r.DispositionDecisionID = second.DecisionID
	act, err := w.a.Record(recCtx(t), r)
	if err != nil || len(act.GrantIDs) != 1 {
		t.Fatalf("the action decision naming the replayed disposition: %+v, %v", act, err)
	}
}
