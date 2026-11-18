package trustport_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// SYNTHETIC fixtures only (AGENT-25). Proves REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, AGENT-08.

type evalSpy struct {
	got []intake.Request
	res intake.Result
	err error
}

func (e *evalSpy) Evaluate(_ context.Context, r intake.Request) (intake.Result, error) {
	e.got = append(e.got, r)
	return e.res, e.err
}

type intentsStub struct {
	a   intake.AdmittedIntent
	err error
	ids []string
}

func (i *intentsStub) Get(_ context.Context, tenant, id string, version int) (intake.AdmittedIntent, error) {
	i.ids = append(i.ids, tenant+"/"+id)
	return i.a, i.err
}

type recordsStub struct {
	d    trustport.DecisionRecord
	g    trustport.GrantRecord
	derr error
	gerr error
}

func (r *recordsStub) Decision(context.Context, string, string) (trustport.DecisionRecord, error) {
	return r.d, r.derr
}
func (r *recordsStub) Grant(context.Context, string, string) (trustport.GrantRecord, error) {
	return r.g, r.gerr
}

func resolved(t *testing.T) decide.ResolvedIntent {
	t.Helper()
	r, ok := decide.Admit(decide.IntentView{Version: 1}).Resolved()
	if !ok {
		t.Fatal("no resolved intent")
	}
	return r
}

func decisionOf(t *testing.T, o decide.Outcome) decide.Decision {
	t.Helper()
	pass := decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}
	reason, err := decide.Core("INTERNAL_ERROR")
	if err != nil {
		t.Fatal(err)
	}
	r := resolved(t)
	var d decide.Decision
	switch o {
	case decide.Allow:
		d, err = r.Allow(pass)
	case decide.Confirm:
		d, err = r.Confirm(pass)
	case decide.Reject:
		d, err = r.Reject(pass, reason)
	case decide.Unknown:
		d, err = r.Unknown(pass, reason)
	case decide.Escalate:
		d, err = r.Escalate(pass, reason)
	case decide.Wait:
		d, err = r.Wait(pass, 1, reason)
	}
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func withAllow(t *testing.T, r intake.Result) intake.Result {
	r.Decision = decisionOf(t, decide.Allow)
	return r
}

var sess = orchestrator.Session{TenantID: "ten_1", PrincipalID: "prn_1", Role: "customer"}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func port(e *evalSpy, i *intentsStub, r *recordsStub) trustport.Port {
	return trustport.Port{Evaluator: e, Intents: i, Records: r}
}

func allowed(grants ...string) intake.Result {
	return intake.Result{DecisionID: "dec_1", GrantIDs: grants}
}

// A request carries the session's identity, the intent's own correlation and the evidence reference, and nothing from the text (INV-12).
func TestDecideBuildsTheRequestFromTheSessionAndTheStoredIntent(t *testing.T) {
	e := &evalSpy{}
	i := &intentsStub{a: intake.AdmittedIntent{CorrelationID: "corr_1"}}
	_, _ = port(e, i, &recordsStub{}).Decide(ctx(t), sess, "int_ab@3", orchestrator.EvidenceRef{SnapshotID: "snap_1", Unavailable: true})
	if len(e.got) != 1 {
		t.Fatalf("calls = %d", len(e.got))
	}
	want := intake.Request{Session: intake.Session{TenantID: "ten_1", PrincipalID: "prn_1", Role: "customer"}, CorrelationID: "corr_1", IntentRef: "int_ab@3", EvidenceSnapshotID: "snap_1", EvidenceUnavailable: true}
	if e.got[0] != want {
		t.Fatalf("request = %+v", e.got[0])
	}
	if len(i.ids) != 1 || i.ids[0] != "ten_1/int_ab" {
		t.Fatalf("the intent was read as %v", i.ids)
	}
}

func TestDecideMapsEveryOutcomeAndNeverInventsOne(t *testing.T) {
	for out, want := range map[decide.Outcome]contracts.EnumsV1DecisionOutcome{
		decide.Confirm: contracts.EnumsV1DecisionOutcomeCONFIRM, decide.Wait: contracts.EnumsV1DecisionOutcomeWAIT,
		decide.Escalate: contracts.EnumsV1DecisionOutcomeESCALATE, decide.Reject: contracts.EnumsV1DecisionOutcomeREJECT, decide.Unknown: contracts.EnumsV1DecisionOutcomeUNKNOWN,
	} {
		d, err := port(&evalSpy{res: intake.Result{Decision: decisionOf(t, out)}}, &intentsStub{a: intake.AdmittedIntent{CorrelationID: "c"}}, &recordsStub{}).Decide(ctx(t), sess, "int_ab@1", orchestrator.EvidenceRef{})
		if err != nil || d.Outcome != want || d.ID != "" || len(d.GrantIDs) != 0 {
			t.Fatalf("%s: %+v %v", out, d, err)
		}
	}
}

