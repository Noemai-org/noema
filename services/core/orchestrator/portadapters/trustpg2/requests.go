package trustpg2

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

// Errors of the request store.
var (
	// ErrRequestConflict: the database refused the write because it would break the chain of requests of one action, or answer one request twice differently.
	ErrRequestConflict = errors.New("trustpg2: the request or answer conflicts with what is stored")
	// ErrAlreadyAnswered: the request already has an answer other than a replay of this one (A341: the first answer wins).
	ErrAlreadyAnswered = errors.New("trustpg2: the request was already answered")
	// ErrRequestForm: a field is not in the contract's form, or the context has no deadline.
	ErrRequestForm = errors.New("trustpg2: the request or answer is not in the contract's form")
	// ErrRequestExpired: the request had already expired on the DATABASE clock when the answer arrived (A368 (iv)). Nothing was recorded.
	ErrRequestExpired = errors.New("trustpg2: the request has expired")
	// ErrRequestSuperseded: a newer request replaced this one, so an answer to it is refused (A368 (iv)). Nothing was recorded.
	ErrRequestSuperseded = errors.New("trustpg2: the request was superseded")
	// ErrNotExpired: the request cannot be expired now: it has not expired yet on the database clock, or it does not exist. For the caller both are the same answer (REQ-TRU-003), so
	// nobody retries on it.
	ErrNotExpired = errors.New("trustpg2: the request has not expired")
	// ErrRequestNotBuilt is the red-commit stub's answer.
	ErrRequestNotBuilt = errors.New("trustpg2: not built")
)

// AnswerRecord is an approver's answer as stored.
type AnswerRecord struct {
	EventID string
	Event   confirm.Event
}

// RequestRecord is a stored request with its first answer, if any.
type RequestRecord struct {
	Request confirm.Request
	Answer  *AnswerRecord
}

// AnswerInput is an approver's answer, with the identity and roles the authenticated session gave (INV-12).
type AnswerInput struct {
	TenantID   string
	RequestID  string
	ApproverID string
	Roles      []string
	Response   confirm.Response
	// There is no time field: answered_at is the database's now() (A368 (ii)). The store records the roles as the session gave them and does not re-check eligibility: the route is the check.
}

// RequestedEvent, ReceivedEvent and ExpiredEvent are what the Record gets (CONFIRMATION_REQUESTED, CONFIRMATION_RECEIVED, CONFIRMATION_EXPIRED).
type (
	RequestedEvent struct {
		TenantID, CorrelationID, RequestID, BindingHash, Requirement, Supersedes string
	}
	ReceivedEvent struct {
		TenantID, CorrelationID, RequestID, EventID, ApproverID string
		Accepted                                                bool
	}
	ExpiredEvent struct{ TenantID, CorrelationID, RequestID string }
)

// RequestEvents appends the Record events of the path. The store calls it before it commits: if the append fails, nothing is stored.
type RequestEvents interface {
	Requested(ctx context.Context, e RequestedEvent) error
	Received(ctx context.Context, e ReceivedEvent) error
	Expired(ctx context.Context, e ExpiredEvent) error
}

// Requests stores confirmation requests and answers (migration 0032) as the Trust login of one tenant.
type Requests struct {
	Pool   *pgxpool.Pool
	IDs    *ids.Generator
	Events RequestEvents
}

var (
	safeRe        = regexp.MustCompile(`^[A-Za-z0-9_.:@-]+$`)
	requestIDRe   = regexp.MustCompile(`^cfr_[0-9A-Za-z]{2,32}$`)
	requirementRe = regexp.MustCompile(`^(SELF|MAKER_CHECKER|ROLE:[A-Za-z0-9_.-]+)$`)
)

func hasDeadline(ctx context.Context) bool {
	_, ok := ctx.Deadline()
	return ok
}

// SQLSTATEs of migration 0032's typed refusals (class 23, integrity constraint violation).
const (
	codeExpired    = "23E01"
	codeSuperseded = "23S01"
)

// mapErr turns a refusal of the database's integrity checks into ErrRequestConflict, and the two typed refusals of an answer into their errors; any other error is wrapped as it is.
func mapErr(what string, err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case codeExpired:
			return fmt.Errorf("%w: %s", ErrRequestExpired, what)
		case codeSuperseded:
			return fmt.Errorf("%w: %s", ErrRequestSuperseded, what)
		}
	}
	if errors.As(err, &pe) && len(pe.Code) >= 2 && pe.Code[:2] == "23" {
		return fmt.Errorf("%w: %s: %s", ErrRequestConflict, what, pe.Message)
	}
	return fmt.Errorf("trustpg2: %s: %w", what, err)
}

func (r Requests) ready() error {
	if r.Pool == nil || r.IDs == nil || r.Events == nil {
		return fmt.Errorf("%w: the store needs a pool, an ID generator and the Record events", ErrRequestForm)
	}
	return nil
}

