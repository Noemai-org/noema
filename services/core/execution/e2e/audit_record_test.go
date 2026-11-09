package e2e_test

// The stored Record must carry what the audit and provenance checks need, including after a restart (S9) and a policy change during execution
// (S11). REQ-AUD-001, REQ-AUD-002, REQ-AUD-003, REQ-EXE-006, INV-10, INV-11, INV-21; EC-E09, EC-E10. Each test runs the real stack against the real
// mock-case, appends through the real appender, and reads back through the audit path (record_read_chain_t and reconstruct.Build) with no other
// source. These were written failing-first against the Record as built on 6 Nov 2026. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

type reconItem struct {
	Facts map[string]any `json:"facts"`
}
type reconSection struct {
	Kind  string      `json:"kind"`
	Items []reconItem `json:"items"`
}

func reconstructSections(t *testing.T, st *stack) map[string][]reconItem {
	t.Helper()
	body, err := reconstruct.Build(bg(t), st.reader(t), tenant, correlation)
	if err != nil {
		t.Fatalf("the audit path cannot reconstruct a correlation the real Execution wrote: %v", err)
	}
	var r struct {
		Sections []reconSection `json:"sections"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	out := map[string][]reconItem{}
	for _, s := range r.Sections {
		out[s.Kind] = s.Items
	}
	return out
}

// G1 (ACTION, every case): a receipt written by the real controller must be reconstructable, and its ACTION must name the capability that was
// executed (a pack capability ID, execution-receipt.v1 logical_action: PackId). The controller wrote the idempotency key's logical action
// ("ESCALATE_CASE") there, which breaks the contract, so the audit path refused every real receipt.
func TestTheRecordOfARealWriteReconstructsAndNamesTheCapability(t *testing.T) {
	st := newStack(t, startMockCase(t))
	first, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	runEscalate(t, st, first)
	action := reconstructSections(t, st)["ACTION"]
	if len(action) == 0 {
		t.Fatal("the ACTION section is empty after a real write")
	}
	want := map[string]bool{bridge.CapCreateEscalationCase: false, bridge.CapRouteToInternalOwner: false}
	for _, it := range action {
		if c, ok := it.Facts["capability_id"].(string); ok {
			want[c] = true
		}
	}
	for c, seen := range want {
		if !seen {
			t.Errorf("no ACTION item names capability %s", c)
		}
	}
}

var errCrash = errors.New("injected crash (S9)")

// G2 (S9, times): the process dies after the spend and the dispatch. The sweeper writes the receipt OUTCOME_UNKNOWN. That receipt must say when the
// attempt was made (the committed spend, grant_spend.spent_at), not when the sweeper ran; the time it was written is the RECEIPT event's own `at`.
func TestARestartedAttemptsReceiptKeepsTheTimeOfTheAttempt(t *testing.T) {
	st := newStack(t, startMockCase(t))
	grant, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	st.ctl.Boundary = func(p controller.Point) error {
		if p == controller.PointAfterDispatch {
			return errCrash
		}
		return nil
	}
	if _, err := (bridge.GrantExecutor{Controller: st.ctl}).ExecuteGrant(bg(t), tenant, grant); err == nil {
		t.Fatal("the injected crash did not stop the run")
	}
	st.ctl.Boundary = nil // the restarted process
	st.clk.Now()          // time passes before the sweeper runs
	st.clk.Now()
	if n, err := st.ctl.Recover(bg(t), tenant); err != nil || n != 1 {
		t.Fatalf("Recover = %d, %v; want 1 receipt", n, err)
	}
	var attempted, dispatchedAt time.Time
	if err := st.admin.QueryRow(bg(t), `SELECT spent_at FROM grant_spend WHERE grant_id = $1`, grant).Scan(&attempted); err != nil {
		t.Fatal(err)
	}
	var s string
	if err := st.admin.QueryRow(bg(t), `SELECT json->>'dispatched_at' FROM execution_receipt WHERE grant_id = $1`, grant).Scan(&s); err != nil {
		t.Fatal(err)
	}
	if dispatchedAt, err = time.Parse(time.RFC3339Nano, s); err != nil {
		t.Fatal(err)
	}
	if !dispatchedAt.Truncate(time.Millisecond).Equal(attempted.Truncate(time.Millisecond)) {
		t.Fatalf("the swept receipt says dispatched_at %s; the attempt was spent at %s", dispatchedAt.Format(time.RFC3339Nano), attempted.Format(time.RFC3339Nano))
	}
}

// G3 (S11, policy version): a policy change after issue voids the grant at E4. The Record must say which policy version the grant was issued under,
// on the void and on the revalidation it failed, and the audit path must serve it.
func TestAPolicyChangeDuringExecutionLeavesThePolicyVersionInTheRecord(t *testing.T) {
	st := newStack(t, startMockCase(t))
	grant, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	st.trust.policyChanged.Store(true)
	run, err := (bridge.GrantExecutor{Controller: st.ctl}).ExecuteGrant(bg(t), tenant, grant)
	if err != nil || run.Void != "POLICY_SUPERSEDED" {
		t.Fatalf("run = %+v, %v; want a void grant (POLICY_SUPERSEDED)", run, err)
	}
	var raw []byte
	if err := st.admin.QueryRow(bg(t), `SELECT payload FROM record_event WHERE event_type = 'GRANT_VOIDED' AND object_ref = $1`, grant).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	want := policyScope + "@1.0"
	if p["policy_version"] != want {
		t.Errorf("GRANT_VOIDED payload %v: policy_version = %v, want %q", p, p["policy_version"], want)
	}
	served := false
	for _, it := range reconstructSections(t, st)["AUTHORITY"] {
		if it.Facts["reason"] == "POLICY_SUPERSEDED" && it.Facts["policy_version"] == want {
			served = true
		}
	}
	if !served {
		t.Error("the AUTHORITY section does not serve the policy version of the voided grant")
	}
}
