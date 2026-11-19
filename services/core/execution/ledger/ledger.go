package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	errPrefix = "ledger: "
	writer    = "core-execution"

	pgUniqueViolation      = "23505"
	pgSerializationFailure = "40001"
	pgDeadlockDetected     = "40P01"
	pgSignatureNotVerified = "NS001" // DEC-2026-11-01 D12: the database refused a signature

	statusIssued = "ISSUED"
	statusSpent  = "SPENT"
	statusVoid   = "VOID"
)

// IDSource makes the prefixed ULIDs of PERD 32.3.
type IDSource interface {
	ID(prefix string) (string, error)
}

// Ledger is the Postgres implementation of controller.Ledger, controller.GrantSource and the kill-switch read of
// controller.Binder. Its Pool must run as the execution_rw role (db.OpenAsRole), so that the database, not this
// code, bounds what Execution may write (REQ-DATA-001). Every method is one transaction; the Record events a step
// carries are appended inside it, so a failed append fails the step and nothing is dispatched (PERD 32.4, EC-E16).
type Ledger struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	IDs   IDSource
	Rec   *chain.Appender
	// RollbackTimeout bounds the rollback of a failed transaction after the caller's context has ended (ruling A24). It comes from configuration and
	// has no default: a Ledger without one refuses to start a transaction.
	RollbackTimeout time.Duration
	// SerializationAttempts is the total number of attempts Spend makes when the database refuses a transaction with a serialization failure (SQLSTATE 40001),
	// the first included (noema#714, Architect rulings A317 to A328, relayed). It comes from the environment setting named by EnvSerializationAttempts and has no default
	// (AGENT-16): a Ledger without a value of at least one refuses to spend.
	SerializationAttempts int
	// Log receives the one line of fixed text that Spend writes when the spend lock could not be released cleanly (noema#748, Architect ruling A348 as refined, relayed,
	// interim). Optional: nil logs nothing. The composition root passes a logger that runs through the mandatory redactor (platform/redaction, PERD 44.2).
	Log *slog.Logger
	// Boundary is called at each boundary inside a transaction (see Point). Production leaves it nil; the crash harness returns ErrCrash.
	Boundary func(Point) error
}

var (
	_ controller.Ledger      = (*Ledger)(nil)
	_ controller.GrantSource = (*Ledger)(nil)
)

// SpendLockNamespace and SpendLockName(tenant) are the two texts whose hashtext values are the two keys of the advisory lock that serialises the spends of one tenant. The
// two-key form of PostgreSQL's advisory locks is a key space of its own: no lock of the one-key form, which every other advisory lock of the system uses, can ever be the same
// lock. Within the two-key form the first key names this use, so no other use can be the same lock unless it picked the same text.
const SpendLockNamespace = "noema:spend-lock"

// SpendLockName is the second text: one per tenant, so that the spends of two tenants never wait for each other.
func SpendLockName(tenantID string) string { return "noema:spend:" + tenantID }

var errNoRollbackBound = newFixed("RollbackTimeout is required: a rollback after a cancelled context must be bounded")

// rolledBack is what rollbackBounded needs of a transaction.
type rolledBack interface {
	Rollback(ctx context.Context) error
}

// rollbackBounded rolls tx back with context.WithTimeout(context.WithoutCancel(ctx), bound): the rollback is still tried when the caller's context is
// already done, so no connection returns to the pool inside a transaction, and it is abandoned after the bound when the server does not answer (A24).
// A rollback that fails is not an error of the step: the step's own error is the one to report, and the server rolls back a dropped session.
func rollbackBounded(ctx context.Context, tx rolledBack, bound time.Duration) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bound)
	defer cancel()
	_ = tx.Rollback(rctx)
}

var errNoDeadline = newFixed("the context needs a deadline")

func needDeadline(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errNoDeadline
	}
	return nil
}

// txStarter is what inTxOn needs to begin a transaction: a pool, or one connection taken from it.
type txStarter interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// inTx runs fn in a transaction at the given isolation level. fn returning a nil error commits; any error rolls back.
func (l *Ledger) inTx(ctx context.Context, level pgx.TxIsoLevel, fn func(pgx.Tx) error) error {
	return l.inTxOn(ctx, l.Pool, level, fn)
}

