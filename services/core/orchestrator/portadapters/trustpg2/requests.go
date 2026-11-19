package trustpg2

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

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

// NewRequest is a confirmation request to store (the fields of trustport.NewConfirmationRequest).
type NewRequest struct {
	TenantID      string
	CorrelationID string
	IntentID      string
	Version       int // at least 1
	Binding       confirm.BindingInput
	Requirement   string
	RequesterID   string
	// TTL is how long the request lives, counted by the DATABASE clock from the moment it is stored. There is no time field: the store never sends a time (A368 (ii)).
	TTL        time.Duration
	Supersedes string
}

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

// The four methods are the red commit's stubs: the behaviour of A368 is not built.

// Latest is the newest request of the intent of the principal, with its answer.
func (r Requests) Latest(_ context.Context, _, _, _ string, _ int) (RequestRecord, bool, error) {
	return RequestRecord{}, false, ErrRequestNotBuilt
}

// Open stores a new request and returns its ID.
func (r Requests) Open(_ context.Context, _ NewRequest) (string, error) {
	return "", ErrRequestNotBuilt
}

// Expire records CONFIRMATION_EXPIRED for the request, once.
func (r Requests) Expire(_ context.Context, _, _ string) error { return ErrRequestNotBuilt }

// Answer stores the first answer to a request.
func (r Requests) Answer(_ context.Context, _ AnswerInput) (string, error) {
	return "", ErrRequestNotBuilt
}
