package trustport

// RES-02 (REQ-RES-002, INV-20; A642, A677, interim, relayed): the freeze row on the real Record. CONTRADICTION_FREEZE_SET is appended by Trust's own identity and read back as the
// latest state of the target; CONTRADICTION_FREEZE_CLEARED needs a Trust decision of the tenant that is a resolution (an ALLOW) or an escalation (an ESCALATE), and a freeze that is recorded.
// A reader that cannot answer is FreezeUnknown, never "no freeze". All data is SYNTHETIC.

import (
	"context"
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
	app := &chain.Appender{Clock: clk, IDs: ids.New(clk, nil), Signers: recordsig.For(t, w.admin, "freezerow", nil, "core-trust")}
	row := NewFreezeRow(FreezeRowConfig{Begin: recPool{w.trust}, Reads: func(string) chain.Queryer { return w.reader }, Appender: app})
	return &frzWorld{recWorld: w, row: row}
}

func (w *frzWorld) state(t *testing.T) reconcile.RecordedFreeze {
	t.Helper()
	return reconcile.ReadFreeze(recCtx(t), w.row, recTenant, frzTarget)
}

func (w *frzWorld) set(t *testing.T) {
	t.Helper()
	fs := reconcile.FreezeSet{TenantID: recTenant, Target: frzTarget, RulesetID: "fin.recon.rules@2", RuleID: "R4@2", Evidence: []string{"ev_A1", "ev_B2"}, Labels: []string{"switch_failed_credit_credited"}}
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
