package recorder

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
)

// ownerQueueRe is the shape of an owner queue as the pack spells it (the same shape the clocks package checks).
var ownerQueueRe = regexp.MustCompile(`\A[A-Z][A-Z0-9_]{0,63}\z`)

// SLARequest is one clock evaluation to record (TRU-06; REQ-TRU-006, REQ-AUD-001; A632, A640, interim, relayed, not yet committed). The tenant and the correlation come from the
// admitted intent and the session, never from a field the caller claims; the evaluation is what clocks.Prioritise returned.
type SLARequest struct {
	TenantID      string
	CorrelationID string
	Evaluation    clocks.Evaluation
}

// AppendSLAEvaluated appends one SLA_EVALUATED event for a clock evaluation, in tx. It is written by the emitter's own identity (no authenticated person causes it, INV-12), names the
// correlation as its object, and carries every field of the evaluation as its payload (clocks.Evaluation.EventPayload). The caller supplies the transaction and commits or rolls back
// (EC-E16): if the append fails the error is returned and the caller rolls back, so no evaluation is acted on without its event. A request that is not an evaluation is ErrRequest,
// before anything is written (INV-16).
func (w *Writer) AppendSLAEvaluated(ctx context.Context, tx pgx.Tx, r SLARequest) error {
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("%sthe context needs a deadline", errPrefix)
	}
	if w == nil || w.Appender == nil {
		return fmt.Errorf("%swriter is not configured", errPrefix)
	}
	if err := checkSLARequest(r); err != nil {
		return err
	}
	if _, err := w.Appender.Append(ctx, tx, chain.Event{TenantID: r.TenantID, CorrelationID: r.CorrelationID, WrittenBy: emitter, Actor: emitter, EventType: "SLA_EVALUATED",
		ObjectRef: r.CorrelationID, Payload: r.Evaluation.EventPayload()}); err != nil {
		return fmt.Errorf("%sappend SLA_EVALUATED: %w", errPrefix, err)
	}
	return nil
}

func checkSLARequest(r SLARequest) error {
	ev := r.Evaluation
	switch {
	case !tenantRe.MatchString(r.TenantID), !correlationRe.MatchString(r.CorrelationID):
		return fmt.Errorf("%w: the tenant or the correlation is not in the contract's form", ErrRequest)
	case ev.Status.ClockID == "":
		return fmt.Errorf("%w: the evaluation names no clock", ErrRequest)
	case ev.Status.Label != contracts.ClockV1LabelSYNTHETIC && ev.Status.Label != contracts.ClockV1LabelVALIDATED:
		return fmt.Errorf("%w: the evaluation's clock has no label", ErrRequest)
	case ev.Status.Phase != clocks.NotStarted && ev.Status.Phase != clocks.Running && ev.Status.Phase != clocks.AtRisk && ev.Status.Phase != clocks.Breached:
		return fmt.Errorf("%w: the evaluation's phase is not a clock phase", ErrRequest)
	case ev.Priority != clocks.PriorityLow && ev.Priority != clocks.PriorityNormal && ev.Priority != clocks.PriorityHigh:
		return fmt.Errorf("%w: the evaluation has no routing priority", ErrRequest)
	case ev.OwnerQueue != "" && !ownerQueueRe.MatchString(ev.OwnerQueue):
		return fmt.Errorf("%w: the owner queue is not of the pack's shape", ErrRequest)
	case ev.Status.Elapsed < 0:
		return fmt.Errorf("%w: the elapsed time is negative", ErrRequest)
	case ev.Raised && ev.Priority == clocks.PriorityLow:
		return fmt.Errorf("%w: a priority raised to the lowest priority is not a raise", ErrRequest)
	}
	return checkSLATimes(ev.Status)
}

// checkSLATimes holds the payload's conditional rule (the shape of the SLA_EVALUATED payload schema, noema#984): a clock not started names no start, no deadline and no remaining
// time, and any other phase names all three; the remaining time is not negative. Nothing is assumed for a clock that has no anchor (INV-16).
func checkSLATimes(st clocks.Status) error {
	hasStart, hasDeadline, hasRemaining := st.StartedAt != nil, st.DeadlineAt != nil, st.Remaining != nil
	if st.Phase == clocks.NotStarted {
		if hasStart || hasDeadline || hasRemaining {
			return fmt.Errorf("%w: a clock that has not started names a start, a deadline or a remaining time", ErrRequest)
		}
		return nil
	}
	if !hasStart || !hasDeadline || !hasRemaining {
		return fmt.Errorf("%w: a started clock must name its start, its deadline and its remaining time", ErrRequest)
	}
	if *st.Remaining < 0 {
		return fmt.Errorf("%w: the remaining time is negative", ErrRequest)
	}
	return nil
}
