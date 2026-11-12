package controller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// Controller runs the spend-and-dispatch protocol of PERD section 27.1, steps E1 to E11, for one grant.
//
// It dispatches at most once per Execute call, exactly at E9, and only after E8 has committed the spend,
// the idempotency record and the Record events in one transaction. It has no retry loop and no code path
// that reports success: silence is OUTCOME_UNKNOWN, and "succeeded" belongs to Outcome.
type Controller struct {
	Clock    Clock
	Keys     KeyVerifier
	Receipts ReceiptSigner // required: a Controller with no receipt signer signs nothing and so dispatches nothing that can be receipted
	Grants   GrantSource
	Bind     Binder
	Reval    Revalidator
	Ledger   Ledger
	Out      Dispatcher
	IDs      IDs
	// Boundary is called at every step boundary. Production leaves it nil; the crash harness returns ErrCrash.
	Boundary func(Point) error
}

func (c *Controller) at(p Point) error {
	if c.Boundary == nil {
		return nil
	}
	return c.Boundary(p)
}

// Execute runs E1 to E11. A grant that fails a check is voided and Execute returns the reason in the
// Outcome with a nil error. An error means the protocol could not complete (a read or an append failed, or a
// crash was injected): nothing was dispatched unless the error comes after E9.
func (c *Controller) Execute(ctx context.Context, tenantID, grantID string) (Outcome, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Outcome{}, errors.New(errPrefix + "the context needs a deadline")
	}
	if err := c.at(PointBeforeE1); err != nil {
		return Outcome{}, err
	}
	sg, err := c.Grants.Load(ctx, tenantID, grantID)
	if err != nil {
		return Outcome{}, fmt.Errorf("%sload grant: %w", errPrefix, err)
	}
	g := sg.Grant

	// E1: signature, under any active key ID.
	canon, err := crypto.CanonicalJSON(g)
	if err != nil || !c.Keys.VerifyWith(sg.Sig.KeyID, crypto.GrantSigningMessage(canon), sg.Sig.Value) {
		return c.void(ctx, g, VoidSignatureInvalid)
	}
	if err := c.at(PointAfterE1); err != nil {
		return Outcome{}, err
	}

	// E2: not_before <= now < expires_at, by the clock interface.
	notBefore, err1 := time.Parse(time.RFC3339, g.NotBefore)
	expires, err2 := time.Parse(time.RFC3339, g.ExpiresAt)
	if err1 != nil || err2 != nil || !g.SingleUse || g.CorrelationID == "" || g.TenantID != tenantID {
		return c.void(ctx, g, VoidBindingMismatch)
	}
	if now := c.Clock.Now(); now.Before(notBefore) || !now.Before(expires) {
		return c.void(ctx, g, VoidExpired)
	}
	if err := c.at(PointAfterE2); err != nil {
		return Outcome{}, err
	}

	// E3: the intent is still the latest admitted one.
	latest, err := c.Bind.LatestIntentHash(ctx, g)
	if err != nil {
		return Outcome{}, fmt.Errorf("%sread intent: %w", errPrefix, err)
	}
	if latest != g.IntentHash {
		return c.void(ctx, g, VoidIntentSuperseded)
	}
	if err := c.at(PointAfterE3); err != nil {
		return Outcome{}, err
	}

	// E4: the decision is ALLOW and its policy version is still active.
	allowed, err := c.Bind.DecisionStillAllowed(ctx, g)
	if err != nil {
		return Outcome{}, fmt.Errorf("%sread decision: %w", errPrefix, err)
	}
	if !allowed {
		return c.void(ctx, g, VoidPolicySuperseded)
	}
	// A grant binds the exact policy_id and version it was decided under: if the active version of that policy_id is not the grant's, or the
	// grant names none, or the policy has none, the grant is refused and the case goes back to Trust (a recovery request), never executed.
	active, err := c.Bind.ActivePolicyVersion(ctx, g)
	if err != nil {
		return Outcome{}, fmt.Errorf("%sread active policy: %w", errPrefix, err)
	}
	if !policyBound(g.PolicyVersion) || active != g.PolicyVersion {
		return c.void(ctx, g, VoidPolicySuperseded)
	}
	if err := c.at(PointAfterE4); err != nil {
		return Outcome{}, err
	}

	// E5: the grant binds to the stored plan, a registered non-prohibited capability with an adapter, and the parameters.
	step, err := c.Bind.Plan(ctx, g)
	if err != nil {
		return Outcome{}, fmt.Errorf("%sread plan: %w", errPrefix, err)
	}
	if !bound(g, step) {
		return c.void(ctx, g, VoidBindingMismatch)
	}
	if err := c.at(PointAfterE5); err != nil {
		return Outcome{}, err
	}

	// E6: the tenant kill switch is off and the circuit is closed. An unreadable switch counts as on.
	if on, err := c.Bind.WritesEnabled(ctx, tenantID); err != nil || !on {
		return c.void(ctx, g, VoidWritesDisabled)
	}
	if err := c.at(PointAfterE6); err != nil {
		return Outcome{}, err
	}

	// E7: revalidate. A read that cannot be made voids the grant.
	rev, err := c.Reval.Revalidate(ctx, g, step)
	if err != nil {
		return c.void(ctx, g, VoidRevalidationFailed)
	}
	if !rev.Passed {
		if err := c.Ledger.Append(ctx, tenantID, Event{Type: eventRevalidationFailed, GrantID: g.GrantID, Detail: joinFacts(rev.ChangedFacts)}); err != nil {
			return Outcome{}, fmt.Errorf("%sappend: %w", errPrefix, err)
		}
		return c.void(ctx, g, VoidStateChanged)
	}
	if err := c.Ledger.Append(ctx, tenantID, Event{Type: eventRevalidationPassed, GrantID: g.GrantID}); err != nil {
		return Outcome{}, fmt.Errorf("%sappend: %w", errPrefix, err)
	}
	if err := c.at(PointAfterE7); err != nil {
		return Outcome{}, err
	}

	// The receipt ID is minted before anything is spent or sent. If the source fails, the presentation is refused here:
	// the grant stays unspent and nothing is dispatched.
	receiptID, err := c.IDs.ReceiptID()
	if err != nil {
		return Outcome{}, fmt.Errorf("%smint receipt ID: %w: %v", errPrefix, ErrIDSource, err)
	}

	// E8: one transaction spends the grant, records the idempotency key and appends the Record events. If it
	// fails, nothing is dispatched.
	key := crypto.IdempotencyKey(crypto.IdempotencyKeyFields{TenantID: tenantID, PrincipalID: g.PrincipalID,
		TransactionID: step.KeyFields.TransactionID, LogicalAction: step.KeyFields.LogicalAction, ReasonCode: step.KeyFields.ReasonCode,
		PolicyScope: step.KeyFields.PolicyScope, LogicalResolutionEpoch: step.KeyFields.LogicalResolutionEpoch})
	if err := c.at(PointInSpendBeforeCommit); err != nil {
		return Outcome{}, err
	}
	spend, err := c.Ledger.Spend(ctx, SpendCommand{Grant: g, IdempotencyKey: key, LogicalAction: step.KeyFields.LogicalAction})
	if errors.Is(err, ErrKeyCollision) {
		// The same key with other parameters is never dispatched (EC-E06): the grant is void and the Ledger
		// has already raised the security event.
		return c.void(ctx, g, VoidBindingMismatch)
	}
	if err != nil {
		return Outcome{}, fmt.Errorf("%sspend: %w", errPrefix, err)
	}
	switch {
	case spend.Replay:
		return Outcome{Replayed: true}, nil
	case spend.WritesDisabled:
		return c.void(ctx, g, VoidWritesDisabled)
	case spend.BudgetSpent:
		return Outcome{BudgetSpent: true}, nil // nothing was written; the grant stays ISSUED and the database refuses it again each time
	case !spend.Spent:
		return Outcome{}, errors.New(errPrefix + "the ledger neither spent nor refused the grant")
	}
	if err := c.at(PointAfterSpend); err != nil {
		return Outcome{}, err
	}

	// E9: the one write, with the key and, for a conditional write, the expected versions.
	req := DispatchRequest{TenantID: tenantID, GrantID: g.GrantID, PrincipalID: g.PrincipalID, IdempotencyKey: key, Attempt: spend.Attempt, CapabilityID: g.CapabilityID, TargetRef: g.TargetRef,
		Params: step.Params, TimeoutMS: step.TimeoutMS}
	if step.Conditional {
		req.ExpectedFrom = g.TargetVersions
	}
	dispatchedAt := c.Clock.Now()
	dctx, cancel := context.WithTimeout(ctx, time.Duration(step.TimeoutMS)*time.Millisecond)
	result := c.Out.Dispatch(dctx, req)
	cancel()
	if err := c.at(PointAfterDispatch); err != nil {
		return Outcome{}, err
	}

	// E10: classify. Silence is OUTCOME_UNKNOWN.
	state := Classify(result)
	if result.KeyCollision {
		// The target refused the same key with another payload (EC-E06): a security event, then the receipt.
		if err := c.Ledger.Append(ctx, tenantID, Event{Type: eventSecurity, Kind: kindIdempotencyCollision, GrantID: g.GrantID}); err != nil {
			return Outcome{}, fmt.Errorf("%sappend: %w", errPrefix, err)
		}
	}
	if err := c.at(PointAfterClassify); err != nil {
		return Outcome{}, err
	}

	// E11: the signed receipt, the idempotency record and the Record event in one transaction.
	rc, err := c.receipt(g, receiptID, tenantID, key, step.KeyFields.LogicalAction, spend.Attempt, dispatchedAt, result, state)
	if err != nil {
		return Outcome{}, err
	}
	if err := c.at(PointInCompleteBeforeCommit); err != nil {
		return Outcome{}, err
	}
	written, err := c.completeReceipt(ctx, rc)
	if err != nil {
		return Outcome{}, fmt.Errorf("%scomplete: %w", errPrefix, err)
	}
	if err := c.at(PointAfterComplete); err != nil {
		return Outcome{}, err
	}
	if !written {
		return Outcome{Raced: true}, nil
	}
	return Outcome{Receipt: &rc}, nil
}

