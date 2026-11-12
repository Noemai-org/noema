package controller_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// PERD v1.18 section 38.2, REQ-EXE-001 Audit, REQ-EXE-004 (DEC-2026-10-28-spent-grant-void): the simulated ledger models the
// one permitted exception, SPENT -> VOID for a confirmed REJECTED_STALE_VERSION refusal, and refuses every other change of
// a grant's status, a duplicate receipt, a duplicate void and a duplicate RecoveryRequest. The 10,000-seed run checks the
// same rules across interleavings (sim_check_test.go); these tests drive each refusal on its own.

var bgctx = context.Background()

// spentWorld has grant grt_1 spent under key idk_1 as attempt 1.
func spentWorld(t *testing.T, grants ...string) *World {
	t.Helper()
	w := NewWorld([]byte("sim-secret-not-a-secret"))
	for i, g := range grants {
		res, err := w.Spend(bgctx, controller.SpendCommand{Grant: controller.Grant{GrantID: g, Nonce: fmt.Sprintf("n_%d", i)}, IdempotencyKey: "idk_" + g, LogicalAction: "A"})
		if err != nil || !res.Spent || res.Attempt != 1 {
			t.Fatalf("spend %s: %+v, %v", g, res, err)
		}
	}
	return w
}

func refusalReceipt(grant string) controller.Receipt {
	return controller.Receipt{ReceiptID: "rcp_" + grant, GrantID: grant, Attempt: 1, State: controller.StateRejectedStaleVersion,
		TransportResult: string(controller.TransportVersionConflict)}
}

func (w *World) counts(grant string) (voided, recovery, receipts int) {
	s := w.Snapshot()
	for _, e := range s.Record {
		if e.GrantID == grant && e.Type == "GRANT_VOIDED" {
			voided++
		}
	}
	for _, g := range s.Recovery {
		if g == grant {
			recovery++
		}
	}
	for _, r := range s.Receipts {
		if r.GrantID == grant {
			receipts++
		}
	}
	return
}

func (w *World) events(grant string) []string {
	var out []string
	for _, e := range w.Snapshot().Record {
		if e.GrantID == grant {
			out = append(out, e.Type)
		}
	}
	return out
}

func TestTheSimulatedLedgerPermitsTheStaleVersionExceptionAndKeepsGrantSpent(t *testing.T) {
	w := spentWorld(t, "grt_1")
	written, err := w.Complete(bgctx, refusalReceipt("grt_1"))
	if err != nil || !written {
		t.Fatalf("complete: %v, %v", written, err)
	}
	if got := w.Snapshot().Status["grt_1"]; got != "VOID" {
		t.Fatalf("status %q, want VOID", got)
	}
	want := []string{"GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "GRANT_VOIDED"}
	if got := w.events("grt_1"); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events %v, want %v: GRANT_SPENT stays on the Record and the void follows the receipt", got, want)
	}
	if v, r, n := w.counts("grt_1"); v != 1 || r != 1 || n != 1 {
		t.Fatalf("voids %d, recovery requests %d, receipts %d, want 1 each", v, r, n)
	}
}

// Every status change the guard refuses: only ISSUED -> SPENT, VOID, EXPIRED and the confirmed SPENT -> VOID are legal.
func TestTheSimulatedLedgerRefusesEveryOtherStatusChange(t *testing.T) {
	states := []string{"ISSUED", "SPENT", "VOID", "EXPIRED", "FROZEN"}
	legal := map[string]bool{"ISSUED>SPENT": true, "ISSUED>VOID": true, "ISSUED>EXPIRED": true} // FROZEN is not a status
	for _, from := range states {
		for _, to := range states {
			if from == to {
				continue
			}
			t.Run(from+" to "+to, func(t *testing.T) {
				w := NewWorld([]byte("sim-secret-not-a-secret"))
				w.status["g"] = from
				err := w.moveLocked("g", to)
				if legal[from+">"+to] {
					if err != nil || w.status["g"] != to {
						t.Fatalf("legal change refused: %v, status %q", err, w.status["g"])
					}
					return
				}
				// SPENT -> VOID with nothing confirming it is refused too.
				if err == nil || w.status["g"] != from {
					t.Fatalf("%s -> %s allowed (%v), status %q", from, to, err, w.status["g"])
				}
			})
		}
	}
}

