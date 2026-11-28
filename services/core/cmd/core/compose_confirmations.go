package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

// compose_confirmations.go wires the confirmation route (REQ-TRU-009; INV-11, INV-12, INV-25, AGENT-16; A341, A724): the store is trustpg2.Requests over each tenant's own Trust login,
// its Record events are appended by RecordEvents over the core-trust appender in the answer's own transaction (the Record first), and the store's calls are bounded by a required setting.

// EnvConfirmationTimeout bounds one call of the confirmation store, in whole milliseconds. Required, no default (AGENT-16).
const EnvConfirmationTimeout = "NOEMA_CONFIRMATION_TIMEOUT_MS"

// confirmationStores is one store per tenant: a tenant with no login has none and finds nothing (INV-11).
type confirmationStores map[string]trustpg2.Requests

// Lookup finds the request for this tenant only; a tenant with no store answers not found, never another tenant's row.
func (s confirmationStores) Lookup(ctx context.Context, tenant, requestID string) (confirm.StoredRequest, bool, error) {
	r, ok := s[tenant]
	if !ok {
		return confirm.StoredRequest{}, false, nil
	}
	return r.Lookup(ctx, tenant, requestID)
}

// Answer records the answer in the tenant's own store; the identity, roles and kind are the session's (INV-12). The store's conflicts are mapped to the route's errors.
func (s confirmationStores) Answer(ctx context.Context, a ConfirmationAnswerInput) (string, error) {
	r, ok := s[a.TenantID]
	if !ok {
		return "", errors.New("cmd/core: no confirmation store for this tenant")
	}
	id, err := r.Answer(ctx, trustpg2.AnswerInput{TenantID: a.TenantID, RequestID: a.RequestID, ApproverID: a.ApproverID, Roles: a.Roles, Response: a.Response, ApproverKind: a.ApproverKind})
	return id, mapConfirmationError(err)
}

// mapConfirmationError maps the store's already-answered and expired errors to the route's; any other error is passed on unchanged.
func mapConfirmationError(err error) error {
	switch {
	case errors.Is(err, trustpg2.ErrAlreadyAnswered):
		return fmt.Errorf("%w", ErrConfirmationAnswered)
	case errors.Is(err, trustpg2.ErrRequestExpired):
		return fmt.Errorf("%w", ErrConfirmationExpired)
	}
	return err
}

// buildConfirmations builds the route's config. Without the bound, the logins or the appender it is a named gap; a bound that is present and invalid is the error (no value in it).
func buildConfirmations(env Env, clk clock.Clock, gen *ids.Generator, logins map[string]*pgxpool.Pool, app *chain.Appender, log *slog.Logger) (*confirmationRouteConfig, string, error) {
	if env.Getenv(EnvConfirmationTimeout) == "" {
		return nil, "confirmations: " + EnvConfirmationTimeout + " missing", nil
	}
	timeout, err := recorderSettingMS(env, EnvConfirmationTimeout)
	if err != nil {
		return nil, "", err
	}
	if len(logins) == 0 || app == nil || gen == nil || clk == nil {
		return nil, "confirmations: the Trust logins, the appender or the clock are not built", nil
	}
	stores := confirmationStores{}
	for tenant, pool := range logins {
		stores[tenant] = trustpg2.Requests{Pool: pool, IDs: gen, Events: trustpg2.RecordEvents{Appender: app}}
	}
	return &confirmationRouteConfig{Requests: stores, Answers: stores, Clock: clk, Timeout: timeout, Logger: log}, "", nil
}