// Latest is the newest request of the intent of the principal, with its answer: a request has at most one (UNIQUE (tenant_id, confirmation_request_id)), so there is no "first" to order by a time. The intent version is not part of the filter: a new version has a new intent hash, which
// is a binding change and is answered by a request that supersedes this one.
func (r Requests) Latest(ctx context.Context, tenantID, principalID, intentID string, _ int) (RequestRecord, bool, error) {
	if !hasDeadline(ctx) || r.Pool == nil || !safeRe.MatchString(tenantID) || !safeRe.MatchString(principalID) || !safeRe.MatchString(intentID) {
		return RequestRecord{}, false, ErrRequestForm
	}
	var (
		out     RequestRecord
		expires time.Time
	)
	b := &out.Request.Binding
	err := r.Pool.QueryRow(ctx, `SELECT confirmation_request_id, principal_id, intent_hash, capability_id, target_ref, params_hash, evidence_set_hash, plan_hash, policy_version,
			requirement, requester_id, expires_at
		FROM confirmation_request WHERE tenant_id = $1 AND principal_id = $2 AND intent_id = $3
		ORDER BY requested_at DESC, confirmation_request_id DESC LIMIT 1`, tenantID, principalID, intentID).
		Scan(&out.Request.ID, &b.PrincipalID, &b.IntentHash, &b.CapabilityID, &b.TargetRef, &b.ParamsHash, &b.EvidenceSetHash, &b.PlanHash, &b.PolicyVersion,
			&out.Request.Requirement, &out.Request.RequesterID, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestRecord{}, false, nil
	}
	if err != nil {
		return RequestRecord{}, false, fmt.Errorf("trustpg2: read the request: %w", err)
	}
	out.Request.ExpiresAt = expires.Unix()
	var (
		a  AnswerRecord
		at time.Time
	)
	err = r.Pool.QueryRow(ctx, `SELECT confirmation_event_id, approver_id, approver_roles, response, answered_at FROM confirmation_answer
		WHERE tenant_id = $1 AND confirmation_request_id = $2`, tenantID, out.Request.ID).
		Scan(&a.EventID, &a.Event.ApproverID, &a.Event.ApproverRoles, &a.Event.Response, &at)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return RequestRecord{}, false, fmt.Errorf("trustpg2: read the answer: %w", err)
	default:
		a.Event.RequestID, a.Event.At = out.Request.ID, at.Unix()
		out.Answer = &a
	}
	return out, true, nil
}

// Lookup is the request the ID names, for this tenant only (A550: the request-by-id read of the confirmation route). It is a read: no event, no write. A request of another tenant (row
// security hides it, and the tenant is in the predicate too), an unknown ID and a request the store does not hold are the same answer, not found (REQ-TRU-003). A malformed tenant or ID
// and a context with no deadline are ErrRequestForm. The request is returned as stored, with its expiry at the database's precision and the database's own verdict on it (A607, A612):
// whether it was answered or superseded is the answer path's to decide (A368 (iv)).
func (r Requests) Lookup(ctx context.Context, tenantID, requestID string) (confirm.StoredRequest, bool, error) {
	return r.lookup(ctx, tenantID, requestID, nil)
}

// lookup is Lookup judged at `at` instead of the database's now(), for the boundary tests only (export_test.go); Lookup passes nil.
func (r Requests) lookup(ctx context.Context, tenantID, requestID string, at *time.Time) (confirm.StoredRequest, bool, error) {
	if !hasDeadline(ctx) || r.Pool == nil || !safeRe.MatchString(tenantID) || !requestIDRe.MatchString(requestID) {
		return confirm.StoredRequest{}, false, ErrRequestForm
	}
	var (
		out     confirm.StoredRequest
		expires time.Time
	)
	_ = at
	b := &out.Request.Binding
	err := r.Pool.QueryRow(ctx, `SELECT confirmation_request_id, principal_id, intent_hash, capability_id, target_ref, params_hash, evidence_set_hash, plan_hash, policy_version,
			requirement, requester_id, expires_at
		FROM confirmation_request WHERE tenant_id = $1 AND confirmation_request_id = $2`, tenantID, requestID).
		Scan(&out.Request.ID, &b.PrincipalID, &b.IntentHash, &b.CapabilityID, &b.TargetRef, &b.ParamsHash, &b.EvidenceSetHash, &b.PlanHash, &b.PolicyVersion, &out.Request.Requirement, &out.Request.RequesterID, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return confirm.StoredRequest{}, false, nil
	}
	if err != nil {
		return confirm.StoredRequest{}, false, fmt.Errorf("trustpg2: read the request: %w", err)
	}
	out.Request.ExpiresAt = expires.Unix()
	out.ExpiresAt = time.Unix(expires.Unix(), 0).UTC()
	return out, true, nil
}

