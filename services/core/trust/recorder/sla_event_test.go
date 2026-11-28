package recorder_test

// REQ-TRU-006, REQ-AUD-001 (PERD 23.5, 32.2; A632, A640, interim, relayed, not yet committed): the Trust Writer appends one SLA_EVALUATED event per clock evaluation, in the caller's
// transaction, written by the emitter's own identity, with every field of the evaluation as its payload. If the append fails nothing remains (the Record comes first, PERD 32.4).
// All values are SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const slaCorrelation = "corr_sla01"

func slaEvaluation() clocks.Evaluation {
	started, deadline, remaining := int64(1_000), int64(1_000+259_200), int64(100)
	return clocks.Evaluation{
		Status:     clocks.Status{ClockID: "syn.clock.test_72h", Label: contracts.ClockV1LabelSYNTHETIC, StartedAt: &started, DeadlineAt: &deadline, Elapsed: 259_100, Remaining: &remaining, Phase: clocks.AtRisk},
		Priority:   "HIGH",
		OwnerQueue: "SYNTHETIC_QUEUE",
		Raised:     true,
	}
}

func (w *world) appendSLA(t *testing.T, r recorder.SLARequest, commit bool) error {
	t.Helper()
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if aerr := w.w.AppendSLAEvaluated(ctx(t), tx, r); aerr != nil || !commit {
		_ = tx.Rollback(ctx(t))
		return aerr
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return nil
}

func TestAClockEvaluationAppendsOneSLAEvaluatedEventWithEveryField(t *testing.T) { // REQ-TRU-006 "SLA_EVALUATED (all fields)"
	w := newWorld(t)
	if err := w.appendSLA(t, recorder.SLARequest{TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: slaEvaluation()}, true); err != nil {
		t.Fatal(err)
	}
	rows, err := chain.ReadChain(ctx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].EventType != "SLA_EVALUATED" || rows[0].ObjectRef != slaCorrelation {
		t.Fatalf("got %d events, first %+v; want exactly one SLA_EVALUATED naming the correlation", len(rows), rows)
	}
	if rows[0].WrittenBy != "core-trust" || rows[0].Actor != "core-trust" {
		t.Errorf("written_by %q actor %q; the emitter's own identity is both (INV-12)", rows[0].WrittenBy, rows[0].Actor)
	}
	var got map[string]any
	if rows[0].Payload == nil || json.Unmarshal([]byte(*rows[0].Payload), &got) != nil {
		t.Fatal("the event carries no readable payload")
	}
	for k, v := range slaEvaluation().EventPayload() {
		b, _ := json.Marshal(v)
		g, _ := json.Marshal(got[k])
		if string(b) != string(g) {
			t.Errorf("payload %s: got %s, want %s", k, g, b)
		}
	}
	if len(got) != len(slaEvaluation().EventPayload()) {
		t.Errorf("payload has %d members, want %d: %v", len(got), len(slaEvaluation().EventPayload()), got)
	}
}

func TestARolledBackTransactionLeavesNoSLAEvaluatedEvent(t *testing.T) { // REQ-AUD-001
	w := newWorld(t)
	if err := w.appendSLA(t, recorder.SLARequest{TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: slaEvaluation()}, false); err != nil {
		t.Fatal(err)
	}
	if got := w.events(t); len(got) != 0 {
		t.Fatalf("events remain after the rollback: %v", got)
	}
}

func TestAFailedSLAEvaluatedAppendIsReturnedAndLeavesNothing(t *testing.T) { // PERD 32.4, EC-E16: the Record comes first
	w := newWorld(t)
	clk := clock.NewManual(epoch)
	base := ids.New(clk, rand.New(rand.NewSource(3)))
	w.w.Appender = newAppender(t, w.admin, clk, &failingIDs{inner: base, prefix: "recev", nth: 1})
	err := w.appendSLA(t, recorder.SLARequest{TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: slaEvaluation()}, true)
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "append SLA_EVALUATED") {
		t.Fatalf("got %v, want the SLA_EVALUATED append failure", err)
	}
	if got := w.events(t); len(got) != 0 {
		t.Fatalf("events remain: %v", got)
	}
}

