// Package store is mock-case's Postgres layer. Every command runs in one transaction that also writes its
// command_log row, so a command and its effect are committed together or not at all. All state changes are
// guarded again by the database (migrations/0001_init.sql): the service role cannot close, delete or rewrite.
package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store holds two pools: app serves every API call; admin runs migrations and the bench-only reset.
type Store struct {
	app   *pgxpool.Pool
	admin *pgxpool.Pool
}

// Open connects both roles.
func Open(ctx context.Context, appDSN, adminDSN string) (*Store, error) {
	app, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		return nil, fmt.Errorf("app pool: %w", err)
	}
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		app.Close()
		return nil, fmt.Errorf("admin pool: %w", err)
	}
	return &Store{app: app, admin: admin}, nil
}

// Close releases both pools.
func (s *Store) Close() {
	s.app.Close()
	s.admin.Close()
}

// Ping reports whether the serving role can reach the database.
func (s *Store) Ping(ctx context.Context) error { return s.app.Ping(ctx) }

// Migrate applies every embedded migration that has not run yet, in file order, once, under an advisory lock.
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.admin.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7402601)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sqlText, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Reset empties every table and restarts the counters. It runs as the admin role, only for the bench control
// API: the triggers that keep the tables append-only are switched off for the length of one transaction.
func (s *Store) Reset(ctx context.Context) error {
	tx, err := s.admin.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tables := []string{"cases", "complaints", "evidence", "command_log"}
	for _, t := range tables {
		if _, err := tx.Exec(ctx, "ALTER TABLE "+t+" DISABLE TRIGGER USER"); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `TRUNCATE cases, complaints, evidence, command_log RESTART IDENTITY`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE counters SET value = 0`); err != nil {
		return err
	}
	for _, t := range tables {
		if _, err := tx.Exec(ctx, "ALTER TABLE "+t+" ENABLE TRIGGER USER"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type result struct {
	outcome   Outcome
	tenant    string
	caseID    string
	key       string
	reqHash   string
	committed bool
	replay    bool
}

var errRetry = errors.New("lost a race to a concurrent create; run again")

// run executes fn in a transaction, logs the command, and commits. fn returns errRetry when a concurrent
// create of the same key won the race: the transaction is rolled back (so no counter value is lost) and fn
// runs again, now finding the winner's row.
func (s *Store) run(ctx context.Context, cmd Cmd, fn func(pgx.Tx) (result, error)) (Outcome, error) {
	if err := cmd.validate(); err != nil {
		return 0, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		tx, err := s.app.Begin(ctx)
		if err != nil {
			return 0, err
		}
		res, err := fn(tx)
		if errors.Is(err, errRetry) {
			tx.Rollback(ctx) //nolint:errcheck
			continue
		}
		if err == nil {
			err = logCommand(ctx, tx, cmd, res)
		}
		if err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return res.outcome, nil
	}
	return 0, errors.New("create kept losing races; giving up")
}

func logCommand(ctx context.Context, tx pgx.Tx, cmd Cmd, res result) error {
	status := res.outcome.Status()
	if cmd.StatusOverride != 0 {
		status = cmd.StatusOverride
	}
	if cmd.Suppressed {
		status = 0
	}
	var params any
	if len(cmd.FaultParams) > 0 {
		params = string(cmd.FaultParams)
	}
	_, err := tx.Exec(ctx, `
INSERT INTO command_log (at, scenario_id, seed, fault_plan_id, operation, call_no, tenant_id, case_id, idempotency_key,
  correlation_id, request_hash, status_code, error_code, committed, replay, fault_index, fault_effect, fault_params, response_suppressed)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18::jsonb,$19)`,
		Micro(cmd.At), cmd.ScenarioID, cmd.Seed, cmd.FaultPlanID, cmd.Operation, cmd.CallNo,
		nullable(res.tenant), nullable(res.caseID), nullable(res.key), nullable(cmd.CorrelationID), nullable(res.reqHash),
		status, nullable(res.outcome.ErrorCode()), res.committed, res.replay, cmd.FaultIndex, nullable(cmd.FaultEffect), params, cmd.Suppressed)
	return err
}

// LogOnly records a command that never reached the store: a fault answered it first, or it was refused.
func (s *Store) LogOnly(ctx context.Context, cmd Cmd, tenant, caseID, key string, status int, errorCode string) error {
	if err := cmd.validate(); err != nil {
		return err
	}
	var params any
	if len(cmd.FaultParams) > 0 {
		params = string(cmd.FaultParams)
	}
	if cmd.Suppressed {
		status = 0
	}
	_, err := s.app.Exec(ctx, `
INSERT INTO command_log (at, scenario_id, seed, fault_plan_id, operation, call_no, tenant_id, case_id, idempotency_key,
  correlation_id, request_hash, status_code, error_code, committed, replay, fault_index, fault_effect, fault_params, response_suppressed)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULL,$11,$12,false,false,$13,$14,$15::jsonb,$16)`,
		Micro(cmd.At), cmd.ScenarioID, cmd.Seed, cmd.FaultPlanID, cmd.Operation, cmd.CallNo,
		nullable(tenant), nullable(caseID), nullable(key), nullable(cmd.CorrelationID),
		status, nullable(errorCode), cmd.FaultIndex, nullable(cmd.FaultEffect), params, cmd.Suppressed)
	return err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func hashJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // a fixed struct of strings always marshals
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PayloadHash is the hash an attachment carries: SHA-256 of the exact bytes received, as "sha256:<hex>".
func PayloadHash(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func nextCounter(ctx context.Context, tx pgx.Tx, kind string) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `UPDATE counters SET value = value + 1 WHERE kind = $1 RETURNING value`, kind).Scan(&n)
	return n, err
}
