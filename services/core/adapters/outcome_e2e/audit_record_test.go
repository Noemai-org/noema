package outcomee2e_test

// Audit and provenance from the stored Record (REQ-AUD-002, REQ-AUD-003; INV-07, INV-21): the chain that Noema's own emitters wrote, in the
// US-06 stack, must reconstruct, and must say who, what was decided under which policy version, what was dispatched, what was verified and
// when. The restart cases (S9: a crash at each boundary of the Execution protocol) must reconstruct too. The policy-change case (S11) is a separate
// change: it needs the E4 void to name both policy versions, which touches the controller that noema#163 is changing. Every payload here is written by the real ledger, controller and Outcome store: nothing
// is hand-built (the reconstruct package's own tests use hand-built payloads). All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

type recon struct {
	Sections []struct {
		Kind  string `json:"kind"`
		Items []struct {
			RecordEventID string         `json:"record_event_id"`
			Facts         map[string]any `json:"facts"`
		} `json:"items"`
	} `json:"sections"`
	Events []struct {
		EventType string `json:"event_type"`
		At        string `json:"at"`
	} `json:"events"`
}

func (r recon) items(kind string) []map[string]any {
	var out []map[string]any
	for _, s := range r.Sections {
		if s.Kind == kind {
			for _, it := range s.Items {
				out = append(out, it.Facts)
			}
		}
	}
	return out
}

// reconstruct builds the body for the stack's correlation as the tenant's record_read login, the only role that may.
func (s *stack) reconstruct(t *testing.T) (recon, error) {
	t.Helper()
	reader := testpg.TenantLogin(t, s.admin, s.dsn, "record_read", tenant)
	raw, err := reconstruct.Build(bg(t), reader, tenant, correlation)
	if err != nil {
		return recon{}, err
	}
	var r recon
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r, nil
}

// The write, the lost response and the verdict of US-06, from the Record alone: the action is the capability that was dispatched, with its
// attempt and its state; the verification is the verdict; every event has a time.
func TestTheRealUS06ChainReconstructsFromTheRecordAlone(t *testing.T) {
	mc := startMockCase(t)
	s := newStack(t, mc)
	plan, err := os.ReadFile(faultFixture)
	if err != nil {
		t.Fatal(err)
	}
	s.bench.loadPlan(t, plan)
	rc := s.dispatch(t)
	s.bench.advance(t, "after_receipt")
	if _, err := s.verify(t, rc.ReceiptID); err != nil {
		t.Fatal(err)
	}
	r, err := s.reconstruct(t)
	if err != nil {
		t.Fatalf("the chain that the real emitters wrote does not reconstruct: %v", err)
	}
	act := r.items("ACTION")
	if len(act) != 1 || act[0]["capability_id"] != bridge.CapCreateEscalationCase || act[0]["execution_state"] != "OUTCOME_UNKNOWN" || act[0]["attempt"] != float64(1) {
		t.Fatalf("ACTION %v, want the create_escalation_case dispatch, attempt 1, OUTCOME_UNKNOWN", act)
	}
	ver := r.items("VERIFICATION")
	if len(ver) != 1 || ver[0]["verdict"] != "ACHIEVED" || ver[0]["verdict_id"] == nil {
		t.Fatalf("VERIFICATION %v, want one ACHIEVED verdict with its id", ver)
	}
	for _, e := range r.Events {
		if e.At == "" {
			t.Errorf("event %s carries no time", e.EventType)
		}
	}
}

// S9: the process dies at a boundary of the Execution protocol. Whatever the Record holds afterwards must reconstruct, and a dispatch that was
// recorded must be visible in the ACTION section even though no receipt follows: a write that may have reached the target and left no receipt is
// OUTCOME_UNKNOWN (INV-07), never an empty action.
func TestEveryCrashPointOfTheExecutionProtocolLeavesARecordThatReconstructs(t *testing.T) {
	dispatchRecorded := map[controller.Point]bool{controller.PointAfterSpend: true, controller.PointAfterDispatch: true, controller.PointAfterClassify: true,
		controller.PointInCompleteBeforeCommit: true}
	for _, p := range controller.Points() {
		if p < controller.PointAfterE7 || p == controller.PointInSpendBeforeCommit {
			continue // nothing governed has been written yet: there is no write to account for
		}
		mc := startMockCase(t)
		s := newStack(t, mc)
		s.ctl.Boundary = func(at controller.Point) error {
			if at == p {
				return controller.ErrCrash
			}
			return nil
		}
		grant, err := s.trust.issueCreate(bg(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.ctl.Execute(bg(t), tenant, grant); !errors.Is(err, controller.ErrCrash) {
			t.Fatalf("point %d: Execute returned %v, want the injected crash", p, err)
		}
		r, err := s.reconstruct(t)
		if err != nil {
			t.Fatalf("crash at point %d: the Record does not reconstruct: %v", p, err)
		}
		act := r.items("ACTION")
		if dispatchRecorded[p] && (len(act) != 1 || act[0]["execution_state"] == nil) {
			t.Errorf("crash at point %d: a dispatch is on the Record but ACTION is %v", p, act)
		}
		if len(r.items("VERIFICATION")) != 0 {
			t.Errorf("crash at point %d: a verification appeared with no verdict on the Record", p)
		}
	}
}

// movedPolicy is the binder after a policy publication: the decision is still ALLOW, but the policy_id's active version is no longer the grant's.
type movedPolicy struct {
	controller.Binder
	active string
}

func (m movedPolicy) ActivePolicyVersion(context.Context, controller.Grant) (string, error) {
	return m.active, nil
}

// S11 (REQ-AUD-003, INV-21, ruling D1): the policy version moved while the grant waited. E4 voids the grant before any write, and the Record must say
// so and name both versions, the one the grant was bound to and the one in force, so a reviewer sees what changed without the database.
func TestAPolicyChangeBeforeTheWriteIsReconstructableWithBothVersions(t *testing.T) {
	mc := startMockCase(t)
	s := newStack(t, mc)
	s.ctl.Bind = movedPolicy{Binder: s.trust, active: policyScope + "@2"}
	grant, err := s.trust.issueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.ctl.Execute(bg(t), tenant, grant)
	if err != nil || out.Void != controller.VoidPolicySuperseded {
		t.Fatalf("Execute: %+v, %v; want a void for POLICY_SUPERSEDED", out, err)
	}
	r, err := s.reconstruct(t)
	if err != nil {
		t.Fatalf("the Record does not reconstruct: %v", err)
	}
	var voided map[string]any
	for _, f := range r.items("AUTHORITY") {
		if f["reason"] == "POLICY_SUPERSEDED" {
			voided = f
		}
	}
	if voided == nil {
		t.Fatalf("AUTHORITY %v holds no POLICY_SUPERSEDED void", r.items("AUTHORITY"))
	}
	if voided["bound_policy_version"] != policyScope+"@1" || voided["active_policy_version"] != policyScope+"@2" {
		t.Fatalf("the void names %v, want bound_policy_version %s@1 and active_policy_version %s@2", voided, policyScope, policyScope)
	}
}
