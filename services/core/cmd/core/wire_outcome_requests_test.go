package main

// LANE-5 slice 4 (A675, A692, interim, relayed; REQ-VER-001, REQ-SEC-004; INV-04, INV-12, AGENT-13): the write path's Requests, composed per tenant. Each tenant's source reads on that tenant's
// own outcome login; the router sends a grant to its session's tenant only and refuses an unknown or empty tenant before any read; a seam that is missing is a named gap and builds nothing.
// Identifiers are SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/request"
	"github.com/Noemai-org/noema/services/core/outcome/store"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

type reqSeamsStub struct{}

func (reqSeamsStub) Check(context.Context, verifier.Receipt) (verifier.Authenticity, error) {
	return verifier.AuthValid, nil
}
func (reqSeamsStub) For(context.Context, string, string) (request.Outcome, error) {
	return request.Outcome{}, nil
}
func (reqSeamsStub) ID(string) (string, error) { return "vrf_0001", nil }

func reqSeams() outcomeRequestSeams {
	st := reqSeamsStub{}
	return outcomeRequestSeams{Signatures: st, Outcomes: st, IDs: st}
}

type readsTo struct {
	name  string
	calls *[]string
}

func (r readsTo) Load(_ context.Context, tenantID, grantID string) (request.Facts, error) {
	*r.calls = append(*r.calls, r.name+":"+tenantID+":"+grantID)
	return request.Facts{}, request.ErrNotFound
}

func TestACompleteOutcomeRequestSourceReadsOnEachTenantsOwnOutcomeLogin(t *testing.T) { // REQ-SEC-004, INV-12
	_, parts := sideParts(t, "ten_aa", "ten_bb")
	got, gaps := buildOutcomeRequests(parts, reqSeams())
	if got == nil || len(gaps) != 0 {
		t.Fatalf("got %v and gaps %v, want a source and no gap", got, gaps)
	}
	o, ok := got.(*outcomeRequests)
	if !ok || len(o.byTenant) != 2 {
		t.Fatalf("got %T with %d tenants, want two", got, len(o.byTenant))
	}
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		rd, ok := o.byTenant[tenant].Reader.(store.Reader)
		if !ok || rd.Pool != parts.OutcomePools[tenant] || rd.Pool == nil {
			t.Errorf("tenant %s: reader %#v is not a store.Reader on that tenant's outcome pool", tenant, o.byTenant[tenant].Reader)
		}
		if parts.ExecutionPools[tenant] == rd.Pool {
			t.Errorf("tenant %s reads on its execution pool: Outcome reads on its own login only", tenant)
		}
	}
}

func TestNoPartsOrAMissingSeamIsANamedGapAndBuildsNothing(t *testing.T) {
	if got, gaps := buildOutcomeRequests(nil, reqSeams()); got != nil || len(gaps) != 1 || gaps[0] != "outcome requests: execution not configured" {
		t.Errorf("no parts: got %v, gaps %v", got, gaps)
	}
	_, parts := sideParts(t, "ten_aa")
	for want, edit := range map[string]func(s *outcomeRequestSeams){
		"outcome requests: signature check missing":  func(s *outcomeRequestSeams) { s.Signatures = nil },
		"outcome requests: pack outcomes missing":    func(s *outcomeRequestSeams) { s.Outcomes = nil },
		"outcome requests: verification ids missing": func(s *outcomeRequestSeams) { s.IDs = nil },
	} {
		s := reqSeams()
		edit(&s)
		got, gaps := buildOutcomeRequests(parts, s)
		if got != nil || len(gaps) != 1 || gaps[0] != want {
			t.Errorf("%s: got %v, gaps %v", want, got, gaps)
		}
	}
}

func TestTheRouterReadsForTheSessionsTenantOnly(t *testing.T) { // INV-12
	var calls []string
	o := newOutcomeRequests(map[string]request.Reader{"ten_aa": readsTo{"a", &calls}, "ten_bb": readsTo{"b", &calls}}, reqSeams())
	if o == nil {
		t.Fatal("no router")
	}
	if _, err := o.For(t.Context(), "ten_bb", "grt_1"); !errors.Is(err, request.ErrNotFound) {
		t.Fatalf("got %v, want the reader's ErrNotFound for ten_bb", err)
	}
	if len(calls) != 1 || calls[0] != "b:ten_bb:grt_1" {
		t.Errorf("calls = %v, want only ten_bb's reader, asked for ten_bb", calls)
	}
	calls = nil
	for _, tenant := range []string{"", "ten_cc"} {
		if _, err := o.For(t.Context(), tenant, "grt_1"); !errors.Is(err, ErrNoOutcomeForTenant) {
			t.Errorf("tenant %q: got %v, want ErrNoOutcomeForTenant", tenant, err)
		}
	}
	var nilO *outcomeRequests
	if _, err := nilO.For(t.Context(), "ten_aa", "grt_1"); !errors.Is(err, ErrNoOutcomeForTenant) {
		t.Errorf("a nil router: got %v, want ErrNoOutcomeForTenant", err)
	}
	if len(calls) != 0 {
		t.Errorf("a refused call read: %v", calls)
	}
}
