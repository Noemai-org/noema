package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
)

const (
	errPrefix  = "recorder: "
	schemaV    = "2.0"
	emitter    = "core-trust"
	timeLayout = "2006-01-02T15:04:05.000Z"
)

// Errors of Write. Callers compare with errors.Is.
var (
	// ErrRequest is returned for a request with a malformed or missing field.
	ErrRequest = errors.New(errPrefix + "the request is not valid")
	// ErrGrantsNeedAllow is returned when steps are given for a decision that is not an ALLOW (INV-25: a grant comes only from an ALLOW).
	ErrGrantsNeedAllow = errors.New(errPrefix + "grants are issued only for an ALLOW decision")
	// ErrNoEvaluatedBasis is returned for a decision that has no complete evaluated basis (no evidence snapshot or no policy). decision-record.v2 requires both.
	ErrNoEvaluatedBasis = errors.New(errPrefix + "the decision has no evaluated evidence and policy basis, which the record requires")
	// ErrUnsupported is returned for an outcome the writer does not map yet (CLARIFY).
	ErrUnsupported = errors.New(errPrefix + "this outcome is not written yet")
)

// IDs makes the decision ID.
type IDs interface {
	ID(prefix string) (string, error)
}

// Writer writes Decision Records and grants.
type Writer struct {
	Keys           *grants.Keys
	Issuer         *grants.Issuer
	Appender       *chain.Appender
	Clock          clock.Clock
	IDs            IDs
	RecordLifetime time.Duration // how long a record is usable after issued_at (ruling A13); from configuration, never a literal here
}

// Request is one decision to write. The identifiers come from the admitted intent and the session, the basis from the evaluation, nothing from the caller's
// own claim about what was evaluated (TRU-13).
type Request struct {
	TenantID              string
	CorrelationID         string
	IntentID              string
	IntentHash            contracts.CommonV1Hash
	DispositionDecisionID string // an action decision names its ESCALATE disposition (REQ-TRU-010); empty otherwise
	PolicyID              string // which policy the record and the grants bind when the basis names several; may be empty with one
	Decision              decide.Decision
	Evaluated             *decide.BasisBuilder
	Steps                 []grants.Step // one grant per write plan step, ALLOW only; tenant, correlation, decision, intent hash and policy are set by the writer
}

// Result is what was written.
type Result struct {
	Record contracts.DecisionRecordV2
	Grants []contracts.DecisionGrantV1
}

