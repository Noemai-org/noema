package policystore

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	errPrefix = "policystore: "
	emitter   = "core-trust"
)

var (
	// ErrRequest marks a request that cannot be honoured; nothing was written.
	ErrRequest = errors.New(errPrefix + "the request is not valid")
	// ErrNotNewer marks a version that is not higher than the active version of its policy_id: the same version again, or a downgrade.
	ErrNotNewer = errors.New(errPrefix + "the version is not higher than the active one")
)

var (
	tenantRe      = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	correlationRe = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	policyIDRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	versionRe     = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(\.[1-9][0-9]{0,8})?$`)
	publisherRe   = regexp.MustCompile(`^[a-z]{2,6}_[0-9A-Za-z_]{2,64}$`)
)

// Queryer is the read side: a pool, a connection or a transaction.
type Queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Request is one publication.
type Request struct {
	TenantID      string
	CorrelationID string // the correlation of the administrative action
	PolicyID      string
	Version       string // dotted numeric, two or three components: "3.1"
	PublishedBy   string // the administrator's principal ID, for example adm_synth01
}

// Store writes publications. The Appender writes the Record event in the caller's transaction.
type Store struct {
	Appender *chain.Appender
}

// Publish records the version and writes POLICY_PUBLISHED, in the caller's transaction (tx). If the version is not higher than the active one the database
// refuses it (ErrNotNewer) and nothing is written. The caller commits or rolls back.
func (s *Store) Publish(ctx context.Context, tx pgx.Tx, r Request) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New(errPrefix + "the context needs a deadline")
	}
	if s == nil || s.Appender == nil {
		return fmt.Errorf("%s: the store is not configured", ErrRequest)
	}
	switch {
	case !tenantRe.MatchString(r.TenantID), !correlationRe.MatchString(r.CorrelationID), !policyIDRe.MatchString(r.PolicyID), !versionRe.MatchString(r.Version),
		!publisherRe.MatchString(r.PublishedBy):
		return fmt.Errorf("%w: a tenant, correlation, policy ID, version or publisher is outside its pattern", ErrRequest)
	}
	_, err := tx.Exec(ctx, `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by)
		VALUES ($1, $2, $3, policy_version_key($3), $4)`, r.TenantID, r.PolicyID, r.Version, r.PublishedBy)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && (pe.Code == "23000" || pe.Code == "23505") {
		return fmt.Errorf("%w: %s@%s", ErrNotNewer, r.PolicyID, r.Version)
	}
	if err != nil {
		return fmt.Errorf("%sinsert publication: %w", errPrefix, err)
	}
	ref := "pol_" + underscored(r.PolicyID) + "_" + underscored(r.Version)
	_, err = s.Appender.Append(ctx, tx, chain.Event{TenantID: r.TenantID, CorrelationID: r.CorrelationID, WrittenBy: emitter, Actor: "admin:" + r.PublishedBy,
		EventType: "POLICY_PUBLISHED", ObjectRef: ref, Payload: map[string]any{"policy_id": r.PolicyID, "version": r.Version, "published_by": r.PublishedBy}})
	if err != nil {
		return fmt.Errorf("%sappend POLICY_PUBLISHED: %w", errPrefix, err)
	}
	return nil
}

// Active is the active version of the tenant's policy as policy_id@version, or "" when the policy has none. The tenant is also bound by the login, so
// a read of another tenant's policy through one's own login finds nothing.
func Active(ctx context.Context, q Queryer, tenantID, policyID string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		return "", errors.New(errPrefix + "the context needs a deadline")
	}
	if !tenantRe.MatchString(tenantID) || !policyIDRe.MatchString(policyID) {
		return "", fmt.Errorf("%w: a tenant or policy ID is outside its pattern", ErrRequest)
	}
	var v string
	err := q.QueryRow(ctx, `SELECT version FROM policy_publication WHERE tenant_id = $1 AND policy_id = $2 ORDER BY version_key DESC LIMIT 1`, tenantID, policyID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%sread active version: %w", errPrefix, err)
	}
	return policyID + "@" + v, nil
}

func underscored(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c == '.' {
			b[i] = '_'
		}
	}
	return string(b)
}
