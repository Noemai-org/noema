package e2e_test

// EXE-04 / DEBT-01 (REQ-EXE-004, REQ-SEC-005, INV-03, INV-04, EC-X09; DEC-2026-11-01 D12, D13), noema#90 carried onto the Ed25519 world: the controller does
// not verify a receipt itself any more, the database does (migration 0012). A receipt the database refuses (NS001) is never written, the attempt stays
// DISPATCHING for the sweeper, and the one thing written is a SECURITY_EVENT SIGNATURE_INVALID in a transaction of its own. The fault is real: the
// signer holds another key than the one registered for the identity, against the real database and the real mock-case. SYNTHETIC.

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/signing/signingtest"
)

func TestAReceiptTheDatabaseRefusesIsNeverWrittenAndTheSweeperTakesTheAttemptOver(t *testing.T) {
	st := newStack(t, startMockCase(t))
	grant, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	genuine := st.ctl.Receipts
	forger, err := signingtest.RingAt(t.TempDir(), "forger", 1) // same identity and key ID, another private key: not the registered one
	if err != nil {
		t.Fatal(err)
	}
	st.ctl.Receipts = forger
	_, err = (bridge.GrantExecutor{Controller: st.ctl}).ExecuteGrant(bg(t), tenant, grant)
	if !errors.Is(err, controller.ErrReceiptSignature) {
		t.Fatalf("ExecuteGrant = %v, want an error wrapping ErrReceiptSignature", err)
	}
	if n := st.count(t, `SELECT count(*) FROM execution_receipt WHERE grant_id = $1`, grant); n != 0 {
		t.Fatalf("%d receipt(s) stored for a receipt the database refused", n)
	}
	if n := st.count(t, `SELECT count(*) FROM grant_spend WHERE grant_id = $1`, grant); n != 1 {
		t.Fatalf("grant_spend rows = %d, want 1 (the spend committed before the dispatch)", n)
	}
	if n := st.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'SECURITY_EVENT' AND object_ref = $1`, grant); n != 1 {
		t.Fatalf("SECURITY_EVENT rows = %d, want exactly 1", n)
	}
	var payload string
	if err := st.admin.QueryRow(bg(t), `SELECT payload::text FROM record_event WHERE event_type = 'SECURITY_EVENT' AND object_ref = $1`, grant).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "ed25519") || regexp.MustCompile(`[0-9a-f]{64}`).MatchString(payload) {
		t.Errorf("the SECURITY_EVENT payload %s repeats the key ID or a signature value: both are untrusted text", payload)
	}
	// The restarted process has the genuine key: the sweeper writes OUTCOME_UNKNOWN for the attempt and Outcome verifies it against the target.
	st.ctl.Receipts = genuine
	if n, err := st.ctl.Recover(bg(t), tenant); err != nil || n != 1 {
		t.Fatalf("Recover = %d, %v; want 1 receipt", n, err)
	}
	var state string
	if err := st.admin.QueryRow(bg(t), `SELECT execution_state FROM execution_receipt WHERE grant_id = $1`, grant).Scan(&state); err != nil || state != "OUTCOME_UNKNOWN" {
		t.Fatalf("swept receipt state %q, %v; want OUTCOME_UNKNOWN", state, err)
	}
}
