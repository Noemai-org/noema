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
	"github.com/jackc/pgx/v5/pgconn"

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

	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgIntegrityViolation  = "23000"                                 // integrity_constraint_violation: the code the confirmation_request chain trigger raises (migration 0032)
	dispositionKey        = "decision_record_disposition_fkey"      // migration 0012: (tenant, disposition) must be an ESCALATE decision of the same tenant
	confirmationEventKey  = "confirmation_spend_event_key"          // migration 0028: UNIQUE (tenant_id, confirmation_event_id)
	confirmationValid     = "confirmation_spend_confirmation_valid" // migration 0028: the trigger that refuses a confirmation that is unknown, expired or given for another action
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
	// ErrDecisionConflict is returned when a decision is already stored for the same input tuple and the new evaluation disagrees with it (INV-21: the same input
	// gives the same decision). Nothing is written and nothing of either decision is returned; the error carries no content of the decision.
	ErrDecisionConflict = errors.New(errPrefix + "the stored decision for this input does not agree with the new evaluation")
	// ErrConfirmationNotValid is returned when the confirmation an ALLOW names does not exist for the tenant, has expired by the database's clock, or was given for another action than
	// the one the ALLOW's step carries (the eight values of SoT 43.2a). The caller cannot tell which: the three are one answer, so no outward answer tells an attacker which.
	ErrConfirmationNotValid = errors.New(errPrefix + "the confirmation is not valid for this action")
	// ErrDeclinedRequestNotValid is returned when the declined request a REJECT DECLINED_BY_APPROVER names is not a confirmation request of this tenant and of this decision's intent (A507).
	// The caller cannot tell which: an absent request, another tenant's and another intent's are one answer.
	ErrDeclinedRequestNotValid = errors.New(errPrefix + "the declined request is not valid for this decision")
	// ErrConfirmationPlanMismatch is returned when the confirmation an ALLOW follows was given for another plan than the one its steps carry (A230).
	ErrConfirmationPlanMismatch = errors.New(errPrefix + "the confirmation was given for another plan")
	// ErrWritesDisabled is returned when the tenant's kill switch is on and the decision would issue grants (REQ-EXE-008, REQ-OPS-002; the contract's WRITES_DISABLED, errors.v3).
	// Nothing is written: no decision record, no grant, no event. A decision with no write step is not refused.
	ErrWritesDisabled = errors.New(errPrefix + "the tenant's kill switch is on: no grant is issued")
	// ErrConfirmationSpent is returned when the confirmation event the request names has already been spent by another decision of the tenant (INV-17: a given
	// confirmation is used once). Nothing is written; the caller rolls back.
	ErrConfirmationSpent = errors.New(errPrefix + "the confirmation was already spent by another decision")
	// ErrConfirmationRequestOpen is returned when the action (intent, capability, target) of a CONFIRM already has a request that is neither answered nor expired on the database clock
	// (A552): a fresh request may supersede only a closed one. Nothing was written.
	ErrConfirmationRequestOpen = errors.New(errPrefix + "the action already has an open confirmation request")
	// ErrDispositionNotValid is returned when a decision names a disposition that is not an existing ESCALATE decision of the same tenant (REQ-TRU-010). The caller cannot tell
	// which of the three it is: a missing decision, another tenant's and one with another outcome are one answer. Nothing is written.
	ErrDispositionNotValid = errors.New(errPrefix + "the disposition is not an existing escalation decision of the tenant")
	// ErrStepOutsideEscalation is returned when a decision that names its ESCALATE disposition carries a step whose capability is not one of the pack's escalation plan (REQ-TRU-010,
	// INV-25): an escalation disposition authorises the escalation plan's write steps and nothing else. Nothing is written.
	ErrStepOutsideEscalation = errors.New(errPrefix + "a step is not a capability of the escalation plan")
)

// EscalationSet is the capability IDs of the pack's escalation plan: the only writes an ESCALATE disposition authorises (REQ-TRU-010). The composition root builds it from pack
// data; this package holds no capability ID of its own (INV-14, SoT 51). The zero value is the empty set, under which no step is allowed after a disposition.
type EscalationSet struct{ ids map[string]struct{} }

