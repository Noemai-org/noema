//go:build !windows

package outcomee2e_test

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/execution/signing/signingtest"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// TST-05, slice 1 (REQ-TST-008, G3; ruling A30: option B, a Go test with real process death and no new CI job; DEC-2026-10-26 Amendment 1
// lets a core test call mock-case's truth listener). The core is killed with SIGKILL at a named boundary of the controller, in a separate
// process, so nothing is cleaned up and PostgreSQL rolls the open transaction back by itself. The parent then reads the database and the
// target's own truth (the command log), runs the recovery path, and counts effects at every step: never more than one (INV-07, AGENT-08).
//
// How: the test re-executes its own binary (TestCrashChild) with the crash point in the environment. The child attaches to the parent's
// database through the tenant's own logins (no password on the throwaway server), wires the real controller, ledger and case client around
// the real mock-case, and kills itself at the point. Linux only: the matrix needs SIGKILL.
//
// The kill-point list is shared with RESIST S9 (ruling A40); see README.md. SYNTHETIC (AGENT-25). It says nothing about Tushe.

const (
	envChild   = "NOEMA_CRASH_CHILD"
	envDSN     = "NOEMA_CRASH_DSN"
	envMockURL = "NOEMA_CRASH_MOCKCASE_URL"
	envMockTok = "NOEMA_CRASH_MOCKCASE_TOKEN_FILE"
	envGrant   = "NOEMA_CRASH_GRANT"
	envPoint   = "NOEMA_CRASH_POINT"
	envLabel   = "NOEMA_CRASH_LABEL"
	childMiss  = 3 // the child came back from Execute: the point was never reached
)

// killPoint is one boundary of the shared list. S9 is the RESIST family it belongs to (A40).
type killPoint struct {
	name  string
	point controller.Point
	s9    string
	// slice 2: the points after the write. dispatched is true when the target already saw the create when the core dies; lostResponse loads TIMEOUT_AFTER_COMMIT first
	// (the target commits and the response never arrives); receiptAtKill is true when the receipt had already committed.
	dispatched, lostResponse, receiptAtKill bool
}

var killPoints = []killPoint{
	{name: "before the spend commits", point: controller.PointInSpendBeforeCommit, s9: "S9.1 before the write"},
	{name: "after the spend commits and before dispatch", point: controller.PointAfterSpend, s9: "S9.1 before the write"},
	{name: "after dispatch, before the response is classified", point: controller.PointAfterDispatch, s9: "S9.2 after the write, before verify", dispatched: true},
	{name: "the target commits and the response is lost", point: controller.PointAfterDispatch, s9: "S9.2 after the write, before verify", dispatched: true, lostResponse: true},
	{name: "after the receipt is built, before its transaction starts", point: controller.PointInCompleteBeforeCommit, s9: "S9.2 after the write, before verify", dispatched: true},
	{name: "after the receipt commits", point: controller.PointAfterComplete, s9: "S9.2 after the write, before verify", dispatched: true, receiptAtKill: true},
}

