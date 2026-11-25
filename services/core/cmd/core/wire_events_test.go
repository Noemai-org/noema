package main

// EDG-02 (REQ-API-001; INV-12, INV-21; AGENT-13): the events feed reads the Record directly, as the tenant, and emits only public stages as sse-event.v1 progress payloads. The
// cursor is the ordinal among the investigation's own events; another principal's, another tenant's and an absent conversation are one answer. Every tenant, principal, ID and
// instant here is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

const (
	evInv   = "inv_AA11"
	evCorr  = "corr_AA11"
	evTen   = "ten_aa"
	evOwner = "usr_1"
)

func evInput(corr, actor string) chain.Row {
	return chain.Row{EventType: "INPUT_RECEIVED", Actor: actor, CorrelationID: corr, ObjectRef: "recev_X1", Body: `{"at":"2026-11-12T09:00:00Z"}`}
}

func evChange(corr, inv, to string, sec int) chain.Row {
	at := time.Date(2026, 11, 12, 9, 0, sec, 0, time.UTC).Format(time.RFC3339)
	p := `{"investigation_id":"` + inv + `","from":"X","to":"` + to + `","from_version":1,"to_version":2}`
	return chain.Row{EventType: "INVESTIGATION_STATE_CHANGED", Actor: "core-workflows", CorrelationID: corr, ObjectRef: inv, Body: `{"at":"` + at + `"}`, Payload: &p}
}

type readCall struct{ tenant string }

func newFeed(t *testing.T, byTenant map[string][]chain.Row, maxBatch int, calls *[]readCall) *recordFeed {
	t.Helper()
	table, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	read := func(ctx context.Context, tenant string) ([]chain.Row, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("the Record read has no deadline")
		}
		if calls != nil {
			*calls = append(*calls, readCall{tenant})
		}
		rows, ok := byTenant[tenant]
		if !ok {
			return nil, errors.New("pq: password authentication failed for user secret_user")
		}
		return rows, nil
	}
	return &recordFeed{read: read, table: table, maxBatch: maxBatch, readTimeout: time.Second}
}

func evCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func princ(tenant, id string) session.Principal {
	return session.Principal{Tenant: tenant, ID: id, Role: session.RoleCustomer}
}

func ownRows() []chain.Row {
	return []chain.Row{
		evInput(evCorr, "customer:"+evOwner),
		evChange(evCorr, evInv, "INTERPRETING", 1),
		evChange(evCorr, evInv, "EVALUATING", 2),
		evChange(evCorr, evInv, "EXECUTING", 3),
		evChange(evCorr, evInv, "EXPLAINING", 4),
		evChange(evCorr, evInv, "COMPLETED", 5),
	}
}

func stagesOf(b events.Batch) []string {
	var out []string
	for _, e := range b.Events {
		out = append(out, string(e.Progress.State))
	}
	return out
}

// REQ-API-001: internal states become public stages, two states of one stage are one event, cursors are ordinals, and a terminal state ends the stream.
func TestTheFeedEmitsPublicStagesInChainOrderWithOrdinalCursors(t *testing.T) {
	f := newFeed(t, map[string][]chain.Row{evTen: ownRows()}, 10, nil)
	b, err := f.Since(evCtx(t), princ(evTen, evOwner), evInv, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stagesOf(b), ","); got != "understanding,working,answered" || !b.Done {
		t.Fatalf("stages %s, done %v; want understanding,working,answered and done", got, b.Done)
	}
	for i, e := range b.Events {
		if e.Cursor != int64(i+1) || e.Clarification != nil || e.Answer != nil || e.Confirmation != nil || e.Error != nil {
			t.Errorf("event %d: %+v", i, e)
		}
	}
	if string(b.Events[0].Progress.At) != "2026-11-12T09:00:01Z" || string(b.Events[2].Progress.At) != "2026-11-12T09:00:05Z" {
		t.Errorf("instants %v, %v", b.Events[0].Progress.At, b.Events[2].Progress.At)
	}
}

