package trustport_test

// TRU-14 (REQ-EXE-006, REQ-TRU-002; INV-03, INV-12, AGENT-08; A335 and A363, interim, relayed, not yet committed): the worker's RouteDecider over the intake's continuation
// entry. The route step is its own action decision: the decider asks for the route capability with the case id as a fact, takes the identity from the stored admission and
// never from the worker, and returns exactly what Trust recorded. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

const routeCap = "fin.cap.route_to_internal_owner"

type stepSpy struct {
	got []intake.ContinuationRequest
	res intake.Result
	err error
}

func (s *stepSpy) EvaluateStep(_ context.Context, r intake.ContinuationRequest) (intake.Result, error) {
	s.got = append(s.got, r)
	return s.res, s.err
}

type scopeStub struct {
	s   trustport.RouteScope
	err error
	got []string
	// sessions are the sessions the decider handed to ScopeFor, in order.
	sessions []intake.Session
}

func (s *scopeStub) ScopeFor(_ context.Context, tenantID, correlationID string, session intake.Session) (trustport.RouteScope, error) {
	s.got = append(s.got, tenantID+"/"+correlationID)
	s.sessions = append(s.sessions, session)
	return s.s, s.err
}

type runnableStub struct{}

func (runnableStub) Runnable(context.Context, intake.RunnableQuery) ([]string, error) {
	return []string{routeCap}, nil
}

func routeScope() *scopeStub {
	return &scopeStub{s: trustport.RouteScope{Session: intake.Session{TenantID: "ten_1", PrincipalID: "prn_1", Role: "customer"}, IntentRef: "int_1@2", EvidenceSnapshotID: "evs_1"}}
}

func routeReq() trustport.RouteRequest {
	return trustport.RouteRequest{TenantID: "ten_1", CorrelationID: "corr_1", CaseID: "cas_1", OwnerQueue: "queue_1", CapabilityID: routeCap}
}

func decider(s *stepSpy, sc *scopeStub) trustport.RouteDecider {
	return trustport.RouteDecider{Trust: s, Scopes: sc, Runnable: runnableStub{}}
}

// The continuation is asked for the route capability with the case id as its fact, under the session and the intent of the stored admission, and the runnable port is the
// decider's own: nothing the worker says names the identity (INV-12).
func TestDecideRouteAsksTrustForTheRouteStepWithTheCaseFactUnderTheAdmittedSession(t *testing.T) {
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	sc := routeScope()
	if _, err := decider(s, sc).DecideRoute(ctx(t), routeReq()); err != nil {
		t.Fatal(err)
	}
	if len(s.got) != 1 {
		t.Fatalf("EvaluateStep called %d times, want once", len(s.got))
	}
	g := s.got[0]
	if g.CapabilityID != routeCap || g.Facts["case_id"] != "cas_1" || len(g.Facts) != 1 || g.EvidencePacket != "" || g.Runnable == nil {
		t.Errorf("continuation %+v, want capability %s, facts {case_id: cas_1}, no packet, a runnable port", g, routeCap)
	}
	r := g.Request
	if r.Session.TenantID != "ten_1" || r.Session.PrincipalID != "prn_1" || r.Session.Role != "customer" || r.CorrelationID != "corr_1" || r.IntentRef != "int_1@2" || r.EvidenceSnapshotID != "evs_1" {
		t.Errorf("request %+v, want the admitted session, correlation, intent and snapshot", r)
	}
	if len(sc.got) != 1 || sc.got[0] != "ten_1/corr_1" {
		t.Errorf("the scope was looked up by %v, want tenant/correlation of the request", sc.got)
	}
}

// An ALLOW with exactly one grant returns the outcome, the decision ID and the grant that Trust's records name.
func TestDecideRouteReturnsWhatTrustRecorded(t *testing.T) {
	res, err := decider(&stepSpy{res: withAllow(t, allowed("grt_1"))}, routeScope()).DecideRoute(ctx(t), routeReq())
	if err != nil || res.Outcome != "ALLOW" || res.DecisionID != "dec_1" || len(res.GrantIDs) != 1 || res.GrantIDs[0] != "grt_1" {
		t.Errorf("%+v %v, want ALLOW dec_1 [grt_1]", res, err)
	}
}

// Any outcome but ALLOW is returned as it is and carries no decision ID and no grant (AGENT-08: no grant without an ALLOW).
func TestDecideRouteReturnsNoGrantForAnyOtherOutcome(t *testing.T) {
	for _, o := range []decide.Outcome{decide.Confirm, decide.Wait, decide.Escalate, decide.Reject, decide.Unknown} {
		s := &stepSpy{res: intake.Result{Decision: decisionOf(t, o), DecisionID: "dec_1", GrantIDs: []string{"grt_1"}}}
		res, err := decider(s, routeScope()).DecideRoute(ctx(t), routeReq())
		if err != nil || res.Outcome != string(o) || res.DecisionID != "" || len(res.GrantIDs) != 0 {
			t.Errorf("%s: %+v %v, want the outcome alone and no grant", o, res, err)
		}
	}
}

