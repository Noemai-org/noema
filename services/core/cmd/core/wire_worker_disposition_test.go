package main

// TRU-10 (REQ-TRU-010, REQ-EXE-006; INV-03, INV-30; A539, interim, relayed, not yet committed): the route issuer hands the workflow request's disposition to Trust's decision, unchanged,
// and invents none. Every value is SYNTHETIC.

import (
	"context"
	"testing"
	"time"
)

func TestRouteIssuerPassesTheDispositionOfTheRequestToTrust(t *testing.T) {
	d := &deciderFake{result: allowOne()}
	iss, err := newRouteGrantIssuer(d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req := routeReq()
	req.DispositionDecisionID = "dec_disposition1"
	if _, err := iss.IssueRouteGrant(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(d.got) != 1 || d.got[0].DispositionDecisionID != "dec_disposition1" {
		t.Fatalf("Trust was asked %+v, want the disposition dec_disposition1", d.got)
	}
}

func TestRouteIssuerPassesNoDispositionWhenTheRequestHasNone(t *testing.T) {
	d := &deciderFake{result: allowOne()}
	iss, err := newRouteGrantIssuer(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.IssueRouteGrant(context.Background(), routeReq()); err != nil {
		t.Fatal(err)
	}
	if len(d.got) != 1 || d.got[0].DispositionDecisionID != "" {
		t.Fatalf("Trust was asked %+v, want no disposition", d.got)
	}
}
