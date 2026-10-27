package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// Controller runs the spend-and-dispatch protocol of PERD section 27.1, steps E1 to E11, for one grant.
//
// It dispatches at most once per Execute call, exactly at E9, and only after E8 has committed the spend,
// the idempotency record and the Record events in one transaction. It has no retry loop and no code path
// that reports success: silence is OUTCOME_UNKNOWN, and "succeeded" belongs to Outcome.
type Controller struct {
	Clock        Clock
	Keys         KeyVerifier
	ReceiptKeyID string
	Grants       GrantSource
	Bind         Binder
	Reval        Revalidator
	Ledger       Ledger
	Out          Dispatcher
	IDs          IDs
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
	if err != nil || !c.Keys.VerifyWith(sg.Sig.KeyID, canon, sg.Sig.Value) {
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
	case !spend.Spent:
		return Outcome{}, errors.New(errPrefix + "the ledger neither spent nor refused the grant")
	}
	if err := c.at(PointAfterSpend); err != nil {
		return Outcome{}, err
	}

	// E9: the one write, with the key and, for a conditional write, the expected versions.
	req := DispatchRequest{TenantID: tenantID, GrantID: g.GrantID, IdempotencyKey: key, Attempt: spend.Attempt, CapabilityID: g.CapabilityID, TargetRef: g.TargetRef,
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
	rc, err := c.receipt(g, tenantID, key, step.KeyFields.LogicalAction, spend.Attempt, dispatchedAt, result, state)
	if err != nil {
		return Outcome{}, err
	}
	if err := c.at(PointInCompleteBeforeCommit); err != nil {
		return Outcome{}, err
	}
	written, err := c.Ledger.Complete(ctx, rc)
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
func (c *Controller) receipt(g Grant, tenantID, key, action string, attempt int64, at time.Time, r TransportResult, state ExecutionState) (Receipt, error) {
	rc := Receipt{ReceiptID: c.IDs.ReceiptID(), GrantID: g.GrantID, TenantID: tenantID, CorrelationID: g.CorrelationID, IdempotencyKey: key,
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
	canon, err := crypto.CanonicalJSON(rc.Document())
	if err != nil {
		return rc, err
	}
	sig, err := c.Keys.SignWith(c.ReceiptKeyID, canon)
	if err != nil {
		return rc, fmt.Errorf("%ssign receipt: %w", errPrefix, err)
	}
	rc.Sig = Sig{KeyID: c.ReceiptKeyID, Value: sig}
	return rc, nil
}
