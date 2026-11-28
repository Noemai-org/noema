package trustport

// RES-02 (REQ-RES-002, INV-20; A642, A677, interim, relayed): the freeze row on the real Record. CONTRADICTION_FREEZE_SET is appended by Trust's own identity and read back as the
// latest state of the target; CONTRADICTION_FREEZE_CLEARED needs a Trust decision of the tenant that is a resolution (an ALLOW) or an escalation (an ESCALATE), and a freeze that is recorded.
// A reader that cannot answer is FreezeUnknown, never "no freeze". All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const frzTarget = "tx_FrzTarget01"

type frzWorld struct {
	*recWorld
	row *FreezeRow
}

func newFrzWorld(t *testing.T) *frzWorld {
	t.Helper()
	w := newRecWorldWith(t, recorder.NewEscalationSet(recCap))
	clk := clock.NewManual(time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC))
	app := &chain.Appender{Clock: clk, IDs: ids.New(clk, nil), Signers: recordsig.For(t, w.admin, "recorder", nil, "core-trust")}
	row := NewFreezeRow(FreezeRowConfig{Begin: recPool{w.trust}, Reads: func(string) chain.Queryer { return w.reader }, Appender: app})
	return &frzWorld{recWorld: w, row: row}
}

func (w *frzWorld) state(t *testing.T) reconcile.RecordedFreeze {
	t.Helper()
	return reconcile.ReadFreeze(recCtx(t), w.row, recTenant, frzTarget)
}

// frzCorr is the correlation of the decisions the recorder world writes (recorder_test.go): the freeze row's SET names the same investigation (SR on #1035).
const frzCorr = "corr_ABC123"

func (w *frzWorld) set(t *testing.T) { w.setIn(t, frzCorr) }

func (w *frzWorld) setIn(t *testing.T, corr string) {
	t.Helper()
	fs := reconcile.FreezeSet{TenantID: recTenant, CorrelationID: corr, Target: frzTarget, RulesetID: "fin.recon.rules@2", RuleID: "R4@2", Evidence: []string{"ev_A1", "ev_B2"}, Labels: []string{"switch_failed_credit_credited"}}
	if err := w.row.RecordFreezeSet(recCtx(t), fs); err != nil {
		t.Fatal(err)
	}
}

func (w *frzWorld) events(t *testing.T) []chain.Row {
	t.Helper()
	rows, err := chain.ReadChain(recCtx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	var out []chain.Row
	for _, r := range rows {
		if r.EventType == "CONTRADICTION_FREEZE_SET" || r.EventType == "CONTRADICTION_FREEZE_CLEARED" {
			out = append(out, r)
		}
	}
	return out
}

func TestATargetWithNoFreezeRowReadsAsNone(t *testing.T) {
	w := newFrzWorld(t)
	if got := w.state(t); got != reconcile.FreezeNone {
		t.Fatalf("got %v, want FreezeNone", got)
	}
}

func TestAFreezeSetIsRecordedByTrustAndReadBack(t *testing.T) {
	w := newFrzWorld(t)
	w.set(t)
	if got := w.state(t); got != reconcile.FreezeRecorded {
		t.Fatalf("got %v, want FreezeRecorded", got)
	}
	ev := w.events(t)
	if len(ev) != 1 || ev[0].EventType != "CONTRADICTION_FREEZE_SET" || ev[0].WrittenBy != "core-trust" || ev[0].ObjectRef != frzTarget {
		t.Fatalf("Record events %+v, want one CONTRADICTION_FREEZE_SET by core-trust about %s", ev, frzTarget)
	}
}

func TestAFreezeIsClearedOnlyByARecordedEscalationOrResolution(t *testing.T) {
	w := newFrzWorld(t)
	w.set(t)
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 50))
	if err != nil || esc.DecisionID == "" {
		t.Fatalf("the escalation: %+v %v", esc, err)
	}
	for name, lift := range map[string]reconcile.FreezeLift{
		"a decision that does not exist":      {TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: "dec_01NOSUCHDECISION"},
		"an escalation named as a resolution": {TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByResolution, DecisionID: esc.DecisionID},
	} {
		if err := w.row.RecordFreezeCleared(recCtx(t), lift); err == nil {
			t.Errorf("%s: the freeze was cleared", name)
		}
		if got := w.state(t); got != reconcile.FreezeRecorded {
			t.Errorf("%s: state %v, want FreezeRecorded", name, got)
		}
	}
	if err := w.row.RecordFreezeCleared(recCtx(t), reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}); err != nil {
		t.Fatal(err)
	}
	if got := w.state(t); got != reconcile.FreezeCleared {
		t.Fatalf("got %v, want FreezeCleared", got)
	}
	if ev := w.events(t); len(ev) != 2 || ev[1].EventType != "CONTRADICTION_FREEZE_CLEARED" || ev[1].ObjectRef != frzTarget {
		t.Fatalf("Record events %+v, want the SET then the CLEARED", ev)
	}
}