// bound is E5: every binding of the grant matches the stored plan step and the registry.
func bound(g Grant, s PlanStep) bool {
	if s.PlanHash != g.PlanHash || s.Step != g.PlanStep || s.CapabilityID != g.CapabilityID {
		return false
	}
	if !s.Registered || s.Prohibited || !s.AdapterExists {
		return false
	}
	h, err := crypto.HashOf(s.Params)
	return err == nil && h == g.ParamsHash
}

func (c *Controller) void(ctx context.Context, g Grant, reason VoidReason) (Outcome, error) {
	if err := c.Ledger.Void(ctx, g, reason); err != nil {
		return Outcome{}, fmt.Errorf("%svoid: %w", errPrefix, err)
	}
	return Outcome{Void: reason}, nil
}

func joinFacts(f []string) string {
	out := ""
	for i, s := range f {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// receipt builds and signs the Execution Receipt. The receipt never says succeeded.
func (c *Controller) receipt(g Grant, receiptID, tenantID, key, action string, attempt int64, at time.Time, r TransportResult, state ExecutionState) (Receipt, error) {
	rc := Receipt{ReceiptID: receiptID, GrantID: g.GrantID, TenantID: tenantID, CorrelationID: g.CorrelationID, IdempotencyKey: key,
		LogicalAction: action, Attempt: attempt, DispatchedAt: at.UTC().Format(time.RFC3339Nano), TransportResult: string(r.Class), RawCode: r.RawCode, State: state}
	if state == StateAcknowledged {
		ack := r.ExternalAck
		rc.ExternalAck = &ack
	}
	return c.sign(rc)
}

// Document is the receipt as it is signed and stored, without the signature (contracts/schemas/execution-receipt.v1.json).
func (rc Receipt) Document() map[string]any {
	doc := map[string]any{"schema_version": receiptSchemaVersion, "correlation_id": rc.CorrelationID,
		"receipt_id": rc.ReceiptID, "grant_id": rc.GrantID, "tenant_id": rc.TenantID, "idempotency_key": rc.IdempotencyKey,
		"logical_action": rc.LogicalAction, "attempt": rc.Attempt, "dispatched_at": rc.DispatchedAt, "transport_result": rc.TransportResult,
		"execution_state": string(rc.State), "external_ack": nil}
	if rc.ExternalAck != nil {
		doc["external_ack"] = *rc.ExternalAck
	}
	return doc
}

func (c *Controller) sign(rc Receipt) (Receipt, error) {
	if c.Receipts == nil {
		return rc, fmt.Errorf("%sno receipt signer", errPrefix)
	}
	keyID, sig, signed, err := c.Receipts.SignReceipt(rc.Document())
	if err != nil {
		return rc, fmt.Errorf("%ssign receipt: %w", errPrefix, err)
	}
	rc.Sig = Sig{KeyID: keyID, Value: sig}
	rc.SignedJCS = signed
	return rc, nil
}

// ErrReceiptSignature is returned when a receipt's signature is refused: no receipt is written.
var ErrReceiptSignature = errors.New(errPrefix + "the receipt's signature does not verify")

// ErrSignatureRefused is what a Ledger returns from Complete when the database refused the receipt's signature (SQLSTATE NS001,
// DEC-2026-11-01 D12). The database verifies every receipt (migration 0013); this controller does not verify it again.
var ErrSignatureRefused = errors.New(errPrefix + "the database refused the receipt's signature")

// completeReceipt hands the receipt to the Ledger (E11, and the sweeper's receipt). The database verifies the Ed25519
// signature before the row exists. If it refuses (a forged receipt, a revoked, retired or unregistered key, or a document that does
// not match what E8 recorded), no receipt is written, the attempt stays DISPATCHING and the sweeper takes it over; the one thing written
// is a SECURITY_EVENT SIGNATURE_INVALID, in its own transaction (rejectReceipt, DEC-2026-11-01 D12). If verification is
// unavailable the insert fails closed with another error, which is returned as it is (D13).
func (c *Controller) completeReceipt(ctx context.Context, rc Receipt) (bool, error) {
	written, err := c.Ledger.Complete(ctx, rc)
	if errors.Is(err, ErrSignatureRefused) {
		return false, c.rejectReceipt(ctx, rc, CauseSignatureNotVerified, nil)
	}
	return written, err
}

// rejectReceipt is the failure branch of completeReceipt (REQ-SEC-005: verification failures fail closed and raise an
// alert; EC-X09; DEC-2026-10-28 section 7). It appends exactly one SECURITY_EVENT SIGNATURE_INVALID, and nothing else, in a
// transaction of its own: there is no object write for it to share (REQ-AUD-001), and the receipt is never written. The
// event names the receipt, the attempt and the cause; it never holds the signature, the key ID the receipt claims or the
// document (all of them untrusted text). The result always wraps ErrReceiptSignature. If the event cannot be appended, the result also carries
// that failure, and nothing is written either.
func (c *Controller) rejectReceipt(ctx context.Context, rc Receipt, cause string, cerr error) error {
	ev := Event{Type: eventSecurity, Kind: kindSignatureInvalid, GrantID: rc.GrantID, Detail: cause,
		Payload: map[string]any{"cause": cause, "receipt_id": rc.ReceiptID, "attempt": rc.Attempt}}
	err := ErrReceiptSignature
	if cerr != nil {
		err = fmt.Errorf("%w: %v", ErrReceiptSignature, cerr)
	}
	if aerr := c.Ledger.Append(ctx, rc.TenantID, ev); aerr != nil {
		return errors.Join(err, fmt.Errorf("%sappend %s: %w", errPrefix, kindSignatureInvalid, aerr))
	}
	return err
}

// policyShape is the grant contract's pattern for policy_version (decision-grant.v1): a lower-case dotted policy ID, "@", and a numeric
// dotted version of two or three components.
var policyShape = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*@[0-9]+\.[0-9]+(\.[0-9]+)?$`)

// policyBound reports whether v has the form the grant contract requires. Only then can it be compared; equality of two malformed values
// proves nothing, so it fails closed.
func policyBound(v string) bool { return policyShape.MatchString(v) }
