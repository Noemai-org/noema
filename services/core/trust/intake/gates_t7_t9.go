package intake

import (
	"context"
	"errors"
	"sort"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// T7 to T9 are the first steps that may read about a target (PERD 23.3), each through its own port: T7 the scoped candidates, T8 the ownership answer, T9 the evidence
// snapshot's freshness. Everything they decide comes from the answers of those ports and from pack data the Candidates port carries; no number is held here (REQ-OPS-003).

// admit sorts the admitted intent into the resolved or the unresolved type (REQ-AMB-001, INV-06). An intent that is missing, or whose view is for another version than the one
// being evaluated, is a defect and fails the step.
func (e *evaluation) admit() (decide.Admission, error) {
	if e.intent == nil || e.intent.View.Version != e.version {
		return decide.Admission{}, ErrIntentView
	}
	return decide.Admit(e.intent.View), nil
}

// t7: material ambiguity. A resolved intent passes without reading anything. An unresolved one reads the candidates scoped to the session's principal and is clarified, or
// escalated when no question can be asked: none of the candidates reaches the pack's floor, more of them reach it than the pack allows in one question, or the question cannot
// be built (one candidate, labels that do not tell the options apart). Trust never resolves the slot itself (INV-06).
func (e *evaluation) t7(ctx context.Context) decide.StepResult {
	adm, err := e.admit()
	if err != nil {
		return decide.FailStep(err)
	}
	if _, ok := adm.Resolved(); ok {
		return decide.Continue()
	}
	u, ok := adm.Unresolved()
	if !ok {
		return decide.FailStep(ErrIntentView)
	}
	s := e.req.Session
	tc, err := e.svc.Ports.Candidates.ScopedCandidates(ctx, s.TenantID, s.PrincipalID, *e.intent)
	if err != nil {
		return decide.FailStep(err)
	}
	d := tc.Disambiguation
	if d.MaxCandidates < 1 || d.Floor < 0 || d.MaxDigitRun < 0 {
		return decide.FailStep(ErrBadDisambiguation)
	}
	offered, err := offerable(tc.Ranked, d.Floor)
	if err != nil {
		return decide.FailStep(err)
	}
	reason, err := decide.Core("AMBIGUOUS_COUNTERPARTY")
	if err != nil {
		return decide.FailStep(err)
	}
	if len(offered) == 0 || len(offered) > d.MaxCandidates {
		return stopWith(u.Escalate(reason))
	}
	q, err := decide.BuildClarification(offered, d.AttributeOrder, d.OptionsKey, decide.Limits{MaxOptions: d.MaxCandidates, MaxDigitRun: d.MaxDigitRun})
	if err != nil {
		return stopWith(u.Escalate(reason)) // one candidate, or labels that do not tell the options apart: a handoff, not a question
	}
	return stopWith(u.Clarify(q, reason))
}

// offerable returns the candidates at or above the floor, by score (highest first) and then by ID, so the order the port listed them in changes nothing (INV-21). A candidate
// with no ID, or an ID listed twice, is an unusable answer.
func offerable(ranked []RankedCandidate, floor int64) ([]decide.Candidate, error) {
	seen := map[string]bool{}
	var above []RankedCandidate
	for _, c := range ranked {
		if c.ID == "" || seen[c.ID] {
			return nil, ErrBadCandidates
		}
		seen[c.ID] = true
		if c.Score >= floor {
			above = append(above, c)
		}
	}
	sort.SliceStable(above, func(i, j int) bool {
		if above[i].Score != above[j].Score {
			return above[i].Score > above[j].Score
		}
		return above[i].ID < above[j].ID
	})
	out := make([]decide.Candidate, 0, len(above))
	for _, c := range above {
		out = append(out, c.Candidate)
	}
	return out, nil
}

// t8: the session's principal owns the resolved target. A target the principal does not own and a target that does not exist give the same decision, with the same one port
// call and the same trace, so that nothing about the target's existence can be read from the answer (REQ-TRU-003, PERD 23.3).
func (e *evaluation) t8(ctx context.Context) decide.StepResult {
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	s := e.req.Session
	owned, err := e.svc.Ports.Ownership.OwnsTarget(ctx, s.TenantID, s.PrincipalID, *e.intent)
	if errors.Is(err, ErrNotFound) {
		owned, err = false, nil
	}
	if err != nil {
		return decide.FailStep(err)
	}
	if owned {
		return decide.Continue()
	}
	reason, err := decide.Core("NOT_FOUND_OR_NOT_AUTHORIZED")
	if err != nil {
		return decide.FailStep(err)
	}
	return stopWith(r.Reject(decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.Fail}, reason))
}