var (
	tenantRe      = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	correlationRe = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	intentRe      = regexp.MustCompile(`^int_[0-9A-Za-z]{2,32}$`)
	hashRe        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	snapshotRe    = regexp.MustCompile(`^evs_[0-9A-Za-z]{2,32}$`)
	packVersionRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*@[0-9]+\.[0-9]+(\.[0-9]+)?$`)
	packRe        = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
	decisionRe    = regexp.MustCompile(`^dec_[0-9A-Za-z]{2,32}$`)
)

// Write checks the request, then in tx: appends DECISION_MADE, inserts the signed record, and for each step issues, inserts and records a grant. It never
// commits or rolls back; on any error the caller must roll back.
func (w *Writer) Write(ctx context.Context, tx pgx.Tx, r Request) (Result, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Result{}, errors.New(errPrefix + "the context needs a deadline")
	}
	if w.Keys == nil || w.Issuer == nil || w.Appender == nil || w.Clock == nil || w.IDs == nil || w.RecordLifetime <= 0 {
		return Result{}, fmt.Errorf("%swriter is not configured", errPrefix)
	}
	d := r.Decision
	basis, policy, err := w.check(r)
	if err != nil {
		return Result{}, err
	}
	decisionID, err := w.IDs.ID("dec")
	if err != nil || !decisionRe.MatchString(decisionID) {
		return Result{}, fmt.Errorf("%w: decision id: %v", ErrRequest, err)
	}
	now := w.Clock.Now().UTC()
	rec := contracts.DecisionRecordV2{
		SchemaVersion: schemaV, TenantId: contracts.CommonV1TenantId(r.TenantID), CorrelationId: contracts.CommonV1CorrelationId(r.CorrelationID),
		DecisionId: contracts.CommonV1DecisionId(decisionID), IntentId: contracts.CommonV1IntentId(r.IntentID), IntentHash: r.IntentHash,
		Outcome: contracts.EnumsV1DecisionOutcome(d.Outcome()), ReasonCodes: reasonCodes(d),
		Authority: contracts.DecisionRecordV2Authority{Technical: contracts.EnumsV1AuthorityResult(d.Authority().Technical), Policy: contracts.EnumsV1AuthorityResult(d.Authority().Policy),
			Business: contracts.EnumsV1AuthorityResult(d.Authority().Business)},
		EvidenceSnapshotId: contracts.CommonV1SnapshotId(basis.EvidenceSnapshotID), PolicyVersion: policy, PackVersion: basis.PackVersion,
		EvidenceFreshness: freshness(basis), EvaluatedAt: stamp(time.Unix(basis.EvaluatedAt, 0)), IssuedAt: stamp(now), ExpiresAt: stamp(now.Add(w.RecordLifetime)),
	}
	if r.DispositionDecisionID != "" {
		rec.DispositionDecisionId = contracts.Opt[contracts.CommonV1DecisionId]{Set: true, Value: contracts.CommonV1DecisionId(r.DispositionDecisionID)}
	}
	if d.Outcome() == decide.Wait {
		at := stamp(time.Unix(d.ReevaluateAt(), 0))
		rec.ReevaluateAt = &at
	}
	if rec.Sig, err = w.Keys.SignRecord(rec); err != nil {
		return Result{}, fmt.Errorf("%ssign: %w", errPrefix, err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return Result{}, fmt.Errorf("%smarshal: %w", errPrefix, err)
	}
	var checked contracts.DecisionRecordV2
	if err := checked.UnmarshalJSON(raw); err != nil {
		return Result{}, fmt.Errorf("%w: the record does not satisfy decision-record.v2: %w", ErrRequest, err)
	}
	versions, err := json.Marshal(policyVersions(basis))
	if err != nil {
		return Result{}, err
	}
	actor := emitter // no authenticated person causes a decision event; a person's identity comes only from the session, never from a request field (INV-12)
	if _, err := w.Appender.Append(ctx, tx, chain.Event{TenantID: r.TenantID, CorrelationID: r.CorrelationID, WrittenBy: emitter, Actor: actor, EventType: "DECISION_MADE", ObjectRef: decisionID,
		Payload: map[string]any{"decision_id": decisionID, "decision_outcome": string(d.Outcome()), "reason_codes": reasonStrings(d), "policy_id": r.PolicyID,
			"policy_version": policy, "sig_key_id": rec.Sig.KeyId}}); err != nil {
		return Result{}, fmt.Errorf("%sappend DECISION_MADE: %w", errPrefix, err)
	}
	var disposition *string
	if r.DispositionDecisionID != "" {
		disposition = &r.DispositionDecisionID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, disposition_decision_id, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7::jsonb, $8, $9)`,
		decisionID, r.TenantID, string(raw), string(d.Outcome()), disposition, string(r.IntentHash), string(versions), rec.Sig.KeyId, rec.Sig.Value); err != nil {
		return Result{}, fmt.Errorf("%sinsert decision record: %w", errPrefix, err)
	}
	res := Result{Record: rec}
	for i, s := range r.Steps {
		s.TenantID, s.CorrelationID, s.DecisionID, s.IntentHash, s.PolicyID = r.TenantID, r.CorrelationID, decisionID, r.IntentHash, r.PolicyID
		g, err := w.Issuer.Issue(d, s)
		if err != nil {
			return Result{}, fmt.Errorf("%sgrant %d: %w", errPrefix, i, err)
		}
		graw, err := json.Marshal(g)
		if err != nil {
			return Result{}, fmt.Errorf("%smarshal grant: %w", errPrefix, err)
		}
		expires, err := time.Parse(time.RFC3339, string(g.ExpiresAt))
		if err != nil {
			return Result{}, fmt.Errorf("%sgrant expiry: %w", errPrefix, err)
		}
		id, version, _ := strings.Cut(g.PolicyVersion, "@")
		if _, err := tx.Exec(ctx, `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10)`,
			string(g.GrantId), r.TenantID, decisionID, g.Nonce, string(graw), id, version, expires, g.Sig.KeyId, g.Sig.Value); err != nil {
			return Result{}, fmt.Errorf("%sinsert grant %d: %w", errPrefix, i, err)
		}
		if _, err := w.Appender.Append(ctx, tx, chain.Event{TenantID: r.TenantID, CorrelationID: r.CorrelationID, WrittenBy: emitter, Actor: actor, EventType: "GRANT_ISSUED", ObjectRef: string(g.GrantId),
			Payload: map[string]any{"grant_id": string(g.GrantId), "decision_id": decisionID, "capability_id": string(g.CapabilityId), "plan_step": g.PlanStep,
				"expires_at": string(g.ExpiresAt)}}); err != nil {
			return Result{}, fmt.Errorf("%sappend GRANT_ISSUED %d: %w", errPrefix, i, err)
		}
		res.Grants = append(res.Grants, g)
	}
	return res, nil
}

