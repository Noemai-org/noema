package recovery_test

// RCV-01 slice 1: recovery is evaluated by Trust only. REQ-RCV-001 (every RecoveryRequest is a fresh Trust evaluation; a Trust
// failure gives UNKNOWN; neither Execution nor Outcome starts a write), INV-15, INV-24. Everything is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/recovery"
)

type trust struct {
	dec   recovery.Decision
	err   error
	panic bool
	calls int
	seen  recovery.Request
}

func (t *trust) Evaluate(_ context.Context, r recovery.Request) (recovery.Decision, error) {
	t.calls++
	t.seen = r
	if t.panic {
		panic("trust blew up")
	}
	return t.dec, t.err
}

type alerts struct {
	got  []recovery.Alert
	fail error
}

func (a *alerts) OpsAlert(_ context.Context, al recovery.Alert) error {
	a.got = append(a.got, al)
	return a.fail
}

func req() recovery.Request {
	return recovery.Request{TenantID: "ten_aa", CorrelationID: "corr_abc123", RecoveryRequestID: "rcv_01", Reason: "POSTCONDITION_UNVERIFIED",
		VerdictID: "ver_01", AttemptCount: 1}
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

// REQ-RCV-001: Trust's ALLOW is passed on as an authorised retry that carries Trust's decision; recovery starts no write.
func TestAnAllowFromTrustAuthorisesARetryAndStartsNothing(t *testing.T) {
	tr := &trust{dec: recovery.Decision{DecisionID: "dec_9", Outcome: "ALLOW"}}
	h := recovery.Handler{Trust: tr, Alerts: &alerts{}}
	res, err := h.Handle(ctx(t), req())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != recovery.RetryAuthorised || res.DecisionID != "dec_9" || tr.calls != 1 || tr.seen.RecoveryRequestID != "rcv_01" {
		t.Fatalf("result = %+v, trust calls = %d, seen = %+v", res, tr.calls, tr.seen)
	}
}

// REQ-RCV-001 failure behaviour: a Trust failure is UNKNOWN, and UNKNOWN is ESCALATE with an ops alert (PERD 30.2).
func TestATrustFailureIsUnknownAndEscalatesWithAnOpsAlert(t *testing.T) {
	cases := map[string]*trust{
		"an error":                {err: errors.New("trust unavailable")},
		"a panic":                 {panic: true},
		"UNKNOWN":                 {dec: recovery.Decision{DecisionID: "dec_1", Outcome: "UNKNOWN"}},
		"an outcome nobody knows": {dec: recovery.Decision{DecisionID: "dec_1", Outcome: "MAYBE"}},
		"an empty outcome":        {dec: recovery.Decision{}},
		"ALLOW with no decision":  {dec: recovery.Decision{Outcome: "ALLOW"}},
	}
	for name, tr := range cases {
		al := &alerts{}
		res, err := (recovery.Handler{Trust: tr, Alerts: al}).Handle(ctx(t), req())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Action != recovery.Escalate || res.DecisionID != "" || len(al.got) != 1 || al.got[0].RecoveryRequestID != "rcv_01" {
			t.Fatalf("%s: result = %+v, alerts = %+v", name, res, al.got)
		}
	}
}

// Trust's own non-ALLOW outcomes are never turned into a retry; ESCALATE and REJECT need no alert from recovery (Trust recorded why).
func TestEveryOutcomeButAllowEscalatesAndNeverRetries(t *testing.T) {
	for _, out := range []string{"ESCALATE", "REJECT", "CLARIFY", "CONFIRM", "WAIT"} {
		al := &alerts{}
		res, err := (recovery.Handler{Trust: &trust{dec: recovery.Decision{DecisionID: "dec_2", Outcome: out}}, Alerts: al}).Handle(ctx(t), req())
		if err != nil || res.Action != recovery.Escalate {
			t.Fatalf("%s: result = %+v, err = %v; want ESCALATE", out, res, err)
		}
		if len(al.got) != 0 {
			t.Fatalf("%s: recovery raised an alert for a decision Trust made: %+v", out, al.got)
		}
	}
}

// An alert that cannot be delivered does not turn an escalation into anything else; the failure is returned.
func TestAnAlertThatCannotBeDeliveredStillEscalates(t *testing.T) {
	boom := errors.New("pager down")
	res, err := (recovery.Handler{Trust: &trust{err: errors.New("x")}, Alerts: &alerts{fail: boom}}).Handle(ctx(t), req())
	if !errors.Is(err, boom) || res.Action != recovery.Escalate {
		t.Fatalf("result = %+v, err = %v; want ESCALATE and the alert failure", res, err)
	}
}

// A malformed request is refused before Trust is asked: exactly one of verdict_id and grant_id; every identifier present.
func TestAMalformedRequestIsRefusedBeforeTrustIsAsked(t *testing.T) {
	bad := map[string]func(*recovery.Request){
		"no tenant":           func(r *recovery.Request) { r.TenantID = "" },
		"no correlation":      func(r *recovery.Request) { r.CorrelationID = "" },
		"no request id":       func(r *recovery.Request) { r.RecoveryRequestID = "" },
		"no reason":           func(r *recovery.Request) { r.Reason = "" },
		"a lower-case reason": func(r *recovery.Request) { r.Reason = "postcondition" },
		"neither trigger":     func(r *recovery.Request) { r.VerdictID = "" },
		"both triggers":       func(r *recovery.Request) { r.GrantID = "grt_1" },
		"a negative attempt":  func(r *recovery.Request) { r.AttemptCount = -1 },
	}
	for name, mut := range bad {
		tr := &trust{dec: recovery.Decision{DecisionID: "dec_9", Outcome: "ALLOW"}}
		r := req()
		mut(&r)
		_, err := (recovery.Handler{Trust: tr, Alerts: &alerts{}}).Handle(ctx(t), r)
		if err == nil || tr.calls != 0 {
			t.Fatalf("%s: err = %v, trust calls = %d; want a refusal before Trust", name, err, tr.calls)
		}
	}
	// the voided-grant trigger is valid on its own
	r := req()
	r.VerdictID, r.GrantID = "", "grt_1"
	if _, err := (recovery.Handler{Trust: &trust{dec: recovery.Decision{DecisionID: "d", Outcome: "ESCALATE"}}, Alerts: &alerts{}}).Handle(ctx(t), r); err != nil {
		t.Fatalf("a voided-grant request was refused: %v", err)
	}
}

// Every call has a deadline (AGENTS 7); a context with none is refused, and a missing Trust or Alerts is a refusal, not a nil panic.
func TestNoDeadlineAndMissingPortsAreRefused(t *testing.T) {
	ok := recovery.Handler{Trust: &trust{}, Alerts: &alerts{}}
	if _, err := ok.Handle(context.Background(), req()); err == nil {
		t.Fatal("a context with no deadline was accepted")
	}
	for name, h := range map[string]recovery.Handler{"no Trust": {Alerts: &alerts{}}, "no Alerts": {Trust: &trust{}}} {
		if _, err := h.Handle(ctx(t), req()); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}