// createPlan is the plan step of the first create grant, exactly as trustStandIn.issueCreate records it for n = 1. The child needs it
// because the plan lives in the Trust stand-in's memory, not in the database.
func createPlan() controller.PlanStep {
	return controller.PlanStep{PlanHash: hash(3), Step: 1, CapabilityID: bridge.CapCreateEscalationCase, Registered: true, AdapterExists: true,
		Params:    map[string]any{"transaction_ref": txRef, "reason_code": reasonCode, "created_by_intent": intentRef, "epoch": int64(1)},
		KeyFields: controller.KeyFields{TransactionID: txRef, LogicalAction: "ESCALATE_CASE", ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"},
		TimeoutMS: dispatchMS}
}

// tenantPool connects as the tenant's own login for a power, as testpg.TenantLogin names it: <group>__<tenant>_<database>.
func tenantPool(t *testing.T, dsn, group string) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db := strings.TrimPrefix(u.Path, "/")
	u.User = url.User(group + "__" + tenant + "_" + db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestCrashChild is the helper process of the matrix. Run by hand or in an ordinary run it does nothing; the matrix fails if the child did
// not die by SIGKILL, so a child that skipped cannot pass.
func TestCrashChild(t *testing.T) {
	if os.Getenv(envChild) == "" {
		t.Skip("helper process of the crash matrix (TestCrashMatrix runs it)")
	}
	dsn, grant := os.Getenv(envDSN), os.Getenv(envGrant)
	var point controller.Point
	for _, kp := range killPoints {
		if kp.name == os.Getenv(envPoint) {
			point = kp.point
		}
	}
	execPool, trustPool := tenantPool(t, dsn, "execution_rw"), tenantPool(t, dsn, "trust_rw")
	clk := &tickClock{now: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)} // SYNTHETIC
	gen := ids.New(clk, nil)
	execKeys := crypto.NewKeyRing(map[string][]byte{execKeyID: []byte("synthetic-test-key-not-a-secret")})
	// The Controller needs a receipt signer (noema#229), and its Record events are signed (REC-08): this is core-execution's SYNTHETIC key, the same one the parent registered.
	// The parent derived its receipt key from its own test name (signingtest.Ring), so the child derives the same one from the name the parent passes.
	ring, err := signingtest.RingAt(t.TempDir(), os.Getenv(envLabel), 1)
	if err != nil {
		t.Fatal(err)
	}
	led := &ledger.Ledger{Pool: execPool, Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: gen, Signers: recordsig.Local("outcome-e2e", map[string]chain.Signer{"core-execution": ring}, "core-trust")}, RollbackTimeout: time.Minute}
	cases, err := synthetic_tushe.NewCaseClient(os.Getenv(envMockURL), os.Getenv(envMockTok), nil)
	if err != nil {
		t.Fatal(err)
	}
	trust := &trustStandIn{pool: trustPool, keys: execKeys, clk: clk, led: led, plans: map[string]controller.PlanStep{grant: createPlan()}}
	ctl := &controller.Controller{Clock: clk, Keys: execKeys, Receipts: ring, Grants: led, Bind: trust,
		Reval: bridge.CaseRevalidator{Case: cases}, Ledger: led, Out: bridge.CaseDispatcher{Case: cases}, IDs: gen}
	ctl.Boundary = func(p controller.Point) error {
		if p == point {
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGKILL) // no deferred cleanup runs; the server rolls the open transaction back
			select {}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, _ = ctl.Execute(ctx, tenant, grant)
	os.Exit(childMiss) // the point was not reached
}

// killAt runs the child against the parent's database and mock-case and requires that it died by SIGKILL.
func killAt(t *testing.T, st *stack, mc mockCase, grant string, kp killPoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCrashChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), envChild+"=1", envDSN+"="+st.admin.Config().ConnString(), envMockURL+"="+mc.URL, envMockTok+"="+mc.TokenFile,
		envGrant+"="+grant, envPoint+"="+kp.name, envLabel+"="+t.Name())
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s: the child exited normally; it must die at the point:\n%s", kp.name, out)
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("%s: %v", kp.name, err)
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("%s: the child did not die by SIGKILL (%v):\n%s", kp.name, err, out)
	}
}

// effects is what the target itself reports (its truth), not what the core believes: create commands received and committed for the key,
// and the cases it holds for it.
func effects(t *testing.T, st *stack) (received, committed, cases int) {
	t.Helper()
	received, committed = creates(st.bench.commands(t), theKey())
	for _, c := range st.bench.cases(t) {
		if c.IdempotencyKey == theKey() {
			cases++
		}
	}
	return received, committed, cases
}

func (s *stack) receiptStates(t *testing.T, grant string) []string {
	t.Helper()
	rows, err := s.admin.Query(bg(t), `SELECT execution_state FROM execution_receipt WHERE grant_id = $1 ORDER BY attempt`, grant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			t.Fatal(err)
		}
		out = append(out, st)
	}
	return out
}

// recordCounts asserts the grant's Record events by type: the spend and the dispatch attempt are appended in the spend transaction, and the receipt by the completion
// transaction (the controller's receipt for a live attempt, the sweeper's OUTCOME_UNKNOWN receipt for a dead one). A regression that persists a row without its event, or that
// appends an event twice on replay or recovery, fails here (Codex P1 on noema#498; AGENTS.md section 8, Record events on consequential paths).
func recordCounts(t *testing.T, st *stack, when string, spent, receipts int) {
	t.Helper()
	counts := map[string]int{}
	for _, e := range st.eventTypes(t) {
		counts[e]++
	}
	if counts["GRANT_SPENT"] != spent || counts["DISPATCH_ATTEMPTED"] != spent || counts["RECEIPT"] != receipts {
		t.Fatalf("%s: Record events GRANT_SPENT=%d DISPATCH_ATTEMPTED=%d RECEIPT=%d, want %d, %d and %d", when, counts["GRANT_SPENT"], counts["DISPATCH_ATTEMPTED"], counts["RECEIPT"], spent, spent, receipts)
	}
}