// A receipt that is not a confirmed refusal never voids the grant; when the state claims a refusal but the guard refuses,
// the receipt is not kept either.
func TestTheSimulatedLedgerDoesNotVoidOnAnyOtherReceipt(t *testing.T) {
	ack := "CASE-1"
	empty := ""
	cases := []struct {
		name    string
		receipt controller.Receipt
		refused bool // the guard refuses: an error, and no receipt
	}{
		{"acknowledged", controller.Receipt{State: controller.StateAcknowledged, TransportResult: "ACK_2XX", ExternalAck: &ack}, false},
		{"rejected by the target", controller.Receipt{State: controller.StateRejectedByTarget, TransportResult: "REJECTED_4XX"}, false},
		{"a refused create (412)", controller.Receipt{State: controller.StateRejectedByTarget, TransportResult: string(controller.TransportDuplicateCreate)}, false},
		{"outcome unknown", controller.Receipt{State: controller.StateOutcomeUnknown, TransportResult: "TIMEOUT"}, false},
		{"failed terminal", controller.Receipt{State: controller.StateFailedTerminal, TransportResult: "DEFECT"}, false},
		{"dispatched", controller.Receipt{State: controller.StateDispatched}, false},
		{"stale state, another transport class", controller.Receipt{State: controller.StateRejectedStaleVersion, TransportResult: "ACK_2XX"}, true},
		{"stale state, no transport class", controller.Receipt{State: controller.StateRejectedStaleVersion}, true},
		{"stale state with an acknowledgement", controller.Receipt{State: controller.StateRejectedStaleVersion, TransportResult: string(controller.TransportVersionConflict), ExternalAck: &ack}, true},
		{"stale state with an empty acknowledgement is still none", controller.Receipt{State: controller.StateRejectedStaleVersion, TransportResult: string(controller.TransportVersionConflict), ExternalAck: &empty}, false},
		{"stale state of another attempt than the spend", controller.Receipt{State: controller.StateRejectedStaleVersion, TransportResult: string(controller.TransportVersionConflict), Attempt: 2}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := spentWorld(t, "grt_1")
			r := c.receipt
			r.GrantID, r.ReceiptID = "grt_1", "rcp_1"
			if r.Attempt == 0 {
				r.Attempt = 1
			}
			written, err := w.Complete(bgctx, r)
			switch {
			case c.refused && (err == nil || written):
				t.Fatalf("the guard did not refuse: %v, %v", written, err)
			case c.refused:
				if v, rq, n := w.counts("grt_1"); v+rq+n != 0 {
					t.Fatalf("a refused receipt left %d voids, %d recovery requests, %d receipts", v, rq, n)
				}
			case err != nil || !written:
				t.Fatalf("receipt not written: %v, %v", written, err)
			}
			wantVoid := c.name == "stale state with an empty acknowledgement is still none"
			if v, rq, _ := w.counts("grt_1"); (v == 1) != wantVoid || (rq == 1) != wantVoid {
				t.Fatalf("voids %d, recovery requests %d; void expected: %v", v, rq, wantVoid)
			}
			if got := w.Snapshot().Status["grt_1"]; wantVoid != (got == "VOID") {
				t.Fatalf("status %q; void expected: %v", got, wantVoid)
			}
		})
	}
}

// Duplicates: the first receipt of a grant and attempt wins; a void and a RecoveryRequest happen once.
func TestTheSimulatedLedgerRefusesADuplicateReceiptVoidAndRecoveryRequest(t *testing.T) {
	w := spentWorld(t, "grt_1")
	if written, err := w.Complete(bgctx, refusalReceipt("grt_1")); err != nil || !written {
		t.Fatal(written, err)
	}
	// a duplicate receipt of the same attempt
	if written, err := w.Complete(bgctx, refusalReceipt("grt_1")); err != nil || written {
		t.Fatalf("a duplicate receipt: written %v, %v; want false", written, err)
	}
	// a second receipt of another attempt, even another refusal, is refused: the grant is no longer SPENT, and nothing is kept
	second := refusalReceipt("grt_1")
	second.Attempt, second.ReceiptID = 2, "rcp_2"
	if written, err := w.Complete(bgctx, second); written || err == nil {
		t.Fatalf("a second attempt's receipt: written %v, %v; want a refusal", written, err)
	}
	// a duplicate void, before and after the stale void
	if err := w.Void(bgctx, controller.Grant{GrantID: "grt_1"}, controller.VoidTargetVersionStale); err != nil {
		t.Fatal(err)
	}
	if err := w.Void(bgctx, controller.Grant{GrantID: "grt_1"}, controller.VoidStateChanged); err != nil {
		t.Fatal(err)
	}
	if v, rq, _ := w.counts("grt_1"); v != 1 || rq != 1 {
		t.Fatalf("voids %d, recovery requests %d after duplicates, want 1 each", v, rq)
	}
}

