package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

func mustExecute(t *testing.T, w *world, grantID string) controller.Outcome {
	t.Helper()
	out, err := w.ctl.Execute(ctx(t), tenant, grantID)
	if err != nil {
		t.Fatalf("execute %s: %v", grantID, err)
	}
	return out
}

func verifyChain(t *testing.T, w *world) {
	t.Helper()
	if _, err := chain.Verify(ctx(t), w.admin, tenant); err != nil {
		t.Fatalf("the Record does not verify: %v", err)
	}
}

// PERD 27.1 E1 to E11 against Postgres: one grant, one spend, one dispatch, one signed receipt, the events in order,
// the Outcome verification enqueued, and the chain intact. REQ-EXE-001, REQ-EXE-002, REQ-AUD-001, REQ-AUD-002.
func TestACleanRunThroughPostgres(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	out := mustExecute(t, w, g.GrantID)
	if out.Receipt == nil || out.Receipt.State != controller.StateAcknowledged || out.Receipt.Attempt != 1 {
		t.Fatalf("outcome %+v", out)
	}
	want := []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT"}
	if got := w.events(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("record events %v, want %v", got, want)
	}
	verifyChain(t, w)
	for q, want := range map[string]int{
		`SELECT count(*) FROM grant_spend`: 1,
		`SELECT count(*) FROM execution_receipt WHERE execution_state = 'ACKNOWLEDGED' AND raw_status = 'ACK_2XX:201'`:   1,
		`SELECT count(*) FROM decision_grant WHERE status = 'SPENT'`:                                                     1,
		`SELECT count(*) FROM idempotency_record WHERE status = 'RECEIPTED' AND attempt = 1 AND external_ref = 'CASE-1'`: 1,
		`SELECT count(*) FROM outbox_event WHERE topic = 'outcome.verify' AND dispatched_at IS NULL`:                     1,
		`SELECT count(*) FROM recovery_request`:                                                                          0,
	} {
		if got := w.count(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	if w.target.count() != 1 || !w.target.seenSpent[0] {
		t.Fatalf("dispatches %d, committed-before-dispatch %v; want 1 and [true]", w.target.count(), w.target.seenSpent)
	}
	// REQ-EXE-002 acceptance: a committed idempotency record exists with created_at < dispatched_at.
	if n := w.count(t, `SELECT count(*) FROM idempotency_record ir JOIN execution_receipt r ON r.idempotency_key = ir.idempotency_key WHERE ir.created_at < r.dispatched_at`); n != 1 {
		t.Fatalf("idempotency_record.created_at is not before dispatched_at (%d)", n)
	}
	// The stored receipt is the document the controller signed, with its signature.
	var state, ack string
	if err := w.admin.QueryRow(ctx(t), `SELECT json->>'execution_state', json->>'external_ack' FROM execution_receipt`).Scan(&state, &ack); err != nil || state != "ACKNOWLEDGED" || ack != "CASE-1" {
		t.Fatalf("stored receipt: %q %q %v", state, ack, err)
	}
}

// REQ-EXE-001 acceptance: 100 concurrent spends of one grant produce exactly one dispatch (real goroutines, real Postgres).
func TestHundredConcurrentExecutesOfOneGrantDispatchOnce(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	var wg sync.WaitGroup
	var mu sync.Mutex
	replays, receipts, errs := 0, 0, 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := w.ctl.Execute(ctx(t), tenant, g.GrantID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				errs++
			case out.Replayed:
				replays++
			case out.Receipt != nil:
				receipts++
			}
		}()
	}
	wg.Wait()
	if w.target.count() != 1 || receipts != 1 {
		t.Fatalf("dispatches %d, receipts %d (replays %d, errors %d); want 1 and 1", w.target.count(), receipts, replays, errs)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM grant_spend`:                                   1,
		`SELECT count(*) FROM record_event WHERE event_type = 'GRANT_SPENT'`: 1,
		`SELECT count(*) FROM execution_receipt`:                             1,
		`SELECT count(*) FROM outbox_event WHERE topic = 'outcome.verify'`:   1,
	} {
		if got := w.count(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	verifyChain(t, w)
	t.Logf("100 concurrent executes: 1 dispatch, %d replays, %d errors (nothing was dispatched for them)", replays, errs)
}

// REQ-EXE-001: a void grant records GRANT_VOIDED, one RecoveryRequest and one outbox event, once; it is never spent later.
func TestAVoidGrantRaisesOneRecoveryRequestAndNeverDispatches(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1, func(g *controller.Grant) { g.ExpiresAt = "2026-10-27T08:00:00Z" })
	for i := 0; i < 2; i++ {
		out := mustExecute(t, w, g.GrantID)
		if out.Void != controller.VoidExpired && !out.Replayed {
			t.Fatalf("run %d: outcome %+v, want EXPIRED then a replay", i, out)
		}
	}
	if w.target.count() != 0 {
		t.Fatal("a void grant was dispatched")
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM decision_grant WHERE status = 'VOID'`:                                           1,
		`SELECT count(*) FROM recovery_request WHERE reason = 'EXPIRED' AND status = 'OPEN'`:                  1,
		`SELECT count(*) FROM outbox_event WHERE topic = 'recovery.requested'`:                                1,
		`SELECT count(*) FROM record_event WHERE event_type = 'GRANT_VOIDED' AND body->>'reason' = 'EXPIRED'`: 1,
		`SELECT count(*) FROM grant_spend`:                                                                    0,
	} {
		if got := w.count(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	verifyChain(t, w)
}

// INV-03: a grant that is already void is never spent, even if it still passes every check before E8.
func TestAVoidGrantThatPassesTheChecksIsNeverSpent(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	if err := w.led.Void(ctx(t), g, controller.VoidStateChanged); err != nil {
		t.Fatal(err)
	}
	out := mustExecute(t, w, g.GrantID)
	if !out.Replayed || w.target.count() != 0 {
		t.Fatalf("outcome %+v, dispatches %d; want a replay and no dispatch", out, w.target.count())
	}
	if w.count(t, `SELECT count(*) FROM grant_spend`) != 0 || w.count(t, `SELECT count(*) FROM decision_grant WHERE status = 'VOID'`) != 1 {
		t.Fatal("a void grant was spent")
	}
}

// The sweeper's input is only attempts without a receipt.
func TestDispatchingIsEmptyOnceEveryAttemptHasAReceipt(t *testing.T) {
	w := newWorld(t)
	mustExecute(t, w, w.issue(t, 1).GrantID)
	if left, err := w.led.Dispatching(ctx(t), tenant); err != nil || len(left) != 0 {
		t.Fatalf("dispatching lists %+v (%v) although the attempt has its receipt", left, err)
	}
}

// REQ-EXE-008: the kill switch is read inside the spend transaction. Flipped between E6 and E8, nothing is spent or dispatched.
func TestKillSwitchFlippedBetweenE6AndE8VoidsTheGrant(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	w.bind.onE6 = func() {
		if _, err := w.admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = $1`, tenant); err != nil {
			t.Error(err)
		}
	}
	out := mustExecute(t, w, g.GrantID)
	if out.Void != controller.VoidWritesDisabled || w.target.count() != 0 {
		t.Fatalf("outcome %+v, dispatches %d; want WRITES_DISABLED and none", out, w.target.count())
	}
	if w.count(t, `SELECT count(*) FROM grant_spend`) != 0 || w.count(t, `SELECT count(*) FROM idempotency_record`) != 0 {
		t.Fatal("a disabled spend left rows behind")
	}
	// With the switch already on, E6 voids the grant before E7.
	w2 := newWorld(t)
	if _, err := w2.admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = true`); err != nil {
		t.Fatal(err)
	}
	g2 := w2.issue(t, 1)
	if out := mustExecute(t, w2, g2.GrantID); out.Void != controller.VoidWritesDisabled || w2.target.count() != 0 {
		t.Fatalf("with the switch on: %+v", out)
	}
}

// EC-E09, REQ-EXE-006: a crash after the spend and before the receipt leaves a DISPATCHING attempt; the sweeper
// writes an OUTCOME_UNKNOWN receipt, never dispatches, and writes nothing the second time.
func TestCrashAfterSpendIsSweptToOutcomeUnknownWithoutADispatch(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	w.ctl.Boundary = func(p controller.Point) error {
		if p == controller.PointAfterSpend {
			return controller.ErrCrash
		}
		return nil
	}
	if _, err := w.ctl.Execute(ctx(t), tenant, g.GrantID); !errors.Is(err, controller.ErrCrash) {
		t.Fatalf("err %v, want the injected crash", err)
	}
	w.ctl.Boundary = nil
	pending, err := w.led.Dispatching(ctx(t), tenant)
	if err != nil || len(pending) != 1 || pending[0].GrantID != g.GrantID || pending[0].Attempt != 1 || pending[0].CorrelationID != corr {
		t.Fatalf("dispatching: %+v, %v", pending, err)
	}
	if n, err := w.ctl.Recover(ctx(t), tenant); err != nil || n != 1 {
		t.Fatalf("recover wrote %d (%v), want 1", n, err)
	}
	if left, err := w.led.Dispatching(ctx(t), tenant); err != nil || len(left) != 0 {
		t.Fatalf("after the sweep, dispatching still lists %+v (%v)", left, err)
	}
	if n, err := w.ctl.Recover(ctx(t), tenant); err != nil || n != 0 {
		t.Fatalf("second recover wrote %d (%v), want 0", n, err)
	}
	if w.target.count() != 0 {
		t.Fatal("the sweeper or the crash dispatched")
	}
	if out := mustExecute(t, w, g.GrantID); !out.Replayed {
		t.Fatalf("a spent grant executed again: %+v", out)
	}
	if got := w.count(t, `SELECT count(*) FROM execution_receipt WHERE execution_state = 'OUTCOME_UNKNOWN' AND raw_status = 'SWEEPER_NO_RECEIPT'`); got != 1 {
		t.Fatalf("sweeper receipts = %d, want 1", got)
	}
	verifyChain(t, w)
}

// EC-E16, PERD 32.4: if the Record append fails, the spend fails with it and nothing is dispatched.
func TestAFailedRecordAppendFailsTheSpend(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	w.led.Rec = &chain.Appender{Clock: w.clk, IDs: failingIDs{after: 1, real: w.ids, n: new(int)}} // REVALIDATION_PASSED succeeds, GRANT_SPENT's append fails
	_, err := w.ctl.Execute(ctx(t), tenant, g.GrantID)
	if err == nil {
		t.Fatal("Execute succeeded although the Record could not be written")
	}
	if w.target.count() != 0 {
		t.Fatal("something was dispatched without its Record events")
	}
	for _, q := range []string{`SELECT count(*) FROM grant_spend`, `SELECT count(*) FROM idempotency_record`, `SELECT count(*) FROM decision_grant WHERE status <> 'ISSUED'`} {
		if got := w.count(t, q); got != 0 {
			t.Errorf("%s = %d after a failed append, want 0", q, got)
		}
	}
	if got := w.events(t); !reflect.DeepEqual(got, []string{"REVALIDATION_PASSED"}) {
		t.Fatalf("record events %v, want only REVALIDATION_PASSED", got)
	}
}

type failingIDs struct {
	after int
	real  interface{ ID(string) (string, error) }
	n     *int
}

func (f failingIDs) ID(prefix string) (string, error) {
	if prefix == "recev" {
		*f.n++
		if *f.n > f.after {
			return "", errors.New("injected: the Record cannot allocate an event ID")
		}
	}
	return f.real.ID(prefix)
}

// AGENT-08, PERD 27.2: a retry under a new grant keeps the key and increments the attempt.
func TestRetryUnderANewGrantKeepsTheKeyAndIncrementsTheAttempt(t *testing.T) {
	w := newWorld(t)
	w.target.answer = controller.TransportResult{Class: controller.TransportTimeout}
	g1 := w.issue(t, 1)
	first := mustExecute(t, w, g1.GrantID)
	if first.Receipt == nil || first.Receipt.State != controller.StateOutcomeUnknown {
		t.Fatalf("first attempt: %+v", first)
	}
	w.target.answer = controller.TransportResult{Class: controller.TransportDuplicate409, ExternalAck: "CASE-1", RawCode: "409:DUPLICATE"}
	g2 := w.issue(t, 2)
	second := mustExecute(t, w, g2.GrantID)
	if second.Receipt == nil || second.Receipt.Attempt != 2 || second.Receipt.IdempotencyKey != first.Receipt.IdempotencyKey || second.Receipt.State != controller.StateAcknowledged {
		t.Fatalf("second attempt: %+v", second)
	}
	if w.count(t, `SELECT count(*) FROM idempotency_record WHERE attempt = 2 AND status = 'RECEIPTED' AND external_ref = 'CASE-1'`) != 1 {
		t.Fatal("the idempotency record did not move to attempt 2")
	}
	if w.target.count() != 2 {
		t.Fatalf("dispatches %d, want 2 (one per grant)", w.target.count())
	}
	verifyChain(t, w)
}

// EC-E06, REQ-EXE-002 security: the same key with other parameters is never dispatched, raises a security event and voids the grant.
func TestKeyCollisionRaisesASecurityEventAndVoidsTheGrant(t *testing.T) {
	w := newWorld(t)
	first := mustExecute(t, w, w.issue(t, 1).GrantID)
	if first.Receipt == nil {
		t.Fatalf("setup: %+v", first)
	}
	w.bind.params = map[string]any{"transaction_ref": "TX1", "reason_code": "CUSTOMER_DISPUTE_UNRECOGNIZED", "epoch": int64(1), "priority": "HIGH"}
	g2 := w.issue(t, 2)
	out := mustExecute(t, w, g2.GrantID)
	if out.Void != controller.VoidBindingMismatch {
		t.Fatalf("outcome %+v, want a void grant with BINDING_MISMATCH", out)
	}
	if w.target.count() != 1 {
		t.Fatalf("dispatches %d, want only the first", w.target.count())
	}
	if w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'SECURITY_EVENT' AND body->>'kind' = 'IDEMPOTENCY_COLLISION'`) != 1 {
		t.Fatal("no IDEMPOTENCY_COLLISION security event on the Record")
	}
	if w.count(t, `SELECT count(*) FROM grant_spend`) != 1 || w.count(t, `SELECT count(*) FROM recovery_request WHERE reason = 'BINDING_MISMATCH'`) != 1 {
		t.Fatal("the colliding grant was spent, or no RecoveryRequest was raised")
	}
	verifyChain(t, w)
}

// E11: the first receipt for a grant and attempt wins; a second writer changes nothing and appends nothing.
func TestCompleteIsFirstWriterWins(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	out := mustExecute(t, w, g.GrantID)
	again := *out.Receipt
	again.ReceiptID = "rcp_01OTHERWRITER00000000000A"
	ok, err := w.led.Complete(ctx(t), again)
	if err != nil || ok {
		t.Fatalf("second Complete = %v, %v; want false, nil", ok, err)
	}
	for q, want := range map[string]int{`SELECT count(*) FROM execution_receipt`: 1, `SELECT count(*) FROM record_event WHERE event_type = 'RECEIPT'`: 1,
		`SELECT count(*) FROM outbox_event WHERE topic = 'outcome.verify'`: 1} {
		if got := w.count(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
}

// Load: a grant that does not exist, belongs to another tenant, or carries a field the controller does not know is an error.
func TestLoadRefusesMissingForeignAndUnknownFieldGrants(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	if sg, err := w.led.Load(ctx(t), tenant, g.GrantID); err != nil || sg.Grant.GrantID != g.GrantID || sg.Sig.KeyID != keyID {
		t.Fatalf("load: %+v %v", sg, err)
	}
	if _, err := w.led.Load(ctx(t), tenant, "grt_9999"); err == nil {
		t.Fatal("a missing grant loaded")
	}
	if _, err := w.led.Load(ctx(t), "ten_bb", g.GrantID); err == nil {
		t.Fatal("a grant loaded under another tenant")
	}
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE decision_grant DISABLE TRIGGER decision_grant_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `UPDATE decision_grant SET json = json || '{"surprise": 1}'::jsonb`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.led.Load(ctx(t), tenant, g.GrantID); err == nil {
		t.Fatal("a grant with an unknown field loaded")
	}
}

// Every method needs a context with a deadline (AGENTS.md section 7).
func TestLedgerMethodsNeedADeadline(t *testing.T) {
	w := newWorld(t)
	bg := context.Background()
	if _, err := w.led.Load(bg, tenant, "grt_1"); err == nil {
		t.Error("Load accepted no deadline")
	}
	if _, err := w.led.Spend(bg, controller.SpendCommand{}); err == nil {
		t.Error("Spend accepted no deadline")
	}
	if err := w.led.Void(bg, controller.Grant{}, controller.VoidExpired); err == nil {
		t.Error("Void accepted no deadline")
	}
	if _, err := w.led.Complete(bg, controller.Receipt{}); err == nil {
		t.Error("Complete accepted no deadline")
	}
	if _, err := w.led.Dispatching(bg, tenant); err == nil {
		t.Error("Dispatching accepted no deadline")
	}
	if _, err := w.led.WritesEnabled(bg, tenant); err == nil {
		t.Error("WritesEnabled accepted no deadline")
	}
	if err := w.led.Append(bg, tenant); err == nil {
		t.Error("Append accepted no deadline")
	}
}

// PERD 27.1, REQ-EXE-003, record-event.v2 GrantVoidReason: every void reason reaches the Record and the RecoveryRequest exactly as the
// controller holds it, kept distinct from the others. Eight come from a real path through the controller; REJECTED_STALE_VERSION
// has no controller path yet (a refused version becomes a receipt state, and the grant is already spent: EXE-04), so the Ledger is
// asked to void a grant with it directly.
func TestEveryVoidReasonReachesTheRecordAsWritten(t *testing.T) {
	type scenario struct {
		reason controller.VoidReason
		setup  func(w *world, g controller.Grant)
		grant  []func(*controller.Grant)
		direct bool
	}
	tamper := func(w *world, g controller.Grant) {
		conn, err := w.admin.Acquire(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx(t), `SET session_replication_role = replica`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx(t), `UPDATE decision_grant SET json = jsonb_set(json, '{plan_step}', '9') WHERE grant_id = $1`, g.GrantID); err != nil {
			t.Fatal(err)
		}
	}
	cases := []scenario{
		{reason: controller.VoidSignatureInvalid, setup: tamper},
		{reason: controller.VoidExpired, grant: []func(*controller.Grant){func(g *controller.Grant) { g.ExpiresAt = "2026-10-27T08:00:00Z" }}},
		{reason: controller.VoidIntentSuperseded, setup: func(w *world, _ controller.Grant) { w.bind.intentHash = "sha256:" + fmt.Sprintf("%064d", 7) }},
		{reason: controller.VoidPolicySuperseded, setup: func(w *world, _ controller.Grant) { w.bind.denied = true }},
		{reason: controller.VoidBindingMismatch, setup: func(w *world, _ controller.Grant) { w.bind.params = map[string]any{"transaction_ref": "TX9"} }},
		{reason: controller.VoidWritesDisabled, setup: func(w *world, _ controller.Grant) {
			if _, err := w.admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = true`); err != nil {
				t.Fatal(err)
			}
		}},
		{reason: controller.VoidStateChanged, setup: func(w *world, _ controller.Grant) { w.ctl.Reval = reval{pass: false} }},
		{reason: controller.VoidRevalidationFailed, setup: func(w *world, _ controller.Grant) { w.ctl.Reval = reval{err: errors.New("the read could not be made")} }},
		{reason: controller.VoidTargetVersionStale, direct: true},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(string(c.reason), func(t *testing.T) {
			w := newWorld(t)
			g := w.issue(t, 1, c.grant...)
			if c.setup != nil {
				c.setup(w, g)
			}
			if c.direct {
				if err := w.led.Void(ctx(t), g, c.reason); err != nil {
					t.Fatal(err)
				}
			} else if out := mustExecute(t, w, g.GrantID); out.Void != c.reason {
				t.Fatalf("outcome %+v, want the grant void with %s", out, c.reason)
			}
			if w.target.count() != 0 {
				t.Fatal("a void grant was dispatched")
			}
			if got := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'GRANT_VOIDED' AND body->>'reason' = $1 AND payload->>'reason' = $1`, string(c.reason)); got != 1 {
				t.Fatalf("GRANT_VOIDED with reason %s on the Record: %d, want 1", c.reason, got)
			}
			if got := w.count(t, `SELECT count(*) FROM recovery_request WHERE reason = $1`, string(c.reason)); got != 1 {
				t.Fatalf("RecoveryRequest with reason %s: %d, want 1", c.reason, got)
			}
			verifyChain(t, w)
			seen[string(c.reason)] = true
		})
	}
	if len(seen) != len(chain.GrantVoidReasons()) {
		t.Fatalf("covered %d reasons, the Record accepts %d: %v", len(seen), len(chain.GrantVoidReasons()), chain.GrantVoidReasons())
	}
}