// An ALLOW that does not carry exactly one grant is an anomaly (a step has one grant): an error and no grant, never the first of several.
func TestDecideRouteRefusesAnAllowWithoutExactlyOneGrant(t *testing.T) {
	for name, grants := range map[string][]string{"none": nil, "two": {"grt_1", "grt_2"}} {
		res, err := decider(&stepSpy{res: withAllow(t, allowed(grants...))}, routeScope()).DecideRoute(ctx(t), routeReq())
		if !errors.Is(err, trustport.ErrBadAnswer) || len(res.GrantIDs) != 0 {
			t.Errorf("%s: %+v %v, want ErrBadAnswer and no grant", name, res, err)
		}
	}
	res, err := decider(&stepSpy{res: intake.Result{Decision: decisionOf(t, decide.Allow), GrantIDs: []string{"grt_1"}}}, routeScope()).DecideRoute(ctx(t), routeReq())
	if !errors.Is(err, trustport.ErrBadAnswer) || len(res.GrantIDs) != 0 {
		t.Errorf("an ALLOW with no decision ID: %+v %v, want ErrBadAnswer and no grant", res, err)
	}
}

// What it cannot trust is an error and Trust is not asked: an incomplete request, another tenant's admission, an unusable scope, a missing port.
func TestDecideRouteRefusesWhatItCannotTrustWithoutAskingTrust(t *testing.T) {
	cases := map[string]struct {
		mut   func(*trustport.RouteRequest)
		scope func() *scopeStub
		d     func(*stepSpy, *scopeStub) trustport.RouteDecider
		want  error
	}{
		"no tenant":          {mut: func(r *trustport.RouteRequest) { r.TenantID = "" }, want: trustport.ErrRouteRequest},
		"no correlation":     {mut: func(r *trustport.RouteRequest) { r.CorrelationID = "" }, want: trustport.ErrRouteRequest},
		"no case":            {mut: func(r *trustport.RouteRequest) { r.CaseID = "" }, want: trustport.ErrRouteRequest},
		"no capability":      {mut: func(r *trustport.RouteRequest) { r.CapabilityID = "" }, want: trustport.ErrRouteRequest},
		"another tenant's":   {scope: func() *scopeStub { s := routeScope(); s.s.Session.TenantID = "ten_2"; return s }, want: trustport.ErrSession},
		"no principal":       {scope: func() *scopeStub { s := routeScope(); s.s.Session.PrincipalID = ""; return s }, want: trustport.ErrSession},
		"no intent":          {scope: func() *scopeStub { s := routeScope(); s.s.IntentRef = ""; return s }, want: trustport.ErrIntentRef},
		"scope lookup fails": {scope: func() *scopeStub { s := routeScope(); s.err = errors.New("injected"); return s }},
		"no trust": {d: func(_ *stepSpy, sc *scopeStub) trustport.RouteDecider {
			return trustport.RouteDecider{Scopes: sc, Runnable: runnableStub{}}
		}, want: trustport.ErrConfig},
		"no scopes": {d: func(s *stepSpy, _ *scopeStub) trustport.RouteDecider {
			return trustport.RouteDecider{Trust: s, Runnable: runnableStub{}}
		}, want: trustport.ErrConfig},
		"no runnable": {d: func(s *stepSpy, sc *scopeStub) trustport.RouteDecider {
			return trustport.RouteDecider{Trust: s, Scopes: sc}
		}, want: trustport.ErrConfig},
	}
	for name, c := range cases {
		s, sc, req := &stepSpy{res: withAllow(t, allowed("grt_1"))}, routeScope(), routeReq()
		if c.scope != nil {
			sc = c.scope()
		}
		if c.mut != nil {
			c.mut(&req)
		}
		d := decider(s, sc)
		if c.d != nil {
			d = c.d(s, sc)
		}
		res, err := d.DecideRoute(ctx(t), req)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) || len(res.GrantIDs) != 0 || len(s.got) != 0 {
			t.Errorf("%s: %+v %v, Trust asked %d times; want an error (%v), no grant and Trust not asked", name, res, err, len(s.got), c.want)
		}
	}
}

// A failure of Trust is returned as an error with no grant and no outcome; an outcome outside the seven is ErrBadAnswer (INV-01: the answer is Trust's, never guessed).
func TestDecideRouteReturnsTrustsErrorAndNeverInventsAnOutcome(t *testing.T) {
	boom := errors.New("injected")
	res, err := decider(&stepSpy{err: boom}, routeScope()).DecideRoute(ctx(t), routeReq())
	if !errors.Is(err, boom) || res.Outcome != "" || len(res.GrantIDs) != 0 {
		t.Errorf("%+v %v, want Trust's error and nothing else", res, err)
	}
	res, err = decider(&stepSpy{}, routeScope()).DecideRoute(ctx(t), routeReq()) // the zero Decision has no outcome
	if !errors.Is(err, trustport.ErrBadAnswer) || len(res.GrantIDs) != 0 {
		t.Errorf("a zero decision: %+v %v, want ErrBadAnswer", res, err)
	}
}