// Void on a spent grant (the pre-write path) changes nothing: it never takes SPENT to VOID without a refusal.
func TestTheSimulatedLedgerDoesNotVoidASpentGrantThroughTheOrdinaryVoid(t *testing.T) {
	w := spentWorld(t, "grt_1")
	for _, reason := range []controller.VoidReason{controller.VoidStateChanged, controller.VoidTargetVersionStale, controller.VoidWritesDisabled} {
		if err := w.Void(bgctx, controller.Grant{GrantID: "grt_1"}, reason); err != nil {
			t.Fatal(err)
		}
	}
	if v, rq, _ := w.counts("grt_1"); v != 0 || rq != 0 || w.Snapshot().Status["grt_1"] != "SPENT" {
		t.Fatalf("voids %d, recovery requests %d, status %q; a spent grant must stay SPENT", v, rq, w.Snapshot().Status["grt_1"])
	}
}

// An ISSUED grant is voided once, with one RecoveryRequest, and is not spent afterwards.
func TestTheSimulatedLedgerVoidsAnIssuedGrantOnceAndRefusesItsSpend(t *testing.T) {
	w := NewWorld([]byte("sim-secret-not-a-secret"))
	g := controller.Grant{GrantID: "grt_9", Nonce: "n9"}
	for i := 0; i < 3; i++ {
		if err := w.Void(bgctx, g, controller.VoidExpired); err != nil {
			t.Fatal(err)
		}
	}
	if v, rq, _ := w.counts("grt_9"); v != 1 || rq != 1 {
		t.Fatalf("voids %d, recovery requests %d, want 1 each", v, rq)
	}
	if res, err := w.Spend(bgctx, controller.SpendCommand{Grant: g, IdempotencyKey: "idk_9", LogicalAction: "A"}); err != nil || res.Spent || !res.Replay {
		t.Fatalf("spend of a void grant: %+v, %v; want a replay and no spend", res, err)
	}
}

// The confirmation looks at the receipt itself, not at the call that asks for the status change: a receipt of another state
// that records the version conflict, and a grant with more than one receipt, never confirm a refusal, however the change
// is asked for.
func TestTheSimulatedConfirmationRefusesAnotherStateAndMoreThanOneReceipt(t *testing.T) {
	conflict := string(controller.TransportVersionConflict)
	t.Run("another state that records the version conflict", func(t *testing.T) {
		w := spentWorld(t, "grt_1")
		if written, err := w.Complete(bgctx, controller.Receipt{ReceiptID: "rcp_1", GrantID: "grt_1", Attempt: 1, State: controller.StateRejectedByTarget, TransportResult: conflict}); err != nil || !written {
			t.Fatal(written, err)
		}
		if err := w.moveLocked("grt_1", "VOID"); err == nil || w.statusOf("grt_1") != "SPENT" {
			t.Fatalf("SPENT -> VOID after a REJECTED_BY_TARGET receipt: %v, status %q", err, w.statusOf("grt_1"))
		}
	})
	t.Run("more than one receipt", func(t *testing.T) {
		// repeated: the confirmation must not depend on which of the receipts a map yields first
		for i := 0; i < 40; i++ {
			w := spentWorld(t, "grt_1")
			w.receipts["grt_1#1"] = controller.Receipt{GrantID: "grt_1", Attempt: 1, State: controller.StateRejectedStaleVersion, TransportResult: conflict}
			w.receipts["grt_1#2"] = controller.Receipt{GrantID: "grt_1", Attempt: 2, State: controller.StateAcknowledged, TransportResult: "ACK_2XX"}
			if w.confirmedRefusalLocked("grt_1") {
				t.Fatal("a grant with two receipts was confirmed")
			}
		}
	})
}
