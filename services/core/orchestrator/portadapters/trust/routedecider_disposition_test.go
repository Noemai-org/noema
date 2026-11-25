package trustport_test

// TRU-10 (REQ-TRU-010, REQ-TRU-002; INV-03, INV-12, INV-30, AGENT-08; A539 and A562, interim, relayed, not yet committed): the route step is an escalation action decision, so the
// decider hands the ESCALATE disposition to the intake's continuation. The id comes from the workflow input; when the input has none (recovery) it is re-read from the Record by
// the correlation, through the Dispositions port, never from workflow history. Whether the id is recorded on the decision is the intake's rule (A562, noema#903) and is not repeated
// here. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

type dispositionsSpy struct {
	id  string
	err error
	got []string
}

func (d *dispositionsSpy) DispositionFor(_ context.Context, tenantID, correlationID string) (string, error) {
	d.got = append(d.got, tenantID+"/"+correlationID)
	return d.id, d.err
}

func deciderWith(s *stepSpy, d trustport.Dispositions) trustport.RouteDecider {
	r := decider(s, routeScope())
	r.Dispositions = d
	return r
}

// The disposition given goes into the continuation request unchanged, and the Record is not asked. It is an argument and not a field of RouteRequest (A504).
func TestTheDispositionGivenGoesIntoTheContinuation(t *testing.T) {
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	d := &dispositionsSpy{id: "dec_other"}
	if _, err := deciderWith(s, d).DecideRouteAfter(ctx(t), routeReq(), "dec_disposition1"); err != nil {
		t.Fatal(err)
	}
	if len(s.got) != 1 || s.got[0].DispositionDecisionID != "dec_disposition1" {
		t.Fatalf("continuation %+v, want the disposition dec_disposition1", s.got)
	}
	if len(d.got) != 0 {
		t.Errorf("the Record was read %v although the request carried the disposition", d.got)
	}
}

// Recovery: no disposition given, so it is re-read from the Record by the request's tenant and correlation, and goes into the continuation.
func TestARouteWithNoDispositionGivenReReadsItFromTheRecordByCorrelation(t *testing.T) {
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	d := &dispositionsSpy{id: "dec_stored1"}
	if _, err := deciderWith(s, d).DecideRouteAfter(ctx(t), routeReq(), ""); err != nil {
		t.Fatal(err)
	}
	if len(d.got) != 1 || d.got[0] != "ten_1/corr_1" {
		t.Errorf("the Record was read by %v, want one read by ten_1/corr_1", d.got)
	}
	if len(s.got) != 1 || s.got[0].DispositionDecisionID != "dec_stored1" {
		t.Fatalf("continuation %+v, want the stored disposition dec_stored1", s.got)
	}
}

// With no disposition given and none on the Record, nothing is decided: Trust is not asked and no grant comes back. The typed not-found is kept for the caller.
func TestNoDispositionOnTheRecordMeansNoDecisionAndNoGrant(t *testing.T) {
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	res, err := deciderWith(s, &dispositionsSpy{err: trustport.ErrNoDisposition}).DecideRouteAfter(ctx(t), routeReq(), "")
	if !errors.Is(err, trustport.ErrNoDisposition) || len(res.GrantIDs) != 0 || res.DecisionID != "" {
		t.Fatalf("%+v %v, want ErrNoDisposition and no grant", res, err)
	}
	if len(s.got) != 0 {
		t.Errorf("Trust was asked %d times, want none", len(s.got))
	}
}

// A read that fails is not a "none": the decision waits, nothing is asked and nothing is granted (a failure must not read as an absence).
func TestAFailedDispositionReadMeansNoDecisionAndNoGrant(t *testing.T) {
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	boom := errors.New("synthetic record failure")
	res, err := deciderWith(s, &dispositionsSpy{err: boom}).DecideRouteAfter(ctx(t), routeReq(), "")
	if !errors.Is(err, boom) || errors.Is(err, trustport.ErrNoDisposition) || len(res.GrantIDs) != 0 || len(s.got) != 0 {
		t.Fatalf("%+v %v (Trust asked %d times), want the failure, no grant and no ask", res, err, len(s.got))
	}
}

// A reader that says "found" with an empty id is a broken reader: refused, never passed on as "no disposition".
func TestAFoundDispositionWithNoIDIsRefused(t *testing.T) {
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	res, err := deciderWith(s, &dispositionsSpy{id: ""}).DecideRouteAfter(ctx(t), routeReq(), "")
	if !errors.Is(err, trustport.ErrBadAnswer) || len(res.GrantIDs) != 0 || len(s.got) != 0 {
		t.Fatalf("%+v %v (Trust asked %d times), want ErrBadAnswer, no grant and no ask", res, err, len(s.got))
	}
}

// With no Dispositions port the decider behaves as before: an absent disposition stays absent (the composition supplies the port; this is the safer reading to keep the earlier
// behaviour for a decider built without it).
func TestWithNoDispositionsPortAnAbsentDispositionStaysAbsent(t *testing.T) {
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	if _, err := deciderWith(s, nil).DecideRouteAfter(ctx(t), routeReq(), ""); err != nil {
		t.Fatal(err)
	}
	if len(s.got) != 1 || s.got[0].DispositionDecisionID != "" {
		t.Fatalf("continuation %+v, want no disposition", s.got)
	}
}
