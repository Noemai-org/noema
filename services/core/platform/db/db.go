package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const errPrefix = "db: "

// cleanupTimeout bounds the cleanup of a request: a rollback, the release of a lock. The cleanup must still be tried when the
// request's own deadline has passed, so it runs on a context detached from the request's cancellation; and it must never run
// without a limit, so that a stalled database cannot hold the request after its answer is decided (review of noema#107).
const cleanupTimeout = 5 * time.Second

// cleanupContext returns the context for a cleanup, and the function that releases it. It is the only place in this package that
// detaches a context from its parent (a test requires it).
func cleanupContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), cleanupTimeout)
}

var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

// Open opens a pool. The deployment decides which login role the DSN names; per-power roles are set with
// AsRole. A pool needs a context with a deadline (the caller's), like every call here.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("%sopen: %w", errPrefix, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%sping: %w", errPrefix, err)
	}
	return pool, nil
}

// OpenAsRole opens a pool whose every connection runs as the given per-power role (SET ROLE), so that the
// database, not the code, decides what that power may write (REQ-DATA-001). The role must be one the login
// role is a member of.
func OpenAsRole(ctx context.Context, dsn, role string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%sparse dsn: %w", errPrefix, err)
	}
	quoted := pgx.Identifier{role}.Sanitize()
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+quoted)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%sopen: %w", errPrefix, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%sping as %s: %w", errPrefix, role, err)
	}
	return pool, nil
}

// unlockStatement releases the migration lock. It is a variable only so that a test can make the release fail or stall on a real
// connection; nothing else changes it.
var unlockStatement = `SELECT pg_advisory_unlock(hashtext('noema:migrate'))`

// unlockMigrations releases the session-level migration lock, within the cleanup limit. If the release fails, times out or is
// uncertain in any way, the lock may still be held by the session, so the connection is NOT returned to the pool: it is taken out
// of the pool and closed, which ends the session and with it the lock, and the failure is returned. Returning a still-locked
// session to the pool would make the next migration wait on it forever (Codex review 5763745105).
func unlockMigrations(ctx context.Context, conn *pgxpool.Conn) error {
	c, release := cleanupContext(ctx)
	defer release()
	if _, err := conn.Exec(c, unlockStatement); err != nil {
		raw := conn.Hijack() // the pool no longer owns it; conn.Release becomes a no-op
		cc, closeRelease := cleanupContext(ctx)
		defer closeRelease()
		closeErr := raw.Close(cc)
		return fmt.Errorf("%srelease the migration lock (the connection was closed so the lock cannot outlive it): %w", errPrefix, errors.Join(err, closeErr))
	}
	return nil
}

// Migrate applies, in file-name order, every migration in fsys that is not yet applied. It is forward-only: a
// migration that was applied and whose file has since changed is an error, and nothing is ever rolled back
// (AGENTS.md section 7). Each file runs in its own transaction with its record in schema_migration. One
// advisory lock serialises concurrent migrators. It returns the names it applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (result []string, retErr error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New(errPrefix + "the context needs a deadline")
	}
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("%slist migrations: %w", errPrefix, err)
	}
	sort.Strings(names)
	for _, n := range names {
		if !migrationName.MatchString(n) {
			return nil, fmt.Errorf("%smigration %q is not named NNNN_name.sql", errPrefix, n)
		}
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("%sacquire: %w", errPrefix, err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('noema:migrate'))`); err != nil {
		return nil, fmt.Errorf("%slock: %w", errPrefix, err)
	}
	defer func() {
		if err := unlockMigrations(ctx, conn); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	if err := refuseEnabledExtensionEventTrigger(ctx, conn); err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migration (
		name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return nil, fmt.Errorf("%screate schema_migration: %w", errPrefix, err)
	}
	applied := map[string]string{}
	rows, err := conn.Query(ctx, `SELECT name, checksum FROM schema_migration`)
	if err != nil {
		return nil, fmt.Errorf("%sread schema_migration: %w", errPrefix, err)
	}
	for rows.Next() {
		var name, sum string
		if err := rows.Scan(&name, &sum); err != nil {
			rows.Close()
			return nil, fmt.Errorf("%sscan: %w", errPrefix, err)
		}
		applied[name] = sum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%sread schema_migration: %w", errPrefix, err)
	}
	for name := range applied {
		if _, err := fs.Stat(fsys, name); err != nil {
			return nil, fmt.Errorf("%smigration %s was applied but its file is gone: migrations are forward-only", errPrefix, name)
		}
	}
	var done []string
	for _, name := range names {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return done, fmt.Errorf("%sread %s: %w", errPrefix, name, err)
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		if prev, ok := applied[name]; ok {
			if prev != checksum {
				return done, fmt.Errorf("%smigration %s changed after it was applied: migrations are never edited, add a new file", errPrefix, name)
			}
			continue
		}
		if err := applyOne(ctx, conn, name, string(body), checksum); err != nil {
			return done, err
		}
		done = append(done, name)
	}
	return done, nil
}

func applyOne(ctx context.Context, conn *pgxpool.Conn, name, body, checksum string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%sbegin %s: %w", errPrefix, name, err)
	}
	defer func() {
		c, release := cleanupContext(ctx)
		defer release()
		_ = tx.Rollback(c)
	}()
	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("%sapply %s: %w", errPrefix, name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migration (name, checksum) VALUES ($1, $2)`, name, checksum); err != nil {
		return fmt.Errorf("%srecord %s: %w", errPrefix, name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%scommit %s: %w", errPrefix, name, err)
	}
	return nil
}

// refuseEnabledExtensionEventTrigger is the guard of DEC-2026-11-01 D10a. pgsodium's event trigger reads a setting that exists only when the
// library is preloaded, so while it is enabled every DDL command can fail, and an extension upgrade or re-install can re-enable it. The bootstrap
// disables it once; Migrate checks before it runs anything, and refuses (fail closed) if any event trigger the extension owns is not disabled.
func refuseEnabledExtensionEventTrigger(ctx context.Context, conn *pgxpool.Conn) error {
	var name string
	err := conn.QueryRow(ctx, `SELECT e.evtname FROM pg_event_trigger e
		JOIN pg_depend d ON d.objid = e.oid AND d.deptype = 'e'
		JOIN pg_extension x ON x.oid = d.refobjid AND x.extname = 'pgsodium'
		WHERE e.evtenabled <> 'D' ORDER BY e.evtname LIMIT 1`).Scan(&name)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("%scheck the event triggers of pgsodium: %w", errPrefix, err)
	}
	return fmt.Errorf("%srefusing to migrate: the event trigger %s of the extension pgsodium is enabled; disable it (deploy/compose/postgres/init.sh, D10a)", errPrefix, name)
}
