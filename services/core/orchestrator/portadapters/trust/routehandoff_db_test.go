package trustport_test

// TRU-12, ruling A625 (interim, relayed, not yet committed; REQ-TRU-010, REQ-SEC-004; INV-12, BD-05): the orchestrator reads the investigation with its own login, by correlation, and
// hands the session to Trust as typed data on the route request. The caller's own Session is never believed. Real PostgreSQL and the real investigations.Store, CI only. SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

type deciderSpy struct {
	got []trustport.RouteRequest
	res trustport.RouteResult
}

func (d *deciderSpy) DecideRoute(_ context.Context, r trustport.RouteRequest) (trustport.RouteResult, error) {
	d.got = append(d.got, r)
	return d.res, nil
}

func (w *rscWorld) investigation(t *testing.T, tenant, inv, ref, corr, principal, role string) {
	t.Helper()
	if _, err := w.admin.Exec(evdCtx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref)
		VALUES ($1, $2, $3, $4, 'RECEIVED', 0, $5, $6, $7)`, tenant, inv, ref, corr, principal, role, rscAuthn); err != nil {
		t.Fatal(err)
	}
}

func (w *rscWorld) handoff(next trustport.RouteRequestDecider) trustport.RouteSessionHandoff {
	return trustport.RouteSessionHandoff{Sessions: &investigations.Store{Pool: w.orch[evdTenant]}, Next: next}
}

func rscRequest() trustport.RouteRequest {
	return trustport.RouteRequest{TenantID: evdTenant, CorrelationID: evdCorr, CaseID: "cas_1", OwnerQueue: "queue_1", CapabilityID: routeCap}
}

func TestHandoffGivesTrustTheSessionTheInvestigationRecorded(t *testing.T) { // A625, INV-12
	w := rscNewWorld(t)
	w.investigation(t, evdTenant, "inv_AA11", "recev_AA11", evdCorr, "usr_01", "ops")
	spy := &deciderSpy{res: trustport.RouteResult{Outcome: "ALLOW", DecisionID: "dec_1", GrantIDs: []string{"grt_1"}}}
	res, err := w.handoff(spy).DecideRoute(evdCtx(t), rscRequest())
	if err != nil || res.DecisionID != "dec_1" || len(spy.got) != 1 {
		t.Fatalf("res %+v, err %v, asks %d", res, err, len(spy.got))
	}
	want := rscRequest()
	want.Session = intake.Session{TenantID: evdTenant, PrincipalID: "usr_01", Role: "ops"}
	if spy.got[0] != want {
		t.Fatalf("Trust was asked %+v, want %+v", spy.got[0], want)
	}
}

// A Session the caller put on the request is dropped: the identity is the recorded one, never the worker's (INV-12).
func TestHandoffOverwritesASessionTheCallerSupplied(t *testing.T) { // A625, INV-12
	w := rscNewWorld(t)
	w.investigation(t, evdTenant, "inv_AA11", "recev_AA11", evdCorr, "usr_01", "ops")
	spy := &deciderSpy{}
	r := rscRequest()
	r.Session = intake.Session{TenantID: evdTenant, PrincipalID: "usr_EVIL", Role: "approver"}
	if _, err := w.handoff(spy).DecideRoute(evdCtx(t), r); err != nil {
		t.Fatal(err)
	}
	if got := spy.got[0].Session; got != (intake.Session{TenantID: evdTenant, PrincipalID: "usr_01", Role: "ops"}) {
		t.Fatalf("Trust was handed %+v: the caller's session survived", got)
	}
}

// No investigation, another tenant's, several, and one that records no session are each an error and Trust is never asked: no investigation, no scope, no grant.
func TestHandoffAsksTrustNothingWhenTheInvestigationCannotBeRead(t *testing.T) { // A625, REQ-SEC-004
	for name, c := range map[string]struct {
		seed func(w *rscWorld, t *testing.T)
		want error
	}{
		"no investigation": {func(*rscWorld, *testing.T) {}, investigations.ErrNoInvestigation},
		"another tenant's correlation": {func(w *rscWorld, t *testing.T) {
			w.investigation(t, evdOther, "inv_BB22", "recev_BB22", evdCorr, "usr_09", "ops")
		}, investigations.ErrNoInvestigation},
		"several investigations of one correlation": {func(w *rscWorld, t *testing.T) {
			w.investigation(t, evdTenant, "inv_AA11", "recev_AA11", evdCorr, "usr_01", "ops")
			w.investigation(t, evdTenant, "inv_AA12", "recev_AA12", evdCorr, "usr_02", "customer")
		}, investigations.ErrAmbiguousInvestigation},
		"a row that records no session": {func(w *rscWorld, t *testing.T) {
			w.investigation(t, evdTenant, "inv_OLD1", "recev_OLD1", evdCorr, "usr_01", "ops")
			for _, stmt := range []string{`ALTER TABLE investigation DISABLE TRIGGER USER`, `UPDATE investigation SET principal_role = NULL, authn_ref = NULL`} {
				if _, err := w.admin.Exec(evdCtx(t), stmt); err != nil {
					t.Fatal(err)
				}
			}
		}, investigations.ErrNoRecordedSession},
	} {
		t.Run(name, func(t *testing.T) {
			w := rscNewWorld(t)
			c.seed(w, t)
			spy := &deciderSpy{}
			if _, err := w.handoff(spy).DecideRoute(evdCtx(t), rscRequest()); !errors.Is(err, c.want) {
				t.Fatalf("err %v; want %v", err, c.want)
			}
			if len(spy.got) != 0 {
				t.Fatalf("Trust was asked %d times with no readable investigation", len(spy.got))
			}
		})
	}
}

func TestHandoffRefusesAnIncompleteRequestAndAnUnconfiguredHandoff(t *testing.T) { // A625, AGENT-16
	w := rscNewWorld(t)
	spy := &deciderSpy{}
	r := rscRequest()
	r.CorrelationID = ""
	if _, err := w.handoff(spy).DecideRoute(evdCtx(t), r); !errors.Is(err, trustport.ErrRouteRequest) || len(spy.got) != 0 {
		t.Errorf("no correlation: %v, asks %d; want ErrRouteRequest and none", err, len(spy.got))
	}
	for name, h := range map[string]trustport.RouteSessionHandoff{
		"no sessions": {Next: spy},
		"no next":     {Sessions: &investigations.Store{Pool: w.orch[evdTenant]}},
	} {
		if _, err := h.DecideRoute(evdCtx(t), rscRequest()); !errors.Is(err, trustport.ErrConfig) {
			t.Errorf("%s: %v; want ErrConfig", name, err)
		}
	}
}
