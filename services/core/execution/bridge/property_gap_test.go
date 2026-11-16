package bridge_test

// TST-01 gap 3 (INV-05, INV-18, REQ-CAP-002; INV-08, REQ-EXE-002). Everything here is SYNTHETIC.
//
// INV-05, at the dispatcher: of every capability ID the fintech pack registers, prohibited or not, and of any ID made up, only the allowed ones the bridge maps
// (create_escalation_case, route_to_internal_owner, record_investigation_evidence and log_complaint_reference) may reach an adapter. Every other ID, every prohibited
// one included, gets NO_ADAPTER_FOR_CAPABILITY, makes no adapter call, and has no revalidation either (so a grant for it is void at E7). log_complaint_reference is in
// the mapped set whether or not EXE-09 (ruling A185) has landed: its own behaviour is proved by its own tests, and this test never asserts anything about it.
// INV-08, at the dispatcher: the key the adapter sees is the key of the request, whatever the attempt number.

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/registry"
)

const fintechPack = "../../../../packs/fintech_v1.json"

type failer interface {
	Fatalf(format string, args ...any)
}

func loadRegistry(t testing.TB) *registry.Registry {
	t.Helper()
	b, err := os.ReadFile(fintechPack)
	if err != nil {
		t.Fatal(err)
	}
	r, err := registry.Load(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the fintech pack did not load: %v", err)
	}
	return r
}

var mapped = map[string]bool{bridge.CapCreateEscalationCase: true, bridge.CapRouteToInternalOwner: true, bridge.CapRecordInvestigationEvidence: true, bridge.CapLogComplaintReference: true}

// checkNoPath: whatever shape of request names id, nothing is dispatched to an adapter and nothing is revalidated.
func checkNoPath(t failer, id string) {
	shapes := []func() controller.DispatchRequest{createReq, routeReq, evidenceReq, complaintReq}
	for _, withRecords := range []bool{false, true} {
		for _, shape := range shapes {
			cr, rr := &recorder{write: ack}, &recordsRecorder{write: ack}
			d := bridge.CaseDispatcher{Case: cr}
			if withRecords {
				d.Records = rr
			}
			req := shape()
			req.CapabilityID = id
			got := d.Dispatch(context.Background(), req)
			if got.Class != controller.TransportDefect || got.RawCode != "NO_ADAPTER_FOR_CAPABILITY" ||
				len(cr.creates)+len(cr.routes)+len(rr.evidence)+len(rr.complaints) != 0 {
				t.Fatalf("%q (records adapter %v): %+v with %d adapter calls; want NO_ADAPTER_FOR_CAPABILITY and none", id, withRecords, got,
					len(cr.creates)+len(cr.routes)+len(rr.evidence)+len(rr.complaints))
			}
			v := &recorder{get: openCase()}
			step := controller.PlanStep{CapabilityID: id, Params: req.Params}
			if rev, err := (bridge.CaseRevalidator{Case: v}).Revalidate(context.Background(), recordGrant(), step); err == nil || rev.Passed || len(v.finds) != 0 {
				t.Fatalf("%q: revalidation gave %+v, %v; want an error (the grant is void) and no read", id, rev, err)
			}
		}
	}
}

func TestINV05OnlyTheMappedCapabilitiesReachAnAdapterAndEveryRegisteredIDIsChecked(t *testing.T) {
	r := loadRegistry(t)
	if len(r.Prohibited()) == 0 {
		t.Fatal("no prohibited IDs to test")
	}
	for id := range mapped {
		if !r.Registered(id) || r.IsProhibited(id) {
			t.Fatalf("%s is mapped by the bridge but the pack does not register it as allowed", id)
		}
	}
	checked := 0
	for _, id := range r.IDs() {
		if mapped[id] {
			continue
		}
		checkNoPath(t, id)
		checked++
	}
	if want := len(r.IDs()) - len(mapped); checked != want {
		t.Fatalf("checked %d IDs, want %d", checked, want)
	}
	for _, id := range r.Prohibited() {
		if mapped[id] {
			t.Fatalf("the bridge maps the prohibited capability %s", id)
		}
	}
}

func TestINV05NoIDOutsideTheMappedSetReachesAnAdapter(t *testing.T) {
	r := loadRegistry(t)
	rapid.Check(t, func(rt *rapid.T) {
		id := rapid.OneOf(rapid.SampledFrom(r.Prohibited()), rapid.SampledFrom(r.IDs()), rapid.StringN(0, 60, 80)).Draw(rt, "id")
		switch rapid.IntRange(0, 3).Draw(rt, "disguise") {
		case 1:
			id = strings.ToUpper(id)
		case 2:
			id = " " + id + "\n"
		case 3:
			id = "../" + id
		}
		if mapped[id] {
			rt.Skip()
		}
		checkNoPath(rt, id)
	})
}

// INV-08, REQ-EXE-002: a retry increments the attempt and keeps the key; the adapter sees the request's key and nothing derived from the attempt.
func TestINV08TheAdapterSeesTheRequestsKeyForEveryAttempt(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		key := "idk_v1_" + rapid.StringMatching(`[a-f0-9]{8,64}`).Draw(rt, "key")
		attempt := rapid.Int64Range(1, 9).Draw(rt, "attempt")
		cr, rr := &recorder{write: ack}, &recordsRecorder{write: ack}
		d := bridge.CaseDispatcher{Case: cr, Records: rr}
		for _, shape := range []func() controller.DispatchRequest{createReq, evidenceReq} {
			req := shape()
			req.IdempotencyKey, req.Attempt = key, attempt
			d.Dispatch(context.Background(), req)
		}
		if len(cr.creates) != 1 || len(rr.evidence) != 1 {
			rt.Fatalf("%d creates and %d evidence appends, want one of each", len(cr.creates), len(rr.evidence))
		}
		if cr.creates[0].IdempotencyKey != key || rr.evidence[0].IdempotencyKey != key {
			rt.Fatalf("the adapters saw keys %q and %q for attempt %d, want %q", cr.creates[0].IdempotencyKey, rr.evidence[0].IdempotencyKey, attempt, key)
		}
	})
}
