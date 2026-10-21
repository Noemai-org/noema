package controller_test

import (
	"fmt"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

func keyFor(k controller.KeyFields) string {
	return crypto.IdempotencyKey(crypto.IdempotencyKeyFields{TenantID: TenantID, PrincipalID: PrincipalID, TransactionID: k.TransactionID,
		LogicalAction: k.LogicalAction, ReasonCode: k.ReasonCode, PolicyScope: k.PolicyScope, LogicalResolutionEpoch: k.LogicalResolutionEpoch})
}

// checkSafety checks what must hold at every moment: the properties of REQ-EXE-001 to REQ-EXE-008.
func checkSafety(run *Run, s Snapshot, kind int, killFromStart bool) {
	// REQ-EXE-002: no duplicate logical effect, ever.
	for key, n := range s.Effects {
		if n > 1 {
			run.fail("REQ-EXE-002: %d effects for one key %s", n, key)
		}
	}
	// REQ-EXE-001: a grant is spent at most once, and dispatched at most once.
	seen := map[string]int{}
	for _, grants := range s.Attempts {
		for _, g := range grants {
			seen[g]++
		}
	}
	for g, n := range seen {
		if n > 1 {
			run.fail("REQ-EXE-001: grant %s spent %d times", g, n)
		}
	}
	disp := map[string]int{}
	for _, d := range s.Dispatches {
		disp[d.GrantID]++
		if d.GrantID == "" || !d.RecordedFirst {
			run.fail("REQ-EXE-002: a dispatch of %q was made before its idempotency record and DISPATCH_ATTEMPTED were committed", d.GrantID)
		}
	}
	for g, n := range disp {
		if n > 1 {
			run.fail("REQ-EXE-001: grant %s dispatched %d times", g, n)
		}
	}
	// The grant is refused outright when tampered, expired, or the switch was on from the start.
	if (kind != grantValid || killFromStart) && len(s.Dispatches) != 0 {
		run.fail("REQ-EXE-001/008: %d dispatches for a grant that must never dispatch (kind %d, kill switch %v)", len(s.Dispatches), kind, killFromStart)
	}
	// REQ-EXE-005 and the contract: only the six states, never SUCCEEDED; an ack needs a reference.
	for id, r := range s.Receipts {
		switch r.State {
		case controller.StateDispatched, controller.StateAcknowledged, controller.StateOutcomeUnknown, controller.StateRejectedByTarget,
			controller.StateRejectedStaleVersion, controller.StateFailedTerminal:
		default:
			run.fail("receipt %s has state %q", id, r.State)
		}
		if r.State == controller.StateAcknowledged && (r.ExternalAck == nil || *r.ExternalAck == "") {
			run.fail("receipt %s is ACKNOWLEDGED without an acknowledgement", id)
		}
		if r.State == controller.StateAcknowledged && s.Effects[r.IdempotencyKey] != 1 {
			run.fail("receipt %s is ACKNOWLEDGED but the target has %d effects", id, s.Effects[r.IdempotencyKey])
		}
		if r.Sig.Value == "" {
			run.fail("receipt %s is unsigned", id)
		}
	}
	// PERD 32.4: the Record is a valid hash chain.
	if !ChainValid(s.Record) {
		run.fail("the Record hash chain is broken")
	}
	// REQ-EXE-006: two dispatches of one key need distinct grants and a verdict between them.
	for key, grants := range s.Attempts {
		for i := 1; i < len(grants); i++ {
			if grants[i] == grants[i-1] {
				run.fail("REQ-EXE-006: the same grant dispatched twice for %s", key)
			}
			if !verdictBetween(s.Record, grants[i-1], grants[i]) {
				run.fail("REQ-EXE-006: no verdict between attempts %d and %d of %s", i, i+1, key)
			}
		}
	}
	// A grant is voided at most once (REQ-EXE-001).
	voids := map[string]int{}
	for _, e := range s.Record {
		if e.Type == "GRANT_VOIDED" {
			voids[e.GrantID]++
		}
	}
	for g, n := range voids {
		if n > 1 {
			run.fail("grant %s voided %d times", g, n)
		}
		if s.Spent[g] {
			run.fail("grant %s is both spent and voided", g)
		}
	}
}

func verdictBetween(rec []RecordEntry, firstGrant, secondGrant string) bool {
	start, end := -1, -1
	for i, e := range rec {
		if e.Type == "DISPATCH_ATTEMPTED" && e.GrantID == firstGrant {
			start = i
		}
		if e.Type == "DISPATCH_ATTEMPTED" && e.GrantID == secondGrant {
			end = i
		}
	}
	if start < 0 || end < 0 {
		return false
	}
	for _, e := range rec[start:end] {
		if e.Type == "VERDICT_STANDIN" {
			return true
		}
	}
	return false
}

// String names a point for failure messages.
func PointName(p controller.Point) string {
	names := []string{"BeforeE1", "AfterE1", "AfterE2", "AfterE3", "AfterE4", "AfterE5", "AfterE6", "AfterE7", "InSpendBeforeCommit", "AfterSpend",
		"AfterDispatch", "AfterClassify", "InCompleteBeforeCommit", "AfterComplete"}
	if int(p) < len(names) {
		return names[p]
	}
	return fmt.Sprintf("Point(%d)", int(p))
}