// Expire records CONFIRMATION_EXPIRED for the request, once, and only when the request has expired by the clock.
func (r Requests) Expire(ctx context.Context, tenantID, requestID string) error {
	if err := r.ready(); err != nil {
		return err
	}
	if !hasDeadline(ctx) || !safeRe.MatchString(tenantID) || !requestIDRe.MatchString(requestID) {
		return ErrRequestForm
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("trustpg2: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var correlation string
	err = tx.QueryRow(ctx, `SELECT correlation_id FROM confirmation_request WHERE tenant_id = $1 AND confirmation_request_id = $2`, tenantID, requestID).Scan(&correlation)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: no such request", ErrNotExpired)
	}
	if err != nil {
		return fmt.Errorf("trustpg2: read the request: %w", err)
	}
	// the expiry is decided by the database clock in this statement (A368 (ii)); observed_at is the database's now(), set by the trigger of 0032
	tag, err := tx.Exec(ctx, `INSERT INTO confirmation_expiry (tenant_id, confirmation_request_id)
		SELECT tenant_id, confirmation_request_id FROM confirmation_request WHERE tenant_id = $1 AND confirmation_request_id = $2 AND expires_at <= now()
		ON CONFLICT (tenant_id, confirmation_request_id) DO NOTHING`, tenantID, requestID)
	if err != nil {
		return mapErr("store the expiry", err)
	}
	if tag.RowsAffected() == 0 {
		var recorded bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM confirmation_expiry WHERE tenant_id = $1 AND confirmation_request_id = $2)`, tenantID, requestID).Scan(&recorded); err != nil {
			return fmt.Errorf("trustpg2: read the expiry: %w", err)
		}
		if !recorded {
			return ErrNotExpired
		}
		return nil // recorded before
	}
	if err := r.Events.Expired(ctx, ExpiredEvent{TenantID: tenantID, CorrelationID: correlation, RequestID: requestID}); err != nil {
		return fmt.Errorf("trustpg2: record CONFIRMATION_EXPIRED: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("trustpg2: commit: %w", err)
	}
	return nil
}

// Answer stores the first answer to a request (A341: the first answer wins) and returns its event ID. A replay by the same approver with the same response returns the stored event ID and
// records nothing; any other second answer, by this or another approver, is ErrAlreadyAnswered. An answer to a request that expired on the database clock is ErrRequestExpired, and one to a
// request that has a successor is ErrRequestSuperseded (A368 (iv)): the database decides both in the insert, so no Go clock is involved and a concurrent supersede cannot slip between.
func (r Requests) Answer(ctx context.Context, a AnswerInput) (string, error) {
	if err := r.ready(); err != nil {
		return "", err
	}
	if !hasDeadline(ctx) || !safeRe.MatchString(a.TenantID) || !requestIDRe.MatchString(a.RequestID) || !safeRe.MatchString(a.ApproverID) ||
		(a.Response != confirm.Accept && a.Response != confirm.Decline) {
		return "", ErrRequestForm
	}
	roles := make([]string, 0, len(a.Roles))
	for _, role := range a.Roles {
		if !safeRe.MatchString(role) {
			return "", ErrRequestForm
		}
		roles = append(roles, role)
	}
	event, err := r.IDs.ID("cfe")
	if err != nil {
		return "", fmt.Errorf("trustpg2: make an event ID: %w", err)
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("trustpg2: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var correlation string
	err = tx.QueryRow(ctx, `SELECT correlation_id FROM confirmation_request WHERE tenant_id = $1 AND confirmation_request_id = $2`, a.TenantID, a.RequestID).Scan(&correlation)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: no such request", ErrRequestConflict)
	}
	if err != nil {
		return "", fmt.Errorf("trustpg2: read the request: %w", err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO confirmation_answer (tenant_id, confirmation_event_id, confirmation_request_id, response, approver_id, approver_roles)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (tenant_id, confirmation_request_id) DO NOTHING`,
		a.TenantID, event, a.RequestID, string(a.Response), a.ApproverID, roles)
	if err != nil {
		return "", mapErr("store the answer", err)
	}
	if tag.RowsAffected() == 0 {
		var stored, response, approver string
		if err := tx.QueryRow(ctx, `SELECT confirmation_event_id, response, approver_id FROM confirmation_answer WHERE tenant_id = $1 AND confirmation_request_id = $2`,
			a.TenantID, a.RequestID).Scan(&stored, &response, &approver); err != nil {
			return "", fmt.Errorf("trustpg2: read the stored answer: %w", err)
		}
		// the first answer wins (A341): only the same approver's same response is a replay; anything else is ErrAlreadyAnswered
		if approver != a.ApproverID || response != string(a.Response) {
			return "", ErrAlreadyAnswered
		}
		return stored, nil
	}
	if err := r.Events.Received(ctx, ReceivedEvent{TenantID: a.TenantID, CorrelationID: correlation, RequestID: a.RequestID, EventID: event, ApproverID: a.ApproverID,
		Accepted: a.Response == confirm.Accept}); err != nil {
		return "", fmt.Errorf("trustpg2: record CONFIRMATION_RECEIVED: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("trustpg2: commit: %w", err)
	}
	return event, nil
}