// check refuses what cannot be honoured before anything is written, and returns the evaluated basis and the bound policy.
func (w *Writer) check(r Request) (decide.Basis, string, error) {
	switch {
	case !tenantRe.MatchString(r.TenantID), !correlationRe.MatchString(r.CorrelationID), !intentRe.MatchString(r.IntentID), !hashRe.MatchString(string(r.IntentHash)),
		r.Evaluated == nil, r.DispositionDecisionID != "" && !decisionRe.MatchString(r.DispositionDecisionID):
		return decide.Basis{}, "", ErrRequest
	}
	d := r.Decision
	if d.Outcome() == "" {
		return decide.Basis{}, "", ErrRequest
	}
	if d.Outcome() == decide.Clarify {
		return decide.Basis{}, "", ErrUnsupported
	}
	if len(r.Steps) > 0 && d.Outcome() != decide.Allow {
		return decide.Basis{}, "", ErrGrantsNeedAllow
	}
	if !d.HasCompleteBasis() {
		return decide.Basis{}, "", ErrNoEvaluatedBasis
	}
	if err := r.Evaluated.Check(d); err != nil {
		return decide.Basis{}, "", err
	}
	basis := d.Basis()
	if !snapshotRe.MatchString(basis.EvidenceSnapshotID) {
		return decide.Basis{}, "", fmt.Errorf("%w: the evidence snapshot ID does not match the contract", ErrRequest)
	}
	if !packVersionRe.MatchString(basis.PackVersion) {
		return decide.Basis{}, "", fmt.Errorf("%w: the pack version does not match the contract", ErrRequest)
	}
	for _, f := range basis.EvidenceFreshness {
		if !packRe.MatchString(f.Source) {
			return decide.Basis{}, "", fmt.Errorf("%w: an evidence source does not match the contract", ErrRequest)
		}
	}
	policy, ok := grants.BoundPolicy(basis, r.PolicyID)
	if !ok {
		return decide.Basis{}, "", fmt.Errorf("%w: no single policy is bound: name one policy the basis holds", ErrRequest)
	}
	return basis, policy, nil
}

func stamp(t time.Time) contracts.CommonV1Timestamp {
	return contracts.CommonV1Timestamp(t.UTC().Format(timeLayout))
}

func reasonStrings(d decide.Decision) []string {
	out := []string{}
	for _, c := range d.Reasons() {
		out = append(out, c.String())
	}
	return out
}

func reasonCodes(d decide.Decision) []contracts.DecisionRecordV2ReasonCodesItem {
	out := []contracts.DecisionRecordV2ReasonCodesItem{}
	for _, c := range d.Reasons() {
		out = append(out, contracts.DecisionRecordV2ReasonCodesItem(c.String()))
	}
	return out
}

func freshness(b decide.Basis) []contracts.DecisionRecordV2EvidenceFreshnessItem {
	out := []contracts.DecisionRecordV2EvidenceFreshnessItem{}
	for _, f := range b.EvidenceFreshness {
		out = append(out, contracts.DecisionRecordV2EvidenceFreshnessItem{Source: contracts.CommonV1PackId(f.Source), AgeSeconds: f.AgeSeconds,
			EffectiveLimitSeconds: contracts.CommonV1PositiveInt(f.EffectiveLimitSeconds), LimitSetBy: contracts.EnumsV1LimitSetBy(f.LimitSetBy)})
	}
	return out
}

type ruleVersion struct {
	RuleID   string `json:"rule_id"`
	PolicyID string `json:"policy_id"`
	Version  string `json:"version"`
}

func policyVersions(b decide.Basis) []ruleVersion {
	out := []ruleVersion{}
	for _, r := range b.PolicyRules {
		out = append(out, ruleVersion{r.RuleID, r.PolicyID, r.Version})
	}
	return out
}