func TestCrashMatrix(t *testing.T) {
	for _, kp := range killPoints {
		t.Run(kp.name, func(t *testing.T) {
			mc := startMockCase(t)
			st := newStack(t, mc)
			grant, err := st.trust.issueCreate(bg(t))
			if err != nil {
				t.Fatal(err)
			}

			if kp.lostResponse { // the real target's own fault: it commits the create and suppresses the response (the same fixture US-06 uses)
				st.bench.loadPlan(t, planWith(t, "fp_crash_response_lost_v1", fault("create_case", "TIMEOUT_AFTER_COMMIT", map[string]any{"delay_ms": 3500})))
			}
			killAt(t, st, mc, grant, kp)

			// What is true at the kill. Before dispatch the target saw nothing. After it, the target holds exactly one committed create under the key.
			spends := st.count(t, `SELECT count(*) FROM grant_spend WHERE grant_id = $1`, grant)
			wantTarget := 0
			if kp.dispatched {
				wantTarget = 1
			}
			if r, c, n := effects(t, st); r != wantTarget || c != wantTarget || n != wantTarget {
				t.Fatalf("%s: at the kill the target received %d create commands, committed %d and holds %d cases, want %d of each", kp.name, r, c, n, wantTarget)
			}
			wantReceipts := 0
			if kp.receiptAtKill {
				wantReceipts = 1
			}
			if got := st.receiptStates(t, grant); len(got) != wantReceipts {
				t.Fatalf("%s: %d receipts at the kill (%v), want %d", kp.name, len(got), got, wantReceipts)
			}
			recordCounts(t, st, kp.name+" at the kill", spends, wantReceipts)
			if kp.lostResponse {
				lost := false
				for _, c := range st.bench.commands(t) {
					lost = lost || (c.Operation == "create_case" && c.ResponseSuppressed && c.FaultEffect != nil && *c.FaultEffect == "TIMEOUT_AFTER_COMMIT")
				}
				if !lost {
					t.Fatalf("%s: the fault trace shows no TIMEOUT_AFTER_COMMIT: the response was not really lost", kp.name)
				}
			}
			if kp.dispatched {
				afterTheWrite(t, st, kp, grant, spends)
				return
			}

			switch kp.point {
			case controller.PointInSpendBeforeCommit:
				// The spend transaction died with the process: the grant is still unspent and can be executed once.
				if spends != 0 {
					t.Fatalf("%s: grant_spend has %d rows, want 0 (the open transaction must have been rolled back)", kp.name, spends)
				}
				out, err := st.ctl.Execute(bg(t), tenant, grant)
				if err != nil || out.Receipt == nil || out.Void != "" || out.Replayed {
					t.Fatalf("%s: executing the unspent grant after the crash: %+v, %v", kp.name, out, err)
				}
				recordCounts(t, st, kp.name+" after the execution", 1, 1)
			case controller.PointAfterSpend:
				// The spend committed and nothing was dispatched. The grant is spent, so it cannot be dispatched again (AGENT-08).
				if spends != 1 {
					t.Fatalf("%s: grant_spend has %d rows, want 1", kp.name, spends)
				}
				out, err := st.ctl.Execute(bg(t), tenant, grant)
				if err != nil || !out.Replayed || out.Receipt != nil {
					t.Fatalf("%s: executing the spent grant again must be a replay with no write: %+v, %v", kp.name, out, err)
				}
				if r, _, _ := effects(t, st); r != 0 {
					t.Fatalf("%s: the target received %d create commands after the replay, want 0", kp.name, r)
				}
				// The sweeper marks the dead attempt UNKNOWN. It never dispatches (EC-E09).
				if n, err := st.ctl.Recover(bg(t), tenant); err != nil || n != 1 {
					t.Fatalf("%s: Recover wrote %d receipts (%v), want 1", kp.name, n, err)
				}
				if got := st.receiptStates(t, grant); len(got) != 1 || got[0] != "OUTCOME_UNKNOWN" {
					t.Fatalf("%s: receipts after recovery %v, want one OUTCOME_UNKNOWN", kp.name, got)
				}
				recordCounts(t, st, kp.name+" after recovery", 1, 1)
				if r, _, _ := effects(t, st); r != 0 {
					t.Fatalf("%s: recovery dispatched: the target received %d create commands, want 0", kp.name, r)
				}
				// Only a new grant may write again.
				fresh, err := st.trust.issueCreate(bg(t))
				if err != nil {
					t.Fatal(err)
				}
				out, err = st.ctl.Execute(bg(t), tenant, fresh)
				if err != nil || out.Receipt == nil || out.Void != "" {
					t.Fatalf("%s: executing a new grant: %+v, %v", kp.name, out, err)
				}
			}

			// Exactly one effect at the end, whatever happened before: one received, one committed, one case.
			if r, c, n := effects(t, st); r != 1 || c != 1 || n != 1 {
				t.Fatalf("%s: final effects: received %d, committed %d, cases %d, want 1, 1, 1", kp.name, r, c, n)
			}
			t.Logf("CRASH_RESULT point=%q s9=%q effects_at_kill=0 effects_final=1 pass=%t", kp.name, kp.s9, !t.Failed())
		})
	}
}

