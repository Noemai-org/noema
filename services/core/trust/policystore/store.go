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

// storeError is a failure of the store as callers see it (A347, AGENT-13): a fixed public text, with the cause kept for errors.Is and errors.As and never printed. The tenant's ID, the
// policy ID and the version, and the driver's message (SQLSTATE text, server and login details) are not in the text. kind, when set, is the sentinel errors.Is finds; cause may be nil.
type storeError struct {
	kind  error
	text  string
	cause error
}

func (e *storeError) Error() string { return e.text }

func (e *storeError) Unwrap() error { return e.cause }

func (e *storeError) Is(target error) bool { return e.kind != nil && errors.Is(e.kind, target) }

var (
	tenantRe      = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	correlationRe = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	policyIDRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	versionRe     = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(\.[1-9][0-9]{0,8})?$`)
	publisherRe   = regexp.MustCompile(`^[a-z]{2,6}_[0-9A-Za-z_]{2,64}$`)
	// documentHashRe is the form of a document hash (policy.DocumentHash); the database checks the same form (policy_publication_document_hash_form, migration 0035).
	documentHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
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
	DocumentHash  string // sha256: and 64 lower-case hex digits, the hash of the policy document (policy.DocumentHash); required: the database refuses a publication without it
}

// Store writes publications. The Appender writes the Record event in the caller's transaction.
type Store struct {
	Appender *chain.Appender
}

// Publish records the version and writes POLICY_PUBLISHED, in the caller's transaction (tx). The event's payload names the policy, the version, the publisher and the document_hash the row stores
// (ruling on noema#771, INTERIM: relayed by the Founder in session, not yet committed, AGENTS section 2.3); the hash is a typed sha256 value, which the Record's payload guard accepts whole. If the version is not higher than the active one the database
// refuses it (ErrNotNewer) and nothing is written. The caller commits or rolls back.
func (s *Store) Publish(ctx context.Context, tx pgx.Tx, r Request) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New(errPrefix + "the context needs a deadline")
	}
	if s == nil || s.Appender == nil {
		return &storeError{kind: ErrRequest, text: ErrRequest.Error() + ": the store is not configured"}
	}
	switch {
	case !tenantRe.MatchString(r.TenantID), !correlationRe.MatchString(r.CorrelationID), !policyIDRe.MatchString(r.PolicyID), !versionRe.MatchString(r.Version),
		!publisherRe.MatchString(r.PublishedBy), !documentHashRe.MatchString(r.DocumentHash):
		return fmt.Errorf("%w: a tenant, correlation, policy ID, version, publisher or document hash is outside its pattern", ErrRequest)
	}
	_, err := tx.Exec(ctx, `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, document_hash)
		VALUES ($1, $2, $3, policy_version_key($3), $4, $5)`, r.TenantID, r.PolicyID, r.Version, r.PublishedBy, r.DocumentHash)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && (pe.Code == "23000" || pe.Code == "23505") {
		return &storeError{kind: ErrNotNewer, text: ErrNotNewer.Error(), cause: err}
	}
	if err != nil {
		return &storeError{text: errPrefix + "the publication could not be recorded", cause: err}
	}
	ref := "pol_" + underscored(r.PolicyID) + "_" + underscored(r.Version)
	_, err = s.Appender.Append(ctx, tx, chain.Event{TenantID: r.TenantID, CorrelationID: r.CorrelationID, WrittenBy: emitter, Actor: "admin:" + r.PublishedBy,
		EventType: "POLICY_PUBLISHED", ObjectRef: ref, Payload: map[string]any{"policy_id": r.PolicyID, "version": r.Version, "published_by": r.PublishedBy, "document_hash": r.DocumentHash}})
	if err != nil {
		return &storeError{text: errPrefix + "the publication could not be appended to the Record", cause: err}
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
		return "", &storeError{text: errPrefix + "the active version could not be read", cause: err}
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