// NewEscalationSet builds a set from capability IDs.
func NewEscalationSet(ids ...string) EscalationSet {
	s := EscalationSet{ids: make(map[string]struct{}, len(ids))}
	for _, id := range ids {
		s.ids[id] = struct{}{}
	}
	return s
}

// Contains reports whether the capability is one of the escalation plan.
func (s EscalationSet) Contains(capabilityID string) bool {
	_, ok := s.ids[capabilityID]
	return ok
}

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
	Escalation     EscalationSet // the pack's escalation plan capabilities, from the composition root (REQ-TRU-010); empty means no step may follow a disposition
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
	ConfirmationPlanHash  contracts.CommonV1Hash // the plan hash the confirmation was given for (A230); the writer refuses an ALLOW whose steps carry another
	ConfirmationEventID   string                 // the given confirmation an ALLOW follows (cfe_...); empty when none was required. The writer spends it once, in this transaction
	Steps                 []grants.Step          // one grant per write plan step, ALLOW only; tenant, correlation, decision, intent hash and policy are set by the writer
	// ConfirmationRequest is the request a CONFIRM decision creates, written with the decision in one transaction (EC-E16); nil for a CONFIRM that names an already open request,
	// and for every other outcome (any other outcome that carries one is ErrRequest).
	ConfirmationRequest *NewConfirmationRequest
	// DeclinedRequestID is the confirmation request (cfr_...) a REJECT DECLINED_BY_APPROVER rests on; empty for every other decision, and any other outcome that names one is ErrRequest
	// (A366). With a confirmation an ALLOW follows, it is the confirmation state of the decision, which the single-decision key of the input tuple includes.
	DeclinedRequestID string
	// ConfirmationBindingEvidenceHash is the evidence component of the binding of the confirmation an ALLOW follows (A435): the content digest of the evidence the decision used, which the
	// writer needs to recompute that binding (the contract's field is still named evidence_set_hash). It is NOT the grant's evidence_set_hash: a step's EvidenceSetHash stays the snapshot's
	// set hash, and neither the grant nor the Decision Record carries this value. Required, in the Hash form, for an ALLOW that follows a confirmation; named with no confirmation it is ErrRequest.
	ConfirmationBindingEvidenceHash contracts.CommonV1Hash
	// TurnID and PrincipalID are the key of an early decision (A581, A590; interim, relayed): the client's request ID of the turn (a lower-case UUID) and the verified principal of the
	// session (TurnPrincipalID). They are required for an ESCALATE reached before the evidence and the policy were evaluated (an incomplete basis, recorded with the three basis members null, A574, A577,
	// A663) and named by no other decision: any other request that names one is ErrRequest.
	TurnID          string
	TurnPrincipalID string // the verified principal of the turn, a key member: never an actor of the Record (INV-12; the events are written as core-trust)
}