func TestThereIsNothingToClearWhereNoFreezeIsRecorded(t *testing.T) {
	w := newFrzWorld(t)
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 51))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.row.RecordFreezeCleared(recCtx(t), reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}); err == nil {
		t.Fatal("a freeze that was never recorded was cleared")
	}
	if ev := w.events(t); len(ev) != 0 {
		t.Fatalf("Record events %+v, want none", ev)
	}
}

func TestANewContradictionAfterAClearingFreezesAgain(t *testing.T) {
	w := newFrzWorld(t)
	w.set(t)
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 52))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.row.RecordFreezeCleared(recCtx(t), reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}); err != nil {
		t.Fatal(err)
	}
	w.set(t)
	if got := w.state(t); got != reconcile.FreezeRecorded {
		t.Fatalf("got %v, want FreezeRecorded (the latest row decides)", got)
	}
}

type brokenQueryer struct{}

func (brokenQueryer) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("synthetic: the Record is unreachable")
}

func TestAReaderThatCannotAnswerIsUnknownNeverNone(t *testing.T) {
	w := newFrzWorld(t)
	for name, reads := range map[string]func(string) chain.Queryer{
		"the Record is unreachable": func(string) chain.Queryer { return brokenQueryer{} },
		"no reader for the tenant":  func(string) chain.Queryer { return nil },
	} {
		row := NewFreezeRow(FreezeRowConfig{Begin: recPool{w.trust}, Reads: reads, Appender: w.row.c.Appender})
		if got := reconcile.ReadFreeze(recCtx(t), row, recTenant, frzTarget); got != reconcile.FreezeUnknown {
			t.Errorf("%s: got %v, want FreezeUnknown", name, got)
		}
	}
}

// A756 (interim, relayed): the target reference is in the payload of both freeze events and is verified against the freeze row. A row whose payload names another target than its object
// reference is a defect: the reader answers unknown (frozen), and a clearing is refused where the recorded SET does not name the target.
func (w *frzWorld) appendRaw(t *testing.T, eventType, objectRef string, payload map[string]any) {
	t.Helper()
	tx, err := recPool{w.trust}.Begin(recCtx(t), recTenant)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(recCtx(t)) }()
	if _, err := w.row.c.Appender.Append(recCtx(t), tx, chain.Event{TenantID: recTenant, CorrelationID: frzCorr, WrittenBy: "core-trust", Actor: "core-trust", EventType: eventType, ObjectRef: objectRef, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(recCtx(t)); err != nil {
		t.Fatal(err)
	}
}

func payloadOf(t *testing.T, r chain.Row) map[string]any {
	t.Helper()
	var m map[string]any
	if r.Payload == nil {
		t.Fatalf("%s has no payload", r.EventType)
	}
	if err := json.Unmarshal([]byte(*r.Payload), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBothFreezeEventsCarryTheTargetReference(t *testing.T) {
	w := newFrzWorld(t)
	w.set(t)
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 53))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.row.RecordFreezeCleared(recCtx(t), reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}); err != nil {
		t.Fatal(err)
	}
	ev := w.events(t)
	if len(ev) != 2 {
		t.Fatalf("events %+v", ev)
	}
	for _, e := range ev {
		if got := payloadOf(t, e)["target_ref"]; got != frzTarget {
			t.Errorf("%s payload target_ref %v, want %s", e.EventType, got, frzTarget)
		}
	}
}

func TestARowThatNamesAnotherTargetInItsPayloadReadsAsUnknown(t *testing.T) {
	for _, kind := range []string{eventSet, eventCleared} {
		w := newFrzWorld(t)
		w.appendRaw(t, kind, frzTarget, map[string]any{"target_ref": "tx_OtherTarget9", "ruleset_id": "fin.recon.rules@2", "rule_id": "R4@2", "evidence": []string{"ev_A1"}, "labels": []string{"l"}, "cleared_by": "ESCALATION", "decision_id": "dec_01X"})
		if got := w.state(t); got != reconcile.FreezeUnknown {
			t.Errorf("%s: got %v, want FreezeUnknown", kind, got)
		}
	}
}

