package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// payloadreader.go is the PayloadReader of the Reconciler (ruling A228, relayed, Founder-approved interim, not yet committed; noema#596): it reads the recorded payload of an
// evidence item by (tenant_id, payload_ref) as that tenant's own trust_rw login, over the same file-mounted per-tenant DSN directory as the Record reader and the Recorder
// (A170, A202). The database, not this code, decides which tenant's rows a connection can read (migration 0008: row-level security through evidence_login_tenant, DEBT-04); this
// code refuses to hand out any pool but the tenant's own, so another tenant's reference is absent, never readable. A DSN holds a password: no error here carries one (AGENT-13).

// EnvTrustDSNDir names the directory of the Trust logins' DSN files. Unset means no payload reader; there is no default directory.
const EnvTrustDSNDir = "NOEMA_TRUST_DSN_DIR"

// EnvPayloadMaxBytes names the largest payload the reader returns, in bytes of its JSON text. It is required whenever the DSN directory is set and has no default: the number
// is a policy of the Architect and the Founders (AGENT-16), set in Compose.
const EnvPayloadMaxBytes = "NOEMA_PAYLOAD_MAX_BYTES"

var (
	// ErrNoPayloadPool is returned for a tenant that has no Trust login configured. It never falls back to another tenant's login.
	ErrNoPayloadPool = errors.New("cmd/core: no Trust login is configured for this tenant")
	// ErrPayloadNotFound is returned when the tenant's login sees no payload under the reference: it does not exist, or it belongs to another tenant (the two are the same
	// answer, REQ-TRU-003).
	ErrPayloadNotFound = errors.New("cmd/core: no payload under this reference for this tenant")
	// ErrPayloadUnusable is returned for a call that cannot be answered safely: no deadline, no tenant or one that is not a tenant ID, a reference that is not a plain
	// reference, or no reader. The database is not asked.
	ErrPayloadUnusable = errors.New("cmd/core: the payload read is not usable")
	// ErrPayloadTooLarge is returned when the payload under the reference is over the configured bound. It names no content, and the payload is not read into memory.
	ErrPayloadTooLarge = errors.New("cmd/core: the payload is larger than the configured bound")
)

// maxRefLen bounds a payload reference: ours are a prefix and a ULID, far under this (a structural bound, not a policy number).
const maxRefLen = 128

// refRe is a plain reference: letters, digits and the separators our IDs use. A space, a control character, a quote or a slash is not one.
var refRe = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9_.:-]*\z`)

// trustPayloads reads evidence payloads over one trust_rw pool per tenant.
type trustPayloads struct {
	pools    map[string]*pgxpool.Pool
	maxBytes int64 // the bound of EnvPayloadMaxBytes; applied in SQL, so an oversize row never leaves the database
}

// Read returns the JSON of the tenant's payload under ref, as the database returns it (jsonb normalises the text). A payload over the bound is ErrPayloadTooLarge.
func (p *trustPayloads) Read(ctx context.Context, tenantID, ref string) ([]byte, error) {
	if p == nil || !tenantFileRe.MatchString(tenantID) || ref == "" || len(ref) > maxRefLen || !refRe.MatchString(ref) {
		return nil, ErrPayloadUnusable
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, fmt.Errorf("%w: the context needs a deadline", ErrPayloadUnusable)
	}
	pool, ok := p.pools[tenantID]
	if !ok {
		return nil, ErrNoPayloadPool
	}
	var (
		size int64
		doc  []byte
	)
	err := pool.QueryRow(ctx, `SELECT octet_length(json::text)::bigint, CASE WHEN octet_length(json::text) <= $3 THEN json END FROM evidence_payload WHERE tenant_id = $1 AND payload_ref = $2`,
		tenantID, ref, p.maxBytes).Scan(&size, &doc)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrPayloadNotFound
	case err != nil:
		// The driver's error can name the server and the login; the message keeps only that the read failed.
		return nil, errors.New("cmd/core: the payload read failed")
	case size > p.maxBytes:
		return nil, ErrPayloadTooLarge
	}
	return doc, nil
}

// buildPayloadReader reads the DSN files and returns the reader and a function that closes every pool (safe to call more than once). With the setting unset it returns no
// reader, a function that does nothing and no error. With it set, any file that cannot be applied safely is an error naming the setting or the tenant, with no reader and
// no pool left open. Pools connect lazily: nothing is dialled here.
func buildPayloadReader(ctx context.Context, env Env) (*trustPayloads, func(), error) {
	noop := func() {}
	if env.Getenv(EnvTrustDSNDir) == "" {
		return nil, noop, nil
	}
	max, err := strconv.ParseInt(env.Getenv(EnvPayloadMaxBytes), 10, 64)
	if err != nil || max < 1 {
		return nil, nil, fmt.Errorf("cmd/core: %s must be a positive whole number of bytes", EnvPayloadMaxBytes)
	}
	pools, closer, err := loginPools(ctx, env, EnvTrustDSNDir)
	if err != nil {
		return nil, nil, err
	}
	return &trustPayloads{pools: pools, maxBytes: max}, closer, nil
}
