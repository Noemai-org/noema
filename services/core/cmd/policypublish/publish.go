package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// publish.go is the one publication. The database decides what is allowed: the guard on policy_publication admits a Trust login mapped to the tenant and refuses a version that
// is not higher than the active one (migration 0015). This file adds the two answers a seed script needs on top of that: "already published" for the version that is active,
// and a clear refusal for a login that is not the tenant's Trust login. It compares no version itself: it only asks whether the active version is the one it tried.

const (
	resultPublished = "published"
	resultAlready   = "already_published"
)

// errNotTrustLogin marks a login that is not the Trust login of the tenant, whether the tenant exists or not: one answer for both, so the command does not tell a login which
// tenants exist.
var errNotTrustLogin = errors.New("policypublish: the login is not the Trust login of this tenant")

// checkLogin refuses, before anything is read or written, a login that is not a member of trust_rw or is not mapped to the tenant. The guard on the table says the same at the
// write; this check gives the refusal its own reason and answers the same for a tenant that does not exist.
func checkLogin(ctx context.Context, q policystore.Queryer, tenantID string) error {
	var member bool
	if err := q.QueryRow(ctx, `SELECT pg_has_role(session_user, 'trust_rw', 'MEMBER')`).Scan(&member); err != nil {
		return fmt.Errorf("policypublish: read the login's role: %w", err)
	}
	if !member {
		return errNotTrustLogin
	}
	var mapped *string
	if err := q.QueryRow(ctx, `SELECT policy_login_tenant()`).Scan(&mapped); err != nil {
		return fmt.Errorf("policypublish: read the login's tenant: %w", err)
	}
	if mapped == nil || *mapped != tenantID {
		return errNotTrustLogin
	}
	return nil
}

// publish records the version and its POLICY_PUBLISHED event in one transaction and returns resultPublished. A version that is not higher than the active one is refused by the
// database. When it is the active version itself the answer is resultAlready and nothing was written: the refused insert is rolled back with the transaction, so no event exists
// for it. Any other refusal is policystore.ErrNotNewer. The active version is read after the refusal, on a fresh read, so that another run that published the same version a
// moment before also gets "already published" and not a refusal.
func publish(ctx context.Context, conn *pgx.Conn, store *policystore.Store, r policystore.Request) (string, error) {
	if err := checkLogin(ctx, conn, r.TenantID); err != nil {
		return "", err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("policypublish: begin: %w", err)
	}
	if err := store.Publish(ctx, tx, r); err != nil {
		rollback(ctx, tx)
		if !errors.Is(err, policystore.ErrNotNewer) {
			return "", err
		}
		active, rerr := policystore.Active(ctx, conn, r.TenantID, r.PolicyID)
		if rerr != nil {
			return "", rerr
		}
		if active == r.PolicyID+"@"+r.Version {
			return resultAlready, nil
		}
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("policypublish: commit: %w", err)
	}
	return resultPublished, nil
}

// rollback ends a transaction that is not kept. Its own error is not returned: the connection is closed at the end of the run, which ends the transaction in any case, and the
// caller already has the error that matters.
func rollback(ctx context.Context, tx pgx.Tx) { _ = tx.Rollback(ctx) }
