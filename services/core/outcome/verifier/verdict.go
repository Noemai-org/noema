package verifier

import (
	"fmt"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// The verdict document and its signature (OUT-01). Split from verifier.go, which decides; this file only builds and signs.

// store signs the verdict and stores it with everything that goes with it, in one call to the Store.
func (r *run) store(d decision) (Result, error) {
	// The column holds microseconds; the document carries the same instant, so the database can check they agree.
	verdictID, now := r.newID(idVerdict), r.v.cfg.Clock.Now().UTC().Truncate(time.Microsecond)
	reads := r.evidence
	if reads == nil {
		reads = []string{}
	}
	w := Write{Reads: r.reads, SignatureInvalid: d.badSig}
	if d.verdict != Achieved {
		w.RecoveryRequestID = r.newID(idRecovery)
	}
	doc := map[string]any{"schema_version": schemaVersion, "tenant_id": r.req.TenantID, "correlation_id": r.rc.CorrelationID, "verdict_id": verdictID,
		"receipt_id": r.rc.ReceiptID, "outcome_definition": r.req.Definition.OutcomeID, "evidence_reads": reads, "verdict": string(d.verdict),
		"verified_at": now.Format(TimestampLayout), "predicate_result": nil}
	if d.predicate != nil {
		doc["predicate_result"] = *d.predicate
	}
	if d.watch != nil {
		w.Watch = &WatchRow{WatchID: r.newID(idWatch), NextCheckAt: d.watch.NextCheckAt.UTC().Truncate(time.Microsecond), DeadlineAt: d.watch.DeadlineAt.UTC().Truncate(time.Microsecond), Triggers: append([]string{}, d.watch.Triggers...)}
		doc["watch"] = map[string]any{"watch_id": w.Watch.WatchID, "next_check_at": w.Watch.NextCheckAt.Format(TimestampLayout),
			"deadline_at": w.Watch.DeadlineAt.Format(TimestampLayout), "triggers": w.Watch.Triggers}
	}
	if r.err != nil {
		return Result{}, r.err
	}
	canonical, err := crypto.CanonicalJSON(doc)
	if err != nil {
		return Result{}, fmt.Errorf("verifier: canonical verdict: %w", err)
	}
	sig, err := r.v.cfg.Signer.Sign(canonical)
	if err != nil {
		return Result{}, fmt.Errorf("verifier: sign verdict: %w", err)
	}
	doc["sig"] = map[string]any{"key_id": sig.KeyID, "value": sig.Value}
	w.Verdict = VerdictDoc{VerdictID: verdictID, VerificationID: r.req.VerificationID, TenantID: r.req.TenantID, CorrelationID: r.rc.CorrelationID, ReceiptID: r.rc.ReceiptID,
		OutcomeID: r.req.Definition.OutcomeID, Verdict: d.verdict, Reason: d.reason, PredicateResult: d.predicate, EvidenceReads: reads,
		VerifiedAt: now, Sig: sig, Document: doc, ReceiptSignature: d.auth}
	if err := r.v.cfg.Store.RecordVerdict(r.ctx, w); err != nil {
		return Result{}, fmt.Errorf("verifier: store verdict %s: %w", verdictID, err)
	}
	return Result{VerdictID: verdictID, Verdict: d.verdict, Reason: d.reason}, nil
}