// inTxOn is inTx on a given pool or connection.
func (l *Ledger) inTxOn(ctx context.Context, from txStarter, level pgx.TxIsoLevel, fn func(pgx.Tx) error) error {
	if err := needDeadline(ctx); err != nil {
		return err
	}
	if l.RollbackTimeout <= 0 {
		return errNoRollbackBound
	}
	tx, err := from.BeginTx(ctx, pgx.TxOptions{IsoLevel: level})
	if err != nil {
		return fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	if err := fn(tx); err != nil {
		rollbackBounded(ctx, tx, l.RollbackTimeout)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%scommit: %w", errPrefix, err)
	}
	return nil
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// Load implements controller.GrantSource: the stored grant and its signature. A grant that does not exist is an
// error, not a void. A stored grant with a field the controller does not know is an error too: the signature
// covers the fields it knows, so an extra field could never have been verified.
func (l *Ledger) Load(ctx context.Context, tenantID, grantID string) (controller.SignedGrant, error) {
	if err := needDeadline(ctx); err != nil {
		return controller.SignedGrant{}, err
	}
	var raw []byte
	var keyID, sig string
	err := l.Pool.QueryRow(ctx, `SELECT json, sig_key_id, sig_value FROM decision_grant WHERE tenant_id = $1 AND grant_id = $2`,
		tenantID, grantID).Scan(&raw, &keyID, &sig)
	if errors.Is(err, pgx.ErrNoRows) {
		return controller.SignedGrant{}, fmt.Errorf("%sgrant %s does not exist for tenant %s", errPrefix, grantID, tenantID)
	}
	if err != nil {
		return controller.SignedGrant{}, fmt.Errorf("%sload grant: %w", errPrefix, err)
	}
	var g controller.Grant
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return controller.SignedGrant{}, fmt.Errorf("%sdecode grant %s: %w", errPrefix, grantID, err)
	}
	return controller.SignedGrant{Grant: g, Sig: controller.Sig{KeyID: keyID, Value: sig}}, nil
}

// WritesEnabled reads the tenant kill switch (E6). An unknown tenant is an error, which the controller treats as
// "writes disabled" (REQ-EXE-008, fail closed).
func (l *Ledger) WritesEnabled(ctx context.Context, tenantID string) (bool, error) {
	if err := needDeadline(ctx); err != nil {
		return false, err
	}
	var on bool
	if err := l.Pool.QueryRow(ctx, `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenantID).Scan(&on); err != nil {
		return false, fmt.Errorf("%sread kill switch: %w", errPrefix, err)
	}
	return !on, nil
}

// correlation reads the correlation ID the grant carries; every Record event of a grant's life shares it.
func correlation(ctx context.Context, tx pgx.Tx, tenantID, grantID string) (string, error) {
	var corr *string
	err := tx.QueryRow(ctx, `SELECT json->>'correlation_id' FROM decision_grant WHERE tenant_id = $1 AND grant_id = $2`, tenantID, grantID).Scan(&corr)
	if err != nil {
		return "", fmt.Errorf("%sread correlation of %s: %w", errPrefix, grantID, err)
	}
	if corr == nil || *corr == "" {
		return "", fmt.Errorf("%sgrant %s carries no correlation_id", errPrefix, grantID)
	}
	return *corr, nil
}

func (l *Ledger) record(ctx context.Context, tx pgx.Tx, tenantID, corr, grantID, eventType, kind, reason string, payload any) error {
	_, err := l.Rec.Append(ctx, tx, chain.Event{TenantID: tenantID, CorrelationID: corr, WrittenBy: writer, Actor: writer, EventType: eventType,
		Kind: kind, Reason: reason, ObjectRef: grantID, Payload: payload})
	return err
}

// Append implements controller.Ledger: Record events outside a spend, for example REVALIDATION_PASSED, in one transaction.
func (l *Ledger) Append(ctx context.Context, tenantID string, events ...controller.Event) error {
	return l.inTx(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		for _, e := range events {
			corr, err := correlation(ctx, tx, tenantID, e.GrantID)
			if err != nil {
				return err
			}
			var payload any
			switch {
			case e.Payload != nil:
				payload = e.Payload
			case e.Detail != "" || e.PolicyVersion != "":
				p := map[string]any{}
				if e.Detail != "" {
					p["detail"] = e.Detail
				}
				if e.PolicyVersion != "" {
					p["policy_version"] = e.PolicyVersion
				}
				payload = p
			}
			if err := l.record(ctx, tx, tenantID, corr, e.GrantID, e.Type, e.Kind, "", payload); err != nil {
				return err
			}
		}
		return nil
	})
}