// Resume: events after the cursor only; the cursor at the last event is an empty batch; a cursor past the last event is the not-found answer (no oracle).
func TestResumeAfterACursorAndACursorPastTheEndIsNotFound(t *testing.T) {
	f := newFeed(t, map[string][]chain.Row{evTen: ownRows()}, 10, nil)
	b, err := f.Since(evCtx(t), princ(evTen, evOwner), evInv, 1)
	if err != nil || len(b.Events) != 2 || b.Events[0].Cursor != 2 || !b.Done {
		t.Fatalf("%+v, %v", b, err)
	}
	b, err = f.Since(evCtx(t), princ(evTen, evOwner), evInv, 3)
	if err != nil || len(b.Events) != 0 || !b.Done {
		t.Fatalf("at the end: %+v, %v", b, err)
	}
	if _, err = f.Since(evCtx(t), princ(evTen, evOwner), evInv, 4); !errors.Is(err, events.ErrNotFound) {
		t.Fatalf("past the end: %v", err)
	}
	if _, err = f.Since(evCtx(t), princ(evTen, evOwner), evInv, -1); !errors.Is(err, events.ErrNotFound) {
		t.Fatalf("negative: %v", err)
	}
}

// INV-12, REQ-API-002: another principal's conversation, an absent one, one with no recorded input and a malformed reference are the same answer; so is another tenant's.
func TestAnotherPrincipalsOrAnAbsentConversationIsTheSameNotFound(t *testing.T) {
	other := []chain.Row{evInput("corr_BB22", "customer:usr_2"), evChange("corr_BB22", "inv_BB22", "INTERPRETING", 1)}
	rows := append(ownRows(), other...)
	noInput := []chain.Row{evChange("corr_CC33", "inv_CC33", "INTERPRETING", 1)}
	mixed := []chain.Row{evInput("corr_DD44", "customer:usr_2"), evInput("corr_DD44", "customer:"+evOwner), evChange("corr_DD44", "inv_DD44", "INTERPRETING", 1)}
	staff := []chain.Row{evInput("corr_EE55", "operator:"+evOwner), evChange("corr_EE55", "inv_EE55", "INTERPRETING", 1)}
	rows = append(append(append(rows, noInput...), mixed...), staff...)
	f := newFeed(t, map[string][]chain.Row{evTen: rows, "ten_bb": ownRows()}, 10, nil)
	for name, c := range map[string]struct {
		p   session.Principal
		ref string
	}{
		"another principal's":               {princ(evTen, evOwner), "inv_BB22"},
		"absent":                            {princ(evTen, evOwner), "inv_ZZ99"},
		"no recorded input":                 {princ(evTen, evOwner), "inv_CC33"},
		"a shared correlation":              {princ(evTen, evOwner), "inv_DD44"},
		"a staff actor of the same subject": {princ(evTen, evOwner), "inv_EE55"},
		"malformed":                         {princ(evTen, evOwner), "not-an-id"},
		"the owner of the other tenant's":   {princ("ten_bb", "usr_9"), evInv},
	} {
		if _, err := f.Since(evCtx(t), c.p, c.ref, 0); !errors.Is(err, events.ErrNotFound) {
			t.Errorf("%s: %v; want ErrNotFound", name, err)
		}
	}
}

// INV-12: the Record is read as the session's tenant, once per call, and a tenant's rows are never mixed with another's.
func TestTheRecordIsReadAsTheSessionsTenant(t *testing.T) {
	var calls []readCall
	f := newFeed(t, map[string][]chain.Row{evTen: ownRows(), "ten_bb": {evInput("corr_BB22", "customer:"+evOwner), evChange("corr_BB22", "inv_BB22", "EXECUTING", 1)}}, 10, &calls)
	b, err := f.Since(evCtx(t), princ("ten_bb", evOwner), "inv_BB22", 0)
	if err != nil || strings.Join(stagesOf(b), ",") != "working" || b.Done || len(calls) != 1 || calls[0].tenant != "ten_bb" {
		t.Fatalf("%+v, %v, calls %+v", b, err, calls)
	}
}

// The batch cap: a longer answer is cut to the cap and is not done; a call with no deadline, an unconfigured feed and a failed read are errors that name no cause.
func TestTheBatchIsCappedAndFailuresNameNoCause(t *testing.T) {
	f := newFeed(t, map[string][]chain.Row{evTen: ownRows()}, 2, nil)
	b, err := f.Since(evCtx(t), princ(evTen, evOwner), evInv, 0)
	if err != nil || len(b.Events) != 2 || b.Done {
		t.Fatalf("%+v, %v", b, err)
	}
	if _, err = f.Since(t.Context(), princ(evTen, evOwner), evInv, 0); err == nil || errors.Is(err, events.ErrNotFound) {
		t.Errorf("no deadline: %v", err)
	}
	if _, err = (*recordFeed)(nil).Since(evCtx(t), princ(evTen, evOwner), evInv, 0); err == nil {
		t.Error("a nil feed answered")
	}
	_, err = f.Since(evCtx(t), princ("ten_zz", evOwner), evInv, 0)
	if err == nil || errors.Is(err, events.ErrNotFound) {
		t.Errorf("a failed read: %v", err)
	}
}