// Result is what was written.
type Result struct {
	Record contracts.DecisionRecordV2
	Grants []contracts.DecisionGrantV1
	// ConfirmationRequestID is the cfr_ ID the writer minted for Request.ConfirmationRequest; empty when none was asked for, and empty when a stored decision of the same input was
	// returned (A186): that decision's request, if it created one, is in the Record.
	ConfirmationRequestID string
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
	confirmRe     = regexp.MustCompile(`^cfe_[0-9A-Za-z]{2,32}$`)
	turnRe        = regexp.MustCompile(`\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z`) // the client's request ID of the turn: a lower-case UUID
	principalRe   = regexp.MustCompile(`\Ausr_[0-9A-Za-z]{2,32}\z`)
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
	basis, policy, early, err := w.check(r)
	if err != nil {
		return Result{}, err
	}
	// A confirmation an ALLOW follows is bound to the action of the ALLOW's own step (SoT 43.2a): the binding is computed here, from the step, never taken from the caller; the database refuses
	// the spend unless the confirmation was given for exactly this binding.
	var binding string
	if d.Outcome() == decide.Allow && r.ConfirmationEventID != "" {
		if binding, err = bindingOf(r, basis); err != nil {
			return Result{}, fmt.Errorf("%w: the confirmation's binding cannot be computed: %v", ErrRequest, err)
		}
	}
	// A186: one decision per full input tuple. Writers of one input are serialised, the stored decision (if any) is read, and only a tuple with none is written.
	key := inputKey{tenant: r.TenantID, intentID: r.IntentID, intentHash: string(r.IntentHash), snapshot: basis.EvidenceSnapshotID, policy: policy}
	if early {
		// A581, A590: an early decision holds no snapshot and no policy, and is keyed by the turn and the verified principal instead (migration 0040).
		key = inputKey{tenant: r.TenantID, intentID: r.IntentID, intentHash: string(r.IntentHash), turn: r.TurnID, principal: r.TurnPrincipalID}
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key.lockName()); err != nil {
		return Result{}, fmt.Errorf("%slock the input: %w", errPrefix, err)
	}
	// REQ-EXE-008, REQ-OPS-002: while the tenant's kill switch is on, no grant is issued. The switch is read here, on the caller's transaction (Write holds no other connection), at the
	// write itself and before the first statement that writes, so a refusal leaves nothing behind. It is the tenant of the request, which the database ties to the login, and only a
	// decision with steps is refused: a read, an explanation or a refusal has no grant and records as before. A switch that cannot be read is an error, never "off".
	if len(r.Steps) > 0 {
		var killed bool
		if err := tx.QueryRow(ctx, `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, r.TenantID).Scan(&killed); err != nil {
			return Result{}, fmt.Errorf("%sread the kill switch: %w", errPrefix, err)
		}
		if killed {
			return Result{}, ErrWritesDisabled
		}
	}
	state, err := resolveState(ctx, tx, r)
	if errors.Is(err, errUnresolved) {
		// a confirmation that is not recorded for the tenant: the one answer the spend gives, unless the input already has an ALLOW, which a retry naming it contradicts (A233, INV-21)
		if err := w.otherState(ctx, tx, key, d.Outcome(), noState); err != nil {
			return Result{}, err
		}
		return Result{}, ErrConfirmationNotValid
	}
	if errors.Is(err, errDeclinedUnowned) {
		return Result{}, ErrDeclinedRequestNotValid
	}
	if err != nil {
		return Result{}, err
	}
	stored, found, err := w.lookup(ctx, tx, key, state, r, basis)
	if err != nil {
		return Result{}, err
	}
	if found {
		return stored, nil
	}
	if r.DispositionDecisionID != "" {
		if err := requireEscalationDisposition(ctx, tx, r); err != nil {
			return Result{}, err
		}
	}
	decisionID, err := w.IDs.ID("dec")
	if err != nil || !decisionRe.MatchString(decisionID) {
		return Result{}, fmt.Errorf("%w: decision id: %v", ErrRequest, err)
	}
	// A CONFIRM that creates a request: Trust mints the ID and computes the binding hash here, never from the caller (SoT 43.2a). This is the new-decision path only, so a stored
	// decision returned above creates no second request.
	var requestID, requestBinding string
	if r.ConfirmationRequest != nil {
		if requestID, err = w.IDs.ID("cfr"); err != nil || !requestRe.MatchString(requestID) {
			return Result{}, fmt.Errorf("%w: confirmation request id: %v", ErrRequest, err)
		}
		if requestBinding, err = bindingOfRequest(r.ConfirmationRequest); err != nil {
			return Result{}, fmt.Errorf("%w: the binding cannot be computed: %v", ErrRequest, err)
		}
	}
	now := w.Clock.Now().UTC()
	// The Writer records a decision that evaluated its basis (A578: an early outcome is not written here yet), so the three members are set; they are pointers because decision-record.v2 lets
	// an early outcome hold null (A574, A577).
	snapshotID, freshEvidence := contracts.CommonV1SnapshotId(basis.EvidenceSnapshotID), freshness(basis)
	basisSnapshot, basisPolicy, basisFreshness := &snapshotID, &policy, &freshEvidence
	if early {
		basisSnapshot, basisPolicy, basisFreshness = nil, nil, nil // the three required keys hold null: an outcome reached before the evaluation claims none (A574, A577)
	}
	rec := contracts.DecisionRecordV2{
		SchemaVersion: schemaV, TenantId: contracts.CommonV1TenantId(r.TenantID), CorrelationId: contracts.CommonV1CorrelationId(r.CorrelationID),
		DecisionId: contracts.CommonV1DecisionId(decisionID), IntentId: contracts.CommonV1IntentId(r.IntentID), IntentHash: r.IntentHash,
		Outcome: contracts.EnumsV1DecisionOutcome(d.Outcome()), ReasonCodes: reasonCodes(d),
		Authority: contracts.DecisionRecordV2Authority{Technical: contracts.EnumsV1AuthorityResult(d.Authority().Technical), Policy: contracts.EnumsV1AuthorityResult(d.Authority().Policy),
			Business: contracts.EnumsV1AuthorityResult(d.Authority().Business)},
		EvidenceSnapshotId: basisSnapshot, PolicyVersion: basisPolicy, PackVersion: basis.PackVersion,
		EvidenceFreshness: basisFreshness, EvaluatedAt: stamp(time.Unix(basis.EvaluatedAt, 0)), IssuedAt: stamp(now), ExpiresAt: stamp(now.Add(w.RecordLifetime)),
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
	decisionMade := map[string]any{"decision_id": decisionID, "decision_outcome": string(d.Outcome()), "reason_codes": reasonStrings(d), "sig_key_id": rec.Sig.KeyId}
	if !early { // an early decision binds no policy (A574)
		decisionMade["policy_id"], decisionMade["policy_version"] = r.PolicyID, policy
	}
	if r.DispositionDecisionID != "" {
		decisionMade["disposition_decision_id"] = r.DispositionDecisionID // the ESCALATE disposition this decision links (A503): the Record shows the link without the decision table
	}
	if binding != "" {
		// PERD 32.2 has no spend event type, so the spend is shown by the two events of the same transaction: the decision names the confirmation it spent and the binding it was spent against,
		// and each grant names the event it follows. A decision with no confirmation carries neither member.
		decisionMade["confirmation_event_id"], decisionMade["confirmation_binding_hash"] = r.ConfirmationEventID, binding
	}
	if requestID != "" {
		decisionMade["confirmation_request_id"] = requestID
	} else if state != "" {
		decisionMade["confirmation_request_id"] = state // the request this decision rests on (A366): an ALLOW's spent confirmation resolved it, a REJECT's was declined
	}
	if _, err := w.Appender.Append(ctx, tx, chain.Event{TenantID: r.TenantID, CorrelationID: r.CorrelationID, WrittenBy: emitter, Actor: actor, EventType: "DECISION_MADE", ObjectRef: decisionID,
		Payload: decisionMade}); err != nil {
		return Result{}, fmt.Errorf("%sappend DECISION_MADE: %w", errPrefix, err)
	}
	var disposition *string
	if r.DispositionDecisionID != "" {
		disposition = &r.DispositionDecisionID
	}
	var rests *string // the confirmation state of the key (A366, migration 0034): NULL for a decision that rests on no confirmation
	if state != "" {
		rests = &state
	}
	var turn, principal *string // the key of an early decision (A581, A590; migration 0040): NULL for every other decision
	if early {
		turn, principal = &r.TurnID, &r.TurnPrincipalID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, disposition_decision_id, intent_hash, policy_versions_json, sig_key_id, sig_value, confirmation_request_id, turn_id, principal_id)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12)`,
		decisionID, r.TenantID, string(raw), string(d.Outcome()), disposition, string(r.IntentHash), string(versions), rec.Sig.KeyId, rec.Sig.Value, rests, turn, principal); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == pgForeignKeyViolation && pg.ConstraintName == dispositionKey {
			return Result{}, ErrDispositionNotValid // the database's own refusal, behind the check above
		}
		return Result{}, fmt.Errorf("%sinsert decision record: %w", errPrefix, err)
	}
	// The confirmation request of a CONFIRM is a row and an event of the same transaction, right after the decision (record first, EC-E16): both commit with the decision or not at all
	// (A552). The row is written first so that the event carries the expiry the row has, which is the database's clock plus the TTL.
	if requestID != "" {
		expires, err := w.insertRequest(ctx, tx, r, requestID, requestBinding, d.IntentVersion())
		if err != nil {
			return Result{}, err
		}
		if _, err := w.Appender.Append(ctx, tx, chain.Event{TenantID: r.TenantID, CorrelationID: r.CorrelationID, WrittenBy: emitter, Actor: actor, EventType: "CONFIRMATION_REQUESTED",
			ObjectRef: requestID, Payload: confirmationRequested(r.ConfirmationRequest, requestID, decisionID, requestBinding, expires)}); err != nil {
			return Result{}, fmt.Errorf("%sappend CONFIRMATION_REQUESTED: %w", errPrefix, err)
		}
	}
	// A given confirmation is spent once, by the ALLOW it authorised, in this transaction. The decision row above is newly written (a stored one was returned
	// earlier), so a retry never reaches this insert. The unique key is the single-use rule; a second spend is the typed error and the caller rolls back.
	if binding != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO confirmation_spend (tenant_id, confirmation_event_id, decision_id, plan_hash, binding_hash) VALUES ($1, $2, $3, $4, $5)`,
			r.TenantID, r.ConfirmationEventID, decisionID, string(r.ConfirmationPlanHash), binding); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == pgUniqueViolation && pg.ConstraintName == confirmationEventKey {
				return Result{}, ErrConfirmationSpent
			}
			if errors.As(err, &pg) && pg.ConstraintName == confirmationValid {
				return Result{}, ErrConfirmationNotValid
			}
			return Result{}, fmt.Errorf("%sspend the confirmation: %w", errPrefix, err)
		}
	}
	res := Result{Record: rec, ConfirmationRequestID: requestID}
	for i, s := range r.Steps {
		s.TenantID, s.CorrelationID, s.DecisionID, s.IntentHash, s.PolicyID = r.TenantID, r.CorrelationID, decisionID, r.IntentHash, r.PolicyID
		s.ConfirmationEventID = r.ConfirmationEventID // the grant follows the confirmation the decision spent; the caller's own claim on the step is not used
		g, err := w.Issuer.Issue(d, s)
		if err != nil {
			return Result{}, fmt.Errorf("%sgrant %d: %w", errPrefix, i, err)
		}
		graw, err := signedPart(g)
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
			Payload: grantIssued(g, decisionID, r.ConfirmationEventID)}); err != nil {
			return Result{}, fmt.Errorf("%sappend GRANT_ISSUED %d: %w", errPrefix, i, err)
		}
		res.Grants = append(res.Grants, g)
	}
	return res, nil
}

// signedPart is the grant document as it is stored in decision_grant.json: the document without its sig member. That is the part the signature covers and the form Execution
// decodes (controller.Grant, strictly, and rebuilds its canonical JSON from it). The signature itself is stored in sig_key_id and sig_value, and lookup puts it back.
func signedPart(g contracts.DecisionGrantV1) ([]byte, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	delete(doc, "sig")
	return json.Marshal(doc)
}

// check refuses what cannot be honoured before anything is written, and returns the evaluated basis, the bound policy and whether the decision is an early one (A663): an ESCALATE whose basis
// is incomplete, recorded with the three basis members null, no policy and the turn and the principal as its key. Every other decision needs a complete basis (INV-17).
func (w *Writer) check(r Request) (decide.Basis, string, bool, error) {
	switch {
	case !tenantRe.MatchString(r.TenantID), !correlationRe.MatchString(r.CorrelationID), !intentRe.MatchString(r.IntentID), !hashRe.MatchString(string(r.IntentHash)),
		r.Evaluated == nil, r.DispositionDecisionID != "" && !decisionRe.MatchString(r.DispositionDecisionID),
		r.ConfirmationEventID != "" && !confirmRe.MatchString(r.ConfirmationEventID),
		r.DeclinedRequestID != "" && (r.Decision.Outcome() != decide.Reject || !requestRe.MatchString(r.DeclinedRequestID)):
		return decide.Basis{}, "", false, ErrRequest
	}
	d := r.Decision
	if d.Outcome() == "" {
		return decide.Basis{}, "", false, ErrRequest
	}
	// A597, A600 (A542): a decline rests on the request it declined. A REJECT whose reason is DECLINED_BY_APPROVER that names none is refused here, before a statement is sent, so no decline
	// is stored with an empty state; every other REJECT may name none. The decision is known by its own reason codes, never by a flag of the caller.
	if d.Outcome() == decide.Reject && r.DeclinedRequestID == "" && declinedByApprover(d) {
		return decide.Basis{}, "", false, ErrRequest
	}
	if d.Outcome() == decide.Clarify {
		return decide.Basis{}, "", false, ErrUnsupported
	}
	if len(r.Steps) > 0 && d.Outcome() != decide.Allow {
		return decide.Basis{}, "", false, ErrGrantsNeedAllow
	}
	if r.DispositionDecisionID != "" {
		// An ESCALATE disposition authorises the escalation plan's write steps and nothing else (REQ-TRU-010, INV-25). The set is the pack's, injected; an empty set allows no step.
		for _, s := range r.Steps {
			if !w.Escalation.Contains(s.CapabilityID) {
				return decide.Basis{}, "", false, ErrStepOutsideEscalation
			}
		}
	}
	if r.ConfirmationPlanHash != "" && r.ConfirmationEventID == "" {
		return decide.Basis{}, "", false, fmt.Errorf("%w: a plan hash is named with no confirmation", ErrRequest)
	}
	if r.ConfirmationBindingEvidenceHash != "" && r.ConfirmationEventID == "" {
		return decide.Basis{}, "", false, fmt.Errorf("%w: the evidence value of a binding is named with no confirmation", ErrRequest)
	}
	if d.Outcome() == decide.Allow && r.ConfirmationEventID != "" && !planMatches(r) {
		return decide.Basis{}, "", false, ErrConfirmationPlanMismatch
	}
	// A435: the evidence component of the binding of the confirmation an ALLOW follows. It is needed, in the contract's form, exactly there; it is named nowhere else.
	if d.Outcome() == decide.Allow && r.ConfirmationEventID != "" && !hashRe.MatchString(string(r.ConfirmationBindingEvidenceHash)) {
		return decide.Basis{}, "", false, fmt.Errorf("%w: an ALLOW after a confirmation names no well-formed evidence value of its binding", ErrRequest)
	}
	if d.Outcome() == decide.Escalate && !d.HasCompleteBasis() {
		return w.checkEarly(r)
	}
	if !d.HasCompleteBasis() {
		return decide.Basis{}, "", false, ErrNoEvaluatedBasis
	}
	if r.TurnID != "" || r.TurnPrincipalID != "" {
		return decide.Basis{}, "", false, fmt.Errorf("%w: only an early ESCALATE names a turn and a principal", ErrRequest)
	}
	if err := r.Evaluated.Check(d); err != nil {
		return decide.Basis{}, "", false, err
	}
	basis := d.Basis()
	if !snapshotRe.MatchString(basis.EvidenceSnapshotID) {
		return decide.Basis{}, "", false, fmt.Errorf("%w: the evidence snapshot ID does not match the contract", ErrRequest)
	}
	if !packVersionRe.MatchString(basis.PackVersion) {
		return decide.Basis{}, "", false, fmt.Errorf("%w: the pack version does not match the contract", ErrRequest)
	}
	for _, f := range basis.EvidenceFreshness {
		if !packRe.MatchString(f.Source) {
			return decide.Basis{}, "", false, fmt.Errorf("%w: an evidence source does not match the contract", ErrRequest)
		}
	}
	policy, ok := grants.BoundPolicy(basis, r.PolicyID)
	if !ok {
		return decide.Basis{}, "", false, fmt.Errorf("%w: no single policy is bound: name one policy the basis holds", ErrRequest)
	}
	if r.ConfirmationRequest != nil {
		if err := w.checkRequest(r, basis); err != nil {
			return decide.Basis{}, "", false, err
		}
	}
	return basis, policy, false, nil
}

// checkEarly is check for an early decision (A663, A581, A590): an ESCALATE whose basis is incomplete. It needs the turn and the verified principal as its key, names no confirmation, no
// confirmation request and no disposition (it has no step and no grant), and its evaluated set must still be the one the decision holds (TRU-13). It binds no policy and holds no snapshot.
func (w *Writer) checkEarly(r Request) (decide.Basis, string, bool, error) {
	if !turnRe.MatchString(r.TurnID) || !principalRe.MatchString(r.TurnPrincipalID) {
		return decide.Basis{}, "", false, fmt.Errorf("%w: an early ESCALATE names the turn and the verified principal in their contract forms", ErrRequest)
	}
	if r.ConfirmationRequest != nil || r.ConfirmationEventID != "" || r.DispositionDecisionID != "" {
		return decide.Basis{}, "", false, fmt.Errorf("%w: an early ESCALATE names no confirmation and no disposition", ErrRequest)
	}
	if err := r.Evaluated.Check(r.Decision); err != nil {
		return decide.Basis{}, "", false, err
	}
	basis := r.Decision.Basis()
	if !packVersionRe.MatchString(basis.PackVersion) {
		return decide.Basis{}, "", false, fmt.Errorf("%w: the pack version does not match the contract", ErrRequest)
	}
	return basis, "", true, nil
}

// requireEscalationDisposition checks, before anything is written, that the disposition a decision names is an existing ESCALATE decision of the same tenant (REQ-TRU-010). A
// missing decision, another tenant's and one with another outcome are one answer. The database's foreign key (migration 0012) says the same behind it.
func requireEscalationDisposition(ctx context.Context, tx pgx.Tx, r Request) error {
	var outcome string
	err := tx.QueryRow(ctx, `SELECT outcome FROM decision_record WHERE tenant_id = $1 AND decision_id = $2`, r.TenantID, r.DispositionDecisionID).Scan(&outcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDispositionNotValid
	}
	if err != nil {
		return fmt.Errorf("%sread the disposition: %w", errPrefix, err)
	}
	if outcome != string(decide.Escalate) {
		return ErrDispositionNotValid
	}
	return nil
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

// planMatches is A230: an ALLOW that follows a confirmation is written only when the confirmation was given for the very plan the steps carry. The hash must be well formed, there must
// be exactly one step (a confirmation approves one action, SoT 43.2a) and its plan hash must equal it. Anything else is refused before anything is written.
func planMatches(r Request) bool {
	if !hashRe.MatchString(string(r.ConfirmationPlanHash)) || len(r.Steps) != 1 {
		return false
	}
	for _, s := range r.Steps {
		if s.PlanHash != r.ConfirmationPlanHash {
			return false
		}
	}
	return true
}

// grantIssued is the payload of GRANT_ISSUED: the grant, its decision, its step and its expiry, and the confirmation event the grant follows when there is one.
func grantIssued(g contracts.DecisionGrantV1, decisionID, confirmationEventID string) map[string]any {
	p := map[string]any{"grant_id": string(g.GrantId), "decision_id": decisionID, "capability_id": string(g.CapabilityId), "plan_step": g.PlanStep, "expires_at": string(g.ExpiresAt)}
	if confirmationEventID != "" {
		p["confirmation_event_id"] = confirmationEventID
	}
	return p
}

// declinedByApprover reports whether the decision is a decline of the approver (the reason code DECLINED_BY_APPROVER, SoT 43.2a).
func declinedByApprover(d decide.Decision) bool {
	for _, c := range d.Reasons() {
		if c.String() == "DECLINED_BY_APPROVER" {
			return true
		}
	}
	return false
}
