package trustport_test

import (
	"context"
	"errors"
	"testing"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// SYNTHETIC fixtures only (AGENT-25). Proves REQ-TRU-009, REQ-AMB-004; INV-17, INV-25, INV-12.

var cfNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

type cfProfile struct {
	required bool
	err      error
	calls    int
}

func (p *cfProfile) Requires(context.Context, intake.AdmittedIntent) (bool, error) {
	p.calls++
	return p.required, p.err
}

type cfReader struct {
	rec   trustport.ConfirmationRecord
	found bool
	err   error
	asked []string
}

func (r *cfReader) Latest(_ context.Context, tenant, principal, intent string, version int) (trustport.ConfirmationRecord, bool, error) {
	r.asked = append(r.asked, tenant+"/"+principal+"/"+intent)
	return r.rec, r.found, r.err
}

func cfAdmitted() intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: "ten_1", IntentID: "int_1", Version: 1, IntentHash: "sha256:aa", PrincipalID: "prn_1", CorrelationID: "cor_1", TargetRef: "acct_1"}
}

func cfBinding() confirm.BindingInput {
	return confirm.BindingInput{PrincipalID: "prn_1", IntentHash: "sha256:aa", CapabilityID: "cap_1", TargetRef: "acct_1", ParamsHash: "sha256:bb",
		EvidenceSetHash: "sha256:cc", PlanHash: "sha256:dd", PolicyVersion: "pol_1"}
}

func cfRecord() trustport.ConfirmationRecord {
	return trustport.ConfirmationRecord{
		TenantID: "ten_1", EventID: "cfe_1",
		Request: confirm.Request{ID: "cfr_1", Binding: cfBinding(), Requirement: "SELF", RequesterID: "prn_1", ExpiresAt: cfNow.Add(time.Hour).Unix()},
		Event:   confirm.Event{RequestID: "cfr_1", ApproverID: "prn_1", Response: confirm.Accept, At: cfNow.Add(-time.Minute).Unix()},
		Fresh:   cfBinding(),
	}
}

