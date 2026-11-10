//go:build !windows

package outcomee2e_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
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
	childMiss  = 3 // the child came back from Execute: the point was never reached
)

// killPoint is one boundary of the shared list. S9 is the RESIST family it belongs to (A40).
type killPoint struct {
	name  string
	point controller.Point
	s9    string
}

var killPoints = []killPoint{
	{"before the spend commits", controller.PointInSpendBeforeCommit, "S9.1 before the write"},
	{"after the spend commits and before dispatch", controller.PointAfterSpend, "S9.1 before the write"},
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
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	led := &ledger.Ledger{Pool: execPool, Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: gen}}
	cases, err := synthetic_tushe.NewCaseClient(os.Getenv(envMockURL), os.Getenv(envMockTok), nil)
	if err != nil {
		t.Fatal(err)
	}
	trust := &trustStandIn{pool: trustPool, keys: execKeys, clk: clk, led: led, plans: map[string]controller.PlanStep{grant: createPlan()}}
	ctl := &controller.Controller{Clock: clk, Keys: receiptRing{hmac: execKeys, priv: priv}, ReceiptKeyID: receiptKeyID, Grants: led, Bind: trust,
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
	out, err := ctl.Execute(ctx, tenant, grant)
	fmt.Fprintf(os.Stderr, "crash child: the kill point %q was not reached: Execute returned %+v, error %v\n", os.Getenv(envPoint), out, err) // the parent prints this; without it a miss is silent
	os.Exit(childMiss)
}

// killAt runs the child against the parent's database and mock-case and requires that it died by SIGKILL.
func killAt(t *testing.T, st *stack, mc mockCase, grant string, kp killPoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCrashChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), envChild+"=1", envDSN+"="+st.admin.Config().ConnString(), envMockURL+"="+mc.URL, envMockTok+"="+mc.TokenFile,
		envGrant+"="+grant, envPoint+"="+kp.name)
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

func TestCrashMatrix(t *testing.T) {
	for _, kp := range killPoints {
		t.Run(kp.name, func(t *testing.T) {
			mc := startMockCase(t)
			st := newStack(t, mc)
			grant, err := st.trust.issueCreate(bg(t))
			if err != nil {
				t.Fatal(err)
			}

			killAt(t, st, mc, grant, kp)

			// What is true at the kill. The target saw nothing at either point: the write was never dispatched.
			spends := st.count(t, `SELECT count(*) FROM grant_spend WHERE grant_id = $1`, grant)
			if r, c, n := effects(t, st); r != 0 || c != 0 || n != 0 {
				t.Fatalf("%s: at the kill the target received %d create commands, committed %d and holds %d cases, want 0 of each", kp.name, r, c, n)
			}
			if got := st.receiptStates(t, grant); len(got) != 0 {
				t.Fatalf("%s: a receipt exists at the kill: %v", kp.name, got)
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