// resolved is the admitted intent as a ResolvedIntent: after T7 an unresolved intent cannot get here, and one that does is a defect.
func (e *evaluation) resolved() (decide.ResolvedIntent, error) {
	adm, err := e.admit()
	if err != nil {
		return decide.ResolvedIntent{}, err
	}
	r, ok := adm.Resolved()
	if !ok {
		return decide.ResolvedIntent{}, ErrIntentView
	}
	return r, nil
}

// t9: the evidence the decision rests on. A source the orchestrator or the evidence port reports unavailable is ESCALATE INSUFFICIENT_EVIDENCE; an item past its effective
// limit, which the Evidence Layer could not refresh, is ESCALATE STALE_EVIDENCE; unavailable wins over stale. Otherwise the snapshot and the freshness of each item, as the
// evidence port reported them and never as a caller supplied them, are recorded once in the evaluation's basis (REQ-CTX-003, REQ-TRU-001).
func (e *evaluation) t9(ctx context.Context) decide.StepResult {
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	if e.basis == nil {
		return decide.FailStep(ErrNoBasis)
	}
	if e.req.EvidenceUnavailable {
		return e.escalateEvidence(r, "INSUFFICIENT_EVIDENCE")
	}
	if e.req.EvidenceSnapshotID == "" {
		return decide.FailStep(ErrNoSnapshot)
	}
	ev, err := e.svc.Ports.Evidence.SnapshotFreshness(ctx, e.req.Session.TenantID, e.req.EvidenceSnapshotID)
	if err != nil {
		return decide.FailStep(err)
	}
	unavailable, stale, err := classify(ev)
	switch {
	case err != nil:
		return decide.FailStep(err)
	case unavailable:
		return e.escalateEvidence(r, "INSUFFICIENT_EVIDENCE")
	case stale:
		return e.escalateEvidence(r, "STALE_EVIDENCE")
	}
	items := make([]decide.Freshness, 0, len(ev.Items))
	for _, it := range ev.Items {
		items = append(items, it.Freshness)
	}
	if err := e.basis.RecordEvidence(e.req.EvidenceSnapshotID, items); err != nil {
		return decide.FailStep(err)
	}
	if _, err := e.basis.Basis(); err != nil {
		return decide.FailStep(ErrBadEvidence) // what was recorded is not a set the Decision Record can hold
	}
	return decide.Continue()
}

// classify reads the evidence report: whether a required source is unavailable, whether an item is past its limit, or an error for a status the contract does not know. Facts
// that are known, inferred, unknown, unauthorized, ambiguous or contradictory pass: reconciliation and policy decide on them (T10, T11).
func classify(ev SnapshotEvidence) (unavailable, stale bool, err error) {
	unavailable = ev.RequiredUnavailable
	for _, it := range ev.Items {
		switch it.Status {
		case contracts.EnumsV1FactStatusUNAVAILABLE:
			unavailable = true
		case contracts.EnumsV1FactStatusSTALE:
			stale = true
		case contracts.EnumsV1FactStatusKNOWN, contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusAMBIGUOUS, contracts.EnumsV1FactStatusCONTRADICTORY,
			contracts.EnumsV1FactStatusUNKNOWN, contracts.EnumsV1FactStatusUNAUTHORIZED:
		default:
			return false, false, ErrBadEvidence
		}
		if it.Freshness.AgeSeconds > it.Freshness.EffectiveLimitSeconds {
			stale = true
		}
	}
	return unavailable, stale, nil
}

// escalateEvidence is the one ESCALATE of T9. Ownership passed at T8 but the business layer is not finished, so only the technical layer is PASS.
func (e *evaluation) escalateEvidence(r decide.ResolvedIntent, code string) decide.StepResult {
	reason, err := decide.Core(code)
	if err != nil {
		return decide.FailStep(err)
	}
	return stopWith(r.Escalate(decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.NotEvaluated}, reason))
}

// stopWith ends the evaluation with d, or fails the step when the decision could not be built.
func stopWith(d decide.Decision, err error) decide.StepResult {
	if err != nil {
		return decide.FailStep(err)
	}
	return decide.Stop(d)
}
