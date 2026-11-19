package trustpg2

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

// Errors of the request store.
var (
	// ErrRequestConflict: the database refused the write because it would break the chain of requests of one action, or answer one request twice differently.
	ErrRequestConflict = errors.New("trustpg2: the request or answer conflicts with what is stored")
	// ErrRequestForm: a field is not in the contract's form, or the context has no deadline.
	ErrRequestForm = errors.New("trustpg2: the request or answer is not in the contract's form")
	// ErrNotExpired: the request has not expired yet, or does not exist.
	ErrNotExpired = errors.New("trustpg2: the request has not expired")
	// ErrRequestNotBuilt is the red-commit stub's answer.
	ErrRequestNotBuilt = errors.New("trustpg2: not built")
)

// NewRequest is a confirmation request to store (the fields of trustport.NewConfirmationRequest).
type NewRequest struct {
	TenantID      string
	CorrelationID string
	IntentID      string
	Version       int
	Binding       confirm.BindingInput
	Requirement   string
	RequesterID   string
	ExpiresAt     int64 // Unix seconds
	Supersedes    string
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
	At         int64 // Unix seconds
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
	Clock  clock.Clock
	IDs    *ids.Generator
	Events RequestEvents
}

// Latest is the newest request of the intent of the principal, with its first answer.
func (r Requests) Latest(ctx context.Context, tenantID, principalID, intentID string, version int) (RequestRecord, bool, error) {
	return RequestRecord{}, false, ErrRequestNotBuilt
}

// Open stores a new request and returns its ID.
func (r Requests) Open(ctx context.Context, n NewRequest) (string, error) {
	return "", ErrRequestNotBuilt
}

// Expire records CONFIRMATION_EXPIRED for the request, once.
func (r Requests) Expire(ctx context.Context, tenantID, requestID string) error {
	return ErrRequestNotBuilt
}

// Answer stores the approver's answer once per (request, approver) and returns its event ID; the same answer again returns the stored event ID.
func (r Requests) Answer(ctx context.Context, a AnswerInput) (string, error) {
	return "", ErrRequestNotBuilt
}