func cfBuild(t *testing.T, p *cfProfile, r *cfReader) intake.Confirmations {
	t.Helper()
	c, err := trustport.NewConfirmations(p, r, clock.NewManual(cfNow))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func cfCtx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func TestConfirmationsNotRequiredWhenTheProfileAsksForNone(t *testing.T) {
	r := &cfReader{err: errors.New("must not be read")}
	got, err := cfBuild(t, &cfProfile{}, r).Status(cfCtx(t), cfAdmitted())
	if err != nil || got.Status != intake.ConfirmationNotRequired || got.EventID != "" || len(r.asked) != 0 {
		t.Fatalf("got %+v %v asked %v", got, err, r.asked)
	}
}

func TestConfirmationsGivenOnlyForAValidAcceptThatResolvedToAFreshEvaluation(t *testing.T) {
	r := &cfReader{rec: cfRecord(), found: true}
	got, err := cfBuild(t, &cfProfile{required: true}, r).Status(cfCtx(t), cfAdmitted())
	if err != nil || got.Status != intake.ConfirmationGiven || got.EventID != "cfe_1" || got.PlanHash != "sha256:dd" {
		t.Fatalf("got %+v %v", got, err)
	}
	if len(r.asked) != 1 || r.asked[0] != "ten_1/prn_1/int_1" {
		t.Fatalf("the store was asked %v: tenant and principal come from the admitted intent (INV-12)", r.asked)
	}
}

func TestConfirmationsPendingForEveryCaseThatIsNotAValidResolvedAccept(t *testing.T) {
	cases := map[string]func(*trustport.ConfirmationRecord){
		"declined":              func(c *trustport.ConfirmationRecord) { c.Event.Response = confirm.Decline },
		"expired at the event":  func(c *trustport.ConfirmationRecord) { c.Event.At = c.Request.ExpiresAt + 1 },
		"expired at use":        func(c *trustport.ConfirmationRecord) { c.Request.ExpiresAt = cfNow.Add(-time.Second).Unix() },
		"approver not eligible": func(c *trustport.ConfirmationRecord) { c.Event.ApproverID = "prn_9" },
		"binding changed":       func(c *trustport.ConfirmationRecord) { c.Fresh.ParamsHash = "sha256:ee" },
		"evidence changed":      func(c *trustport.ConfirmationRecord) { c.Fresh.EvidenceSetHash = "sha256:ff" },
		"policy changed":        func(c *trustport.ConfirmationRecord) { c.Fresh.PolicyVersion = "pol_2" },
		"another request":       func(c *trustport.ConfirmationRecord) { c.Event.RequestID = "cfr_9" },
		"wrong principal bound": func(c *trustport.ConfirmationRecord) { c.Request.Binding.PrincipalID = "prn_9" },
		"wrong requester":       func(c *trustport.ConfirmationRecord) { c.Request.RequesterID = "prn_9"; c.Event.ApproverID = "prn_9" },
		"another intent bound":  func(c *trustport.ConfirmationRecord) { c.Request.Binding.IntentHash = "sha256:99" },
		"unknown reply":         func(c *trustport.ConfirmationRecord) { c.Event.Response = "MAYBE" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rec := cfRecord()
			mutate(&rec)
			got, err := cfBuild(t, &cfProfile{required: true}, &cfReader{rec: rec, found: true}).Status(cfCtx(t), cfAdmitted())
			if err != nil || got.Status != intake.ConfirmationPending || got.EventID != "" || got.PlanHash != "" {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestConfirmationsPendingWhenThereIsNone(t *testing.T) {
	got, err := cfBuild(t, &cfProfile{required: true}, &cfReader{}).Status(cfCtx(t), cfAdmitted())
	if err != nil || got.Status != intake.ConfirmationPending {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestConfirmationsAnotherTenantsRecordIsAnErrorNeverGiven(t *testing.T) {
	rec := cfRecord()
	rec.TenantID = "ten_2"
	got, err := cfBuild(t, &cfProfile{required: true}, &cfReader{rec: rec, found: true}).Status(cfCtx(t), cfAdmitted())
	if !errors.Is(err, trustport.ErrBadAnswer) || got.Status != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestConfirmationsAStoreOrProfileFailureIsAnErrorNeverPendingOrGiven(t *testing.T) {
	boom := errors.New("store down")
	for name, c := range map[string]intake.Confirmations{
		"store":   cfBuild(t, &cfProfile{required: true}, &cfReader{err: boom}),
		"profile": cfBuild(t, &cfProfile{err: boom}, &cfReader{rec: cfRecord(), found: true}),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := c.Status(cfCtx(t), cfAdmitted())
			if !errors.Is(err, boom) || got.Status != 0 || got.EventID != "" {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestConfirmationsAGivenRecordWithNoEventOrAnUnsafeBindingIsAnError(t *testing.T) {
	noEvent := cfRecord()
	noEvent.EventID = ""
	unsafe := cfRecord()
	unsafe.Request.Binding.PlanHash = "bad value"
	for name, rec := range map[string]trustport.ConfirmationRecord{"no event id": noEvent, "unsafe binding": unsafe} {
		t.Run(name, func(t *testing.T) {
			got, err := cfBuild(t, &cfProfile{required: true}, &cfReader{rec: rec, found: true}).Status(cfCtx(t), cfAdmitted())
			if !errors.Is(err, trustport.ErrBadAnswer) || got.Status != 0 {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestConfirmationsRefusesAnIntentWithNoTenantPrincipalOrIntentAndAnAdapterWithNoPort(t *testing.T) {
	for name, mutate := range map[string]func(*intake.AdmittedIntent){
		"tenant":    func(a *intake.AdmittedIntent) { a.TenantID = "" },
		"principal": func(a *intake.AdmittedIntent) { a.PrincipalID = "" },
		"intent":    func(a *intake.AdmittedIntent) { a.IntentID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			a := cfAdmitted()
			mutate(&a)
			p := &cfProfile{required: true}
			got, err := cfBuild(t, p, &cfReader{rec: cfRecord(), found: true}).Status(cfCtx(t), a)
			if !errors.Is(err, trustport.ErrSession) || got.Status != 0 || p.calls != 0 {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
	if _, err := trustport.NewConfirmations(nil, &cfReader{}, clock.NewManual(cfNow)); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("no profile: %v", err)
	}
	if _, err := trustport.NewConfirmations(&cfProfile{}, nil, clock.NewManual(cfNow)); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("no reader: %v", err)
	}
	if _, err := trustport.NewConfirmations(&cfProfile{}, &cfReader{}, nil); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("no clock: %v", err)
	}
	var zero trustport.Confirmations
	if _, err := zero.Status(cfCtx(t), cfAdmitted()); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("zero value: %v", err)
	}
}

func TestConfirmationsNeverReturnsAZeroStatusWithoutAnError(t *testing.T) {
	for _, required := range []bool{false, true} {
		got, err := cfBuild(t, &cfProfile{required: required}, &cfReader{rec: cfRecord(), found: true}).Status(cfCtx(t), cfAdmitted())
		if err == nil && got.Status == 0 {
			t.Fatalf("required=%v: a zero status with no error", required)
		}
	}
}
