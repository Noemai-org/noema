package trustpg2

// A552 (interim, relayed, not yet committed): the confirmation store no longer creates requests in production. A request exists only through the Recorder's CONFIRM decision, in the
// same transaction (trust/recorder). The creator below is a TEST helper: it makes the request rows that the answer and expiry tests of this package need, with the same checks the store
// had, and is compiled into the tests only (an in-package _test file, so it is not part of the package a service imports).

import (
	"context"
	"fmt"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/confirm"
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

// Open stores a new request and returns its ID, for tests only. The Record event is appended inside the transaction, before the commit: if the append fails, nothing is stored.
func (r Requests) Open(ctx context.Context, n NewRequest) (string, error) {
	if err := r.ready(); err != nil {
		return "", err
	}
	hash, err := confirm.BindingHash(n.Binding)
	switch {
	case !hasDeadline(ctx), err != nil, !safeRe.MatchString(n.TenantID), !safeRe.MatchString(n.CorrelationID), !safeRe.MatchString(n.IntentID), n.Version < 1,
		!requirementRe.MatchString(n.Requirement), !safeRe.MatchString(n.RequesterID), n.TTL <= 0, n.Supersedes != "" && !requestIDRe.MatchString(n.Supersedes):
		return "", ErrRequestForm
	}
	id, err := r.IDs.ID("cfr")
	if err != nil {
		return "", fmt.Errorf("trustpg2: make a request ID: %w", err)
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("trustpg2: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	b := n.Binding
	if _, err := tx.Exec(ctx, `INSERT INTO confirmation_request (tenant_id, confirmation_request_id, correlation_id, intent_id, intent_version, principal_id, intent_hash, capability_id,
			target_ref, params_hash, evidence_set_hash, plan_hash, policy_version, binding_hash, requirement, requester_id, expires_at, supersedes_request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, now() + make_interval(secs => $17::float8), NULLIF($18, ''))`,
		n.TenantID, id, n.CorrelationID, n.IntentID, n.Version, b.PrincipalID, b.IntentHash, b.CapabilityID, b.TargetRef, b.ParamsHash, b.EvidenceSetHash, b.PlanHash, b.PolicyVersion,
		hash, n.Requirement, n.RequesterID, n.TTL.Seconds(), n.Supersedes); err != nil {
		return "", mapErr("store the request", err)
	}
	if err := r.Events.Requested(ctx, RequestedEvent{TenantID: n.TenantID, CorrelationID: n.CorrelationID, RequestID: id, BindingHash: hash, Requirement: n.Requirement,
		Supersedes: n.Supersedes}); err != nil {
		return "", fmt.Errorf("trustpg2: record CONFIRMATION_REQUESTED: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("trustpg2: commit: %w", err)
	}
	return id, nil
}

// LookupAt is Lookup judged at the instant `at` instead of the database's now(), so that the boundary of the expiry (A612: exactly at expiry is expired) can be tested exactly.
func (r Requests) LookupAt(ctx context.Context, tenantID, requestID string, at time.Time) (confirm.StoredRequest, bool, error) {
	return r.lookup(ctx, tenantID, requestID, &at)
}
