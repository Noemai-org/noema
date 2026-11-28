package resumestate_test

// INP-01, slice 6 (REQ-INP-004; A320, A404; INV-12): an investigation has ended when the state its row holds has no exit in the orchestrator's transition table. The chain's correlation id `corr_<ULID>`
// names the investigation `inv_<ULID>` (A234). The row is read as the verified session's tenant; an unknown state, a row of another principal and a store that cannot answer are read failures, never "open".
// No database: a fake reader over the real transition table. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/resumestate"
)

const (
	tenant = "ten_demo"
	prin   = "usr_88213"
	corr   = "corr_01J9X3A1B2C3D4E5F6G7H8J9K0"
	inv    = "inv_01J9X3A1B2C3D4E5F6G7H8J9K0"
)

var errBoom = errors.New("synthetic failure")

type readerFake struct {
	snap  investigations.Snapshot
	err   error
	sess  orchestrator.Session
	id    string
	calls int
}

func (r *readerFake) Read(_ context.Context, sess orchestrator.Session, id string) (investigations.Snapshot, error) {
	r.calls++
	r.sess, r.id = sess, id
	return r.snap, r.err
}

func table(t *testing.T) *orchestrator.Table {
	t.Helper()
	tb, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func build(t *testing.T, r *readerFake) *resumestate.States {
	t.Helper()
	s, err := resumestate.New(r, table(t), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAStateWithNoExitIsTerminalAndEveryOtherStateIsNot(t *testing.T) {
	tb := table(t)
	ended := 0
	for _, st := range tb.States() {
		r := &readerFake{snap: investigations.Snapshot{State: st, Principal: prin}}
		got, err := build(t, r).Terminal(context.Background(), tenant, prin, corr)
		if err != nil {
			t.Fatalf("%s: %v", st, err)
		}
		if want := len(tb.Exits(st)) == 0; got != want {
			t.Errorf("%s: terminal %v, want %v", st, got, want)
		}
		if got {
			ended++
		}
	}
	if ended == 0 || ended == len(tb.States()) {
		t.Errorf("%d of %d states are terminal: the table's own exits decide, and a table with none or all would make the answer meaningless", ended, len(tb.States()))
	}
}

func TestTheRowIsReadAsTheSessionsTenantAndPrincipalForTheInvestigationOfTheChain(t *testing.T) {
	r := &readerFake{snap: investigations.Snapshot{State: orchestrator.State("EVALUATING"), Principal: prin}}
	if _, err := build(t, r).Terminal(context.Background(), tenant, prin, corr); err != nil {
		t.Fatal(err)
	}
	if r.calls != 1 || r.sess.TenantID != tenant || r.sess.PrincipalID != prin || r.id != inv {
		t.Errorf("read %d times as %+v for %q; want once as the session's tenant and principal for %q", r.calls, r.sess, r.id, inv)
	}
}

func TestAnUnknownStateARowOfAnotherPrincipalAndAFailingStoreAreReadFailures(t *testing.T) {
	for name, r := range map[string]*readerFake{
		"unknown state":     {snap: investigations.Snapshot{State: orchestrator.State("NOT_A_STATE"), Principal: prin}},
		"another principal": {snap: investigations.Snapshot{State: orchestrator.State("EVALUATING"), Principal: "usr_99999"}},
		"no row":            {err: investigations.ErrNoInvestigation},
		"a failing store":   {err: errBoom},
		"an empty state":    {snap: investigations.Snapshot{Principal: prin}},
	} {
		got, err := build(t, r).Terminal(context.Background(), tenant, prin, corr)
		if !errors.Is(err, resumestate.ErrRead) || got {
			t.Errorf("%s: got %v, %v; want a read failure and not terminal", name, got, err)
		}
	}
	if _, err := build(t, &readerFake{err: errBoom}).Terminal(context.Background(), tenant, prin, corr); !errors.Is(err, errBoom) {
		t.Error("the cause is not reachable")
	}
}

func TestARowThatHoldsNoPrincipalIsReadByTheSessionsTenantAlone(t *testing.T) {
	r := &readerFake{snap: investigations.Snapshot{State: orchestrator.State("EVALUATING")}} // a row from before migration 0029 holds an empty principal
	if _, err := build(t, r).Terminal(context.Background(), tenant, prin, corr); err != nil {
		t.Errorf("an old row was refused: %v", err)
	}
}

func TestAMalformedChainOrAnIncompleteSessionReadsNothing(t *testing.T) {
	for name, c := range map[string]struct{ tenant, principal, corr string }{
		"not a chain":  {tenant, prin, "inv_01J9X3A1B2C3D4E5F6G7H8J9K0"},
		"short":        {tenant, prin, "corr_x"},
		"no tenant":    {"", prin, corr},
		"no principal": {tenant, "", corr},
	} {
		r := &readerFake{snap: investigations.Snapshot{State: orchestrator.State("EVALUATING"), Principal: prin}}
		if got, err := build(t, r).Terminal(context.Background(), c.tenant, c.principal, c.corr); !errors.Is(err, resumestate.ErrRead) || got || r.calls != 0 {
			t.Errorf("%s: got %v, %v, reads %d; want a read failure and no read", name, got, err, r.calls)
		}
	}
}

func TestAnIncompleteSetupBuildsNothing(t *testing.T) {
	r := &readerFake{}
	for name, f := range map[string]func() (*resumestate.States, error){
		"no reader":  func() (*resumestate.States, error) { return resumestate.New(nil, table(t), time.Second) },
		"no table":   func() (*resumestate.States, error) { return resumestate.New(r, nil, time.Second) },
		"no timeout": func() (*resumestate.States, error) { return resumestate.New(r, table(t), 0) },
	} {
		if s, err := f(); err == nil || s != nil {
			t.Errorf("%s: built %v", name, s)
		}
	}
}