func TestOutcomeOfCoversTheSevenOutcomesAndRefusesAnyOther(t *testing.T) {
	for o, want := range map[decide.Outcome]contracts.EnumsV1DecisionOutcome{
		decide.Allow: contracts.EnumsV1DecisionOutcomeALLOW, decide.Clarify: contracts.EnumsV1DecisionOutcomeCLARIFY, decide.Confirm: contracts.EnumsV1DecisionOutcomeCONFIRM,
		decide.Wait: contracts.EnumsV1DecisionOutcomeWAIT, decide.Escalate: contracts.EnumsV1DecisionOutcomeESCALATE, decide.Reject: contracts.EnumsV1DecisionOutcomeREJECT,
		decide.Unknown: contracts.EnumsV1DecisionOutcomeUNKNOWN,
	} {
		if got, err := trustport.OutcomeOf(o); err != nil || got != want {
			t.Errorf("%s: %v %v", o, got, err)
		}
	}
	for _, bad := range []decide.Outcome{"", "allow", "MAYBE"} {
		if _, err := trustport.OutcomeOf(bad); !errors.Is(err, trustport.ErrBadAnswer) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestAnAllowWithNoDecisionIDIsAnError(t *testing.T) {
	e := &evalSpy{res: withAllow(t, intake.Result{})}
	_, err := port(e, &intentsStub{a: intake.AdmittedIntent{CorrelationID: "c"}}, &recordsStub{}).Decide(ctx(t), sess, "int_ab@1", orchestrator.EvidenceRef{})
	if !errors.Is(err, trustport.ErrBadAnswer) {
		t.Fatalf("err = %v", err)
	}
}

func TestAReadAllowHasNoGrantsAndAWriteAllowKeepsThem(t *testing.T) {
	i := &intentsStub{a: intake.AdmittedIntent{CorrelationID: "c"}}
	r, err := port(&evalSpy{res: withAllow(t, allowed())}, i, &recordsStub{}).Decide(ctx(t), sess, "int_ab@1", orchestrator.EvidenceRef{})
	if err != nil || !r.Read || r.ID != "dec_1" || r.IntentRef != "int_ab@1" || len(r.GrantIDs) != 0 {
		t.Fatalf("read: %+v %v", r, err)
	}
	w, err := port(&evalSpy{res: withAllow(t, allowed("gr_1"))}, i, &recordsStub{}).Decide(ctx(t), sess, "int_ab@1", orchestrator.EvidenceRef{})
	if err != nil || w.Read || len(w.GrantIDs) != 1 || w.GrantIDs[0] != "gr_1" {
		t.Fatalf("write: %+v %v", w, err)
	}
}

func TestDecideRefusesWhatItCannotTrust(t *testing.T) {
	ok := &intentsStub{a: intake.AdmittedIntent{CorrelationID: "c"}}
	for name, c := range map[string]struct {
		s   orchestrator.Session
		ref string
		i   *intentsStub
		e   *evalSpy
		is  error
	}{
		"no tenant":         {orchestrator.Session{PrincipalID: "p"}, "int_ab@1", ok, &evalSpy{}, trustport.ErrSession},
		"no principal":      {orchestrator.Session{TenantID: "t"}, "int_ab@1", ok, &evalSpy{}, trustport.ErrSession},
		"a bad reference":   {sess, "int_ab", ok, &evalSpy{}, trustport.ErrIntentRef},
		"an empty ref":      {sess, "", ok, &evalSpy{}, trustport.ErrIntentRef},
		"no correlation":    {sess, "int_ab@1", &intentsStub{}, &evalSpy{}, trustport.ErrIntentRef},
		"an unknown intent": {sess, "int_ab@1", &intentsStub{err: intake.ErrNotFound}, &evalSpy{}, intake.ErrNotFound},
		"evaluation fails":  {sess, "int_ab@1", ok, &evalSpy{err: intake.ErrNoDeadline}, intake.ErrNoDeadline},
	} {
		if _, err := port(c.e, c.i, &recordsStub{}).Decide(ctx(t), c.s, c.ref, orchestrator.EvidenceRef{}); !errors.Is(err, c.is) {
			t.Errorf("%s: err = %v", name, err)
		}
		if name != "evaluation fails" && len(c.e.got) != 0 {
			t.Errorf("%s: Trust was asked", name)
		}
	}
}

func bindingFor() orchestrator.Binding {
	return orchestrator.Binding{CorrelationID: "corr_1", DecisionID: "dec_1", GrantID: "gr_1", IntentRef: "int_ab@1"}
}

func goodRecords() *recordsStub {
	return &recordsStub{
		d: trustport.DecisionRecord{ID: "dec_1", Outcome: "ALLOW", IntentRef: "int_ab@1"},
		g: trustport.GrantRecord{ID: "gr_1", DecisionID: "dec_1", CorrelationID: "corr_1", PrincipalID: "prn_1"},
	}
}

func TestBoundAcceptsOnlyTheRecordedAllowOfThisTurn(t *testing.T) {
	if err := port(&evalSpy{}, &intentsStub{}, goodRecords()).Bound(ctx(t), sess, bindingFor()); err != nil {
		t.Fatalf("err = %v", err)
	}
	for name, mut := range map[string]func(*recordsStub){
		"the decision is not an ALLOW":        func(r *recordsStub) { r.d.Outcome = "CONFIRM" },
		"the decision is another's":           func(r *recordsStub) { r.d.ID = "dec_2" },
		"the intent is another":               func(r *recordsStub) { r.d.IntentRef = "int_ab@2" },
		"the grant is of another decision":    func(r *recordsStub) { r.g.DecisionID = "dec_2" },
		"the grant is another":                func(r *recordsStub) { r.g.ID = "gr_2" },
		"the grant names another correlation": func(r *recordsStub) { r.g.CorrelationID = "corr_2" },
		"the grant names another principal":   func(r *recordsStub) { r.g.PrincipalID = "prn_2" },
		"the decision is not found":           func(r *recordsStub) { r.derr = trustport.ErrNoRecord },
		"the grant is not found":              func(r *recordsStub) { r.gerr = trustport.ErrNoRecord },
	} {
		r := goodRecords()
		mut(r)
		if err := port(&evalSpy{}, &intentsStub{}, r).Bound(ctx(t), sess, bindingFor()); !errors.Is(err, orchestrator.ErrNotBound) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestBoundSaysItCouldNotTellWhenTheStoreFails(t *testing.T) {
	boom := errors.New("store down")
	r := goodRecords()
	r.derr = boom
	err := port(&evalSpy{}, &intentsStub{}, r).Bound(ctx(t), sess, bindingFor())
	if !errors.Is(err, boom) || errors.Is(err, orchestrator.ErrNotBound) {
		t.Fatalf("err = %v", err)
	}
}

func TestBoundRefusesAnIncompleteBindingOrSession(t *testing.T) {
	for name, mut := range map[string]func(*orchestrator.Binding){
		"no correlation": func(b *orchestrator.Binding) { b.CorrelationID = "" },
		"no decision":    func(b *orchestrator.Binding) { b.DecisionID = "" },
		"no grant":       func(b *orchestrator.Binding) { b.GrantID = "" },
		"no intent":      func(b *orchestrator.Binding) { b.IntentRef = "" },
	} {
		b := bindingFor()
		mut(&b)
		if err := port(&evalSpy{}, &intentsStub{}, goodRecords()).Bound(ctx(t), sess, b); !errors.Is(err, orchestrator.ErrNotBound) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := port(&evalSpy{}, &intentsStub{}, goodRecords()).Bound(ctx(t), orchestrator.Session{}, bindingFor()); !errors.Is(err, trustport.ErrSession) {
		t.Errorf("session: err = %v", err)
	}
}

type usedStub struct {
	used int
	err  error
	got  []intake.DispatchKey
}

func (u *usedStub) Used(_ context.Context, k intake.DispatchKey) (int, error) {
	u.got = append(u.got, k)
	return u.used, u.err
}

// A306: the ledger answers a limit of at least 1 for every capability, a read included (the intake fails closed on less); the limit is the one number the composition
// holds (PERD 2) and the adapter has no number of its own.
func TestDispatchesAnswerALimitOfAtLeastOneForEveryCapability(t *testing.T) {
	if _, err := trustport.NewDispatches(&usedStub{}, 0); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("limit 0: %v", err)
	}
	if _, err := trustport.NewDispatches(nil, 2); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("no ledger: %v", err)
	}
	u := &usedStub{used: 1}
	d, err := trustport.NewDispatches(u, orchestrator.PERDMaxDispatchesPerKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, capID := range []string{"cap_read_status", "cap_write_hold"} {
		k := intake.DispatchKey{TenantID: "ten_1", CorrelationID: "corr_1", CapabilityID: capID, TargetRef: "tgt_1"}
		c, err := d.Count(ctx(t), k)
		if err != nil || c.Limit != 2 || c.Limit < 1 || c.Used != 1 {
			t.Fatalf("%s: %+v %v", capID, c, err)
		}
	}
	if len(u.got) != 2 || u.got[1].CapabilityID != "cap_write_hold" {
		t.Fatalf("the ledger was asked %+v", u.got)
	}
}

func TestDispatchesNeverInventACount(t *testing.T) {
	k := intake.DispatchKey{TenantID: "ten_1", CorrelationID: "corr_1", CapabilityID: "cap", TargetRef: "tgt"}
	d, _ := trustport.NewDispatches(&usedStub{err: errors.New("down")}, 2)
	if _, err := d.Count(ctx(t), k); err == nil {
		t.Fatal("a ledger failure became a count")
	}
	d, _ = trustport.NewDispatches(&usedStub{used: -1}, 2)
	if _, err := d.Count(ctx(t), k); !errors.Is(err, trustport.ErrBadAnswer) {
		t.Fatalf("a negative count: %v", err)
	}
	k.TenantID = ""
	d, _ = trustport.NewDispatches(&usedStub{}, 2)
	if _, err := d.Count(ctx(t), k); !errors.Is(err, trustport.ErrSession) {
		t.Fatalf("no tenant: %v", err)
	}
}