func TestAClearingIsRefusedWhereTheRecordedFreezeNamesAnotherTarget(t *testing.T) {
	w := newFrzWorld(t)
	w.appendRaw(t, eventSet, frzTarget, map[string]any{"target_ref": "tx_OtherTarget9", "ruleset_id": "fin.recon.rules@2", "rule_id": "R4@2", "evidence": []string{"ev_A1"}, "labels": []string{"l"}})
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 54))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.row.RecordFreezeCleared(recCtx(t), reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}); err == nil {
		t.Fatal("a freeze row that names another target was cleared")
	}
}

// SR on #1035 (finding 1; A756): a freeze is lifted by the decision that resolves THIS investigation, not by any decision of the tenant with the right outcome. The clearing decision names the
// same correlation as the recorded SET and is recorded after it. (The Decision Record has no target member; a target member is a contracts PR for both Founders and is listed.)
func TestADecisionOfAnotherInvestigationCannotClearTheFreeze(t *testing.T) {
	w := newFrzWorld(t)
	w.setIn(t, "corr_Other7")
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 60))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.row.RecordFreezeCleared(recCtx(t), reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}); err == nil {
		t.Fatal("a decision of another investigation cleared the freeze")
	}
	if got := w.state(t); got != reconcile.FreezeRecorded {
		t.Fatalf("state %v, want FreezeRecorded", got)
	}
}

func TestADecisionRecordedBeforeTheFreezeCannotClearIt(t *testing.T) {
	w := newFrzWorld(t)
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 61))
	if err != nil {
		t.Fatal(err)
	}
	w.set(t)
	if err := w.row.RecordFreezeCleared(recCtx(t), reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}); err == nil {
		t.Fatal("a decision recorded before the freeze cleared it")
	}
}

// Finding 2 and condition 4: a SET that cannot be appended is an error the caller must treat as a refusal (no grant, no write, retry), and a SET with no correlation writes nothing.
func TestAFreezeSetThatCannotBeAppendedIsAnErrorAndWritesNothing(t *testing.T) {
	w := newFrzWorld(t)
	fs := reconcile.FreezeSet{TenantID: recTenant, CorrelationID: "", Target: frzTarget, RulesetID: "fin.recon.rules@2", RuleID: "R4@2", Evidence: []string{"ev_A1"}, Labels: []string{"l"}}
	if err := w.row.RecordFreezeSet(recCtx(t), fs); err == nil {
		t.Fatal("a SET with no correlation was written")
	}
	broken := NewFreezeRow(FreezeRowConfig{Begin: recPool{w.trust}, Reads: func(string) chain.Queryer { return w.reader }, Appender: &chain.Appender{Clock: clock.NewManual(time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)), IDs: ids.New(clock.NewManual(time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)), nil)}}) // no signer for core-trust: the append fails
	fs.CorrelationID = frzCorr
	if err := broken.RecordFreezeSet(recCtx(t), fs); err == nil {
		t.Fatal("a SET whose append failed was reported as written")
	}
	if ev := w.events(t); len(ev) != 0 {
		t.Fatalf("Record events %+v, want none", ev)
	}
}

// Condition 3: two clearings at once write one CLEARED, never two.
func TestTwoClearingsAtOnceWriteOneClearedEvent(t *testing.T) {
	w := newFrzWorld(t)
	w.set(t)
	esc, err := w.a.Record(recCtx(t), escalateDisposition(t, 62))
	if err != nil {
		t.Fatal(err)
	}
	lift := reconcile.FreezeLift{TenantID: recTenant, Target: frzTarget, By: reconcile.ClearedByEscalation, DecisionID: esc.DecisionID}
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { errs <- w.row.RecordFreezeCleared(recCtx(t), lift) }()
	}
	ok := 0
	for i := 0; i < 4; i++ {
		if <-errs == nil {
			ok++
		}
	}
	cleared := 0
	for _, e := range w.events(t) {
		if e.EventType == "CONTRADICTION_FREEZE_CLEARED" {
			cleared++
		}
	}
	if cleared != 1 || ok != 1 {
		t.Fatalf("%d CLEARED events and %d successes from four clearings, want exactly one of each", cleared, ok)
	}
}
