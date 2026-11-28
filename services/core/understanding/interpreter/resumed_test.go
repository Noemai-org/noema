package interpreter_test

// INP-01, slice 4 (REQ-INP-004; EC-E16, AGENT-13): the follow-up's INVESTIGATION_RESUMED is recorded when the follow-up has passed the gate and BEFORE it is saved, names the investigation by the prior's
// intent id and version only, and a Record that cannot take it stops the save. A first turn, and an Interpreter with no Resumes port, record no resume. No database here: spies. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

type resumeSpy struct {
	c       *calls
	got     []string
	version []int
	err     error
}

func (s *resumeSpy) Resumed(_ context.Context, sub interpreter.Subject, id string, version int) error {
	s.c.order = append(s.c.order, "resumed")
	s.got, s.version = append(s.got, id+"|"+sub.TenantID+"|"+sub.Actor+"|"+sub.CorrelationID), append(s.version, version)
	return s.err
}

func resumeWorld(t *testing.T, out []byte, err error) (*world, *resumeSpy) {
	t.Helper()
	w := newWorld(t, out)
	spy := &resumeSpy{c: w.calls, err: err}
	var e error
	w.in, e = interpreter.New(interpreter.Config{Compiler: w.comp, Gate: mustGate(t), Recorder: w.rec, Saver: w.save, Resumed: spy})
	if e != nil {
		t.Fatal(e)
	}
	return w, spy
}

func TestAFollowUpRecordsItsResumeAfterTheGateAndBeforeTheSave(t *testing.T) {
	prior := withPrior(t, 2)
	w, spy := resumeWorld(t, proposal(nil), nil)
	in := input()
	in.Prior = prior
	if _, err := w.in.Interpret(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if got := w.calls.order; len(got) != 3 || got[0] != "compile" || got[1] != "resumed" || got[2] != "save" {
		t.Errorf("calls %v, want compile, resumed, save", got)
	}
	if len(spy.got) != 1 || spy.got[0] != prior.IntentID+"|"+session.TenantID+"|"+actor+"|"+corr || spy.version[0] != 2 {
		t.Errorf("recorded %v v%v; want the prior %s v2 for the session's tenant, actor and chain", spy.got, spy.version, prior.IntentID)
	}
}

func TestAResumeThatCannotBeRecordedStopsTheSaveAndIsReportedAsARecordFailure(t *testing.T) {
	w, _ := resumeWorld(t, proposal(nil), errors.New("synthetic: the Record refused"))
	in := input()
	in.Prior = withPrior(t, 1)
	if _, err := w.in.Interpret(context.Background(), in); !errors.Is(err, interpreter.ErrRecord) {
		t.Errorf("err %v, want ErrRecord", err)
	}
	if len(w.save.saved) != 0 {
		t.Errorf("saved %d, want none: the Record comes first", len(w.save.saved))
	}
}

func TestAFirstTurnRecordsNoResume(t *testing.T) {
	w, spy := resumeWorld(t, proposal(nil), nil)
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if len(spy.got) != 0 {
		t.Errorf("a first turn recorded %v", spy.got)
	}
}

func TestAFollowUpRefusedAtTheGateRecordsNoResume(t *testing.T) {
	w, spy := resumeWorld(t, proposal(func(m map[string]any) { m["intent_node"] = "fin.not.a.node" }), nil)
	in := input()
	in.Prior = withPrior(t, 1)
	var rej *understanding.Rejection
	if _, err := w.in.Interpret(context.Background(), in); !errors.As(err, &rej) {
		t.Fatalf("err %v, want a rejection", err)
	}
	if len(spy.got) != 0 || len(w.save.saved) != 0 {
		t.Errorf("a refused follow-up recorded %v and saved %d", spy.got, len(w.save.saved))
	}
}

func TestWithNoResumesPortAFollowUpIsSavedAsBefore(t *testing.T) {
	w := newWorld(t, proposal(nil))
	in := input()
	in.Prior = withPrior(t, 1)
	if _, err := w.in.Interpret(context.Background(), in); err != nil || len(w.save.saved) != 1 {
		t.Errorf("err %v, saved %d", err, len(w.save.saved))
	}
}