func TestARequestThatIsNotAnEvaluationIsRefusedBeforeAnythingIsWritten(t *testing.T) { // INV-16
	good := slaEvaluation()
	noPhase := slaEvaluation()
	noPhase.Status.Phase = ""
	noPriority := slaEvaluation()
	noPriority.Priority = ""
	noClock := slaEvaluation()
	noClock.Status.ClockID = ""
	// A694 (SR on noema#977): the Recorder re-checks what the evaluator promised; a caller that builds an Evaluation by hand gets no further than the refusal.
	mod := func(f func(e *clocks.Evaluation)) clocks.Evaluation {
		e := slaEvaluation()
		f(&e)
		return e
	}
	neg := int64(-1)
	for name, r := range map[string]recorder.SLARequest{
		"no tenant":                     {CorrelationID: slaCorrelation, Evaluation: good},
		"bad tenant":                    {TenantID: "tenant-a", CorrelationID: slaCorrelation, Evaluation: good},
		"no correlation":                {TenantID: tenant, Evaluation: good},
		"bad correlation":               {TenantID: tenant, CorrelationID: "case 1", Evaluation: good},
		"no phase":                      {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: noPhase},
		"no priority":                   {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: noPriority},
		"no clock":                      {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: noClock},
		"zero value":                    {},
		"negative elapsed":              {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) { e.Status.Elapsed = -1 })},
		"negative remaining":            {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) { e.Status.Remaining = &neg })},
		"raised to the lowest priority": {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) { e.Priority = clocks.PriorityLow })},
		"not started names a start": {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) {
			e.Status.Phase = clocks.NotStarted
			e.Status.Elapsed = 0
			e.Status.Remaining = nil
			e.Status.DeadlineAt = nil
		})},
		"not started names a remaining": {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) {
			e.Status.Phase = clocks.NotStarted
			e.Status.Elapsed = 0
			e.Status.StartedAt = nil
			e.Status.DeadlineAt = nil
		})},
		"running without a start":     {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) { e.Status.StartedAt = nil })},
		"running without a deadline":  {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) { e.Status.DeadlineAt = nil })},
		"running without a remaining": {TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: mod(func(e *clocks.Evaluation) { e.Status.Remaining = nil })},
	} {
		w := newWorld(t)
		err := w.appendSLA(t, r, true)
		if !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%s: got %v, want ErrRequest", name, err)
		}
		if got := w.events(t); len(got) != 0 {
			t.Errorf("%s: events remain: %v", name, got)
		}
	}
}

func TestAnEvaluationForAnotherTenantIsRefusedByTheDatabaseAndLeavesNothing(t *testing.T) { // REQ-SEC-004
	w := newWorld(t)
	if err := w.appendSLA(t, recorder.SLARequest{TenantID: "ten_bb", CorrelationID: slaCorrelation, Evaluation: slaEvaluation()}, true); err == nil {
		t.Fatal("an event naming another tenant than the login's was appended")
	}
	if got := w.events(t); len(got) != 0 {
		t.Fatalf("events remain: %v", got)
	}
}

func TestAnOwnerQueueOutsideThePacksShapeIsRefusedBeforeAnythingIsWritten(t *testing.T) { // A694, INV-16: the shape of #984's owner_queue and of the pack's queue
	for name, q := range map[string]string{"lower case": "synthetic_queue", "leading digit": "1QUEUE", "space": "BAD QUEUE", "newline": "QUEUE\n", "too long": "QABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZ"} {
		e := slaEvaluation()
		e.OwnerQueue = q
		w := newWorld(t)
		if err := w.appendSLA(t, recorder.SLARequest{TenantID: tenant, CorrelationID: slaCorrelation, Evaluation: e}, true); !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%s: got %v, want ErrRequest", name, err)
		}
		if got := w.events(t); len(got) != 0 {
			t.Errorf("%s: events remain: %v", name, got)
		}
	}
}