// afterTheWrite is the recovery path for a core that died after the target saw the create (S9.2): the target holds exactly one effect and must keep exactly one. The grant is
// spent, so executing it again is not a write (AGENT-08); the sweeper only ever records what it does not know and never dispatches (EC-E09); and nothing, not the replay and not
// the recovery, sends the target a second create (INV-07: an unknown outcome is verified, never retried blind).
func afterTheWrite(t *testing.T, st *stack, kp killPoint, grant string, spends int) {
	t.Helper()
	if spends != 1 {
		t.Fatalf("%s: grant_spend has %d rows, want 1 (the spend committed before the write)", kp.name, spends)
	}
	// Executing the spent grant again must never write. The controller revalidates (E7) before it reaches the spend, and after the write the target already holds the case for
	// the subject, so revalidation fails and the grant is voided as STATE_CHANGED: that is the form this takes once the target has committed (CI run 43243610931 showed it).
	// A replay is the other safe form. What is forbidden is a receipt, a race or any other void, and a second create at the target (checked below).
	out, err := st.ctl.Execute(bg(t), tenant, grant)
	if err != nil || out.Receipt != nil || out.Raced || !(out.Replayed || out.Void == controller.VoidStateChanged) {
		t.Fatalf("%s: executing the spent grant again must be a replay or a STATE_CHANGED void and never a write: %+v, %v", kp.name, out, err)
	}
	if r, c, n := effects(t, st); r != 1 || c != 1 || n != 1 {
		t.Fatalf("%s: after the replay the target received %d creates, committed %d and holds %d cases, want 1 of each (a second dispatch happened)", kp.name, r, c, n)
	}
	wantReceiptEvents := 0 // the replay or the void appends no receipt; a receipt that committed before the kill keeps its one event
	if kp.receiptAtKill {
		wantReceiptEvents = 1
	}
	recordCounts(t, st, kp.name+" after the replay", 1, wantReceiptEvents)
	wantRecovered := 1 // the dead attempt has no receipt: the sweeper writes OUTCOME_UNKNOWN. If the receipt had committed, there is nothing to record.
	if kp.receiptAtKill {
		wantRecovered = 0
	}
	if n, err := st.ctl.Recover(bg(t), tenant); err != nil || n != wantRecovered {
		t.Fatalf("%s: Recover wrote %d receipts (%v), want %d", kp.name, n, err, wantRecovered)
	}
	got := st.receiptStates(t, grant)
	if len(got) != 1 || (!kp.receiptAtKill && got[0] != "OUTCOME_UNKNOWN") {
		t.Fatalf("%s: receipts after recovery %v, want exactly one (OUTCOME_UNKNOWN when none had committed)", kp.name, got)
	}
	if r, c, n := effects(t, st); r != 1 || c != 1 || n != 1 {
		t.Fatalf("%s: after recovery the target received %d creates, committed %d and holds %d cases, want 1 of each (recovery must not dispatch)", kp.name, r, c, n)
	}
	recordCounts(t, st, kp.name+" after recovery", 1, 1) // exactly one RECEIPT event, never duplicated by the sweeper
	t.Logf("CRASH_RESULT point=%q s9=%q effects_at_kill=1 effects_final=1 pass=%t", kp.name, kp.s9, !t.Failed())
}