// A non-terminal last state is not done; a state with no public stage and an unreadable row are errors, never a guessed stage.
func TestNothingIsGuessed(t *testing.T) {
	f := newFeed(t, map[string][]chain.Row{evTen: ownRows()[:4]}, 10, nil)
	if b, err := f.Since(evCtx(t), princ(evTen, evOwner), evInv, 0); err != nil || b.Done {
		t.Fatalf("a running investigation: %+v, %v", b, err)
	}
	for name, rows := range map[string][]chain.Row{
		"an unknown state": {evInput(evCorr, "customer:"+evOwner), evChange(evCorr, evInv, "SOMETHING_NEW", 1)},
		"no payload":       {evInput(evCorr, "customer:"+evOwner), func() chain.Row { r := evChange(evCorr, evInv, "EXECUTING", 1); r.Payload = nil; return r }()},
		"no instant":       {evInput(evCorr, "customer:"+evOwner), func() chain.Row { r := evChange(evCorr, evInv, "EXECUTING", 1); r.Body = `{}`; return r }()},
	} {
		g := newFeed(t, map[string][]chain.Row{evTen: rows}, 10, nil)
		if _, err := g.Since(evCtx(t), princ(evTen, evOwner), evInv, 0); err == nil || errors.Is(err, events.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

type noDBs struct{}

func (noDBs) For(string) (reconstruct.TxBeginner, error) {
	return nil, errors.New("synthetic: no pool")
}

func eventsEnv(m envMap) Env { return Env{Getenv: m.get} }

func allEventsSettings() envMap {
	return envMap{EnvEventsPollMS: "2000", EnvEventsMaxStreamMS: "60000", EnvEventsWriteTimeoutMS: "5000", EnvEventsReadTimeoutMS: "2000", EnvEventsMaxEventBytes: "4096",
		EnvEventsMaxStreams: "8", EnvEventsMaxStreamsPerPrincipal: "2", EnvEventsMaxBatch: "32"}
}

// AGENT-16: with no Record reader there is no route; with one, every setting is required and none has a default.
func TestBuildEventsNeedsTheRecordReaderAndEverySetting(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC))
	if h, err := buildEvents(eventsEnv(envMap{}), clk, nil); h != nil || err != nil {
		t.Fatalf("no reader: %v, %v", h, err)
	}
	h, err := buildEvents(eventsEnv(allEventsSettings()), clk, noDBs{})
	if err != nil || h == nil {
		t.Fatalf("%v, %v", h, err)
	}
	for name := range allEventsSettings() {
		m := allEventsSettings()
		delete(m, name)
		if h, err := buildEvents(eventsEnv(m), clk, noDBs{}); h != nil || err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s missing: %v, %v", name, h, err)
		}
		m[name] = "0"
		if h, err := buildEvents(eventsEnv(m), clk, noDBs{}); h != nil || err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s zero: %v, %v", name, h, err)
		}
	}
}

// The production reader refuses a call with no deadline and a tenant with no pool, before any database is asked.
func TestTheChainReaderNeedsADeadlineAndTheTenantsOwnPool(t *testing.T) {
	read := readVerifiedChain(noDBs{})
	if _, err := read(t.Context(), evTen); err == nil {
		t.Error("no deadline accepted")
	}
	if _, err := read(evCtx(t), evTen); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("no pool: %v", err)
	}
}

// Only this investigation's own events in its own correlation count: another principal's input and another investigation's changes in the same Record do not change the answer.
func TestOtherConversationsInTheRecordDoNotChangeTheAnswer(t *testing.T) {
	rows := append(ownRows(), evInput("corr_BB22", "customer:usr_2"), evChange("corr_BB22", "inv_BB22", "EXECUTING", 6), evChange(evCorr, "inv_FF66", "EXECUTING", 7))
	f := newFeed(t, map[string][]chain.Row{evTen: rows}, 10, nil)
	b, err := f.Since(evCtx(t), princ(evTen, evOwner), evInv, 0)
	if err != nil || strings.Join(stagesOf(b), ",") != "understanding,working,answered" || !b.Done {
		t.Fatalf("%v, %+v", err, b)
	}
}
