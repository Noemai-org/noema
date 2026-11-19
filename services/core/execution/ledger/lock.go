package ledger

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	spendLockStatement   = `SELECT pg_advisory_lock(hashtext($1), hashtext($2))`
	spendUnlockStatement = `SELECT pg_advisory_unlock(hashtext($1), hashtext($2))`
	txIdle               = 'I' // pgconn's transaction status of a connection that is in no transaction
)

// withSpendLock runs fn on one connection of the pool while it holds the advisory lock of the tenant's spends (noema#714, Architect ruling A339, relayed, interim). The
// spends of one tenant all append to the head of one Record chain, so without the lock the ones that overlap refuse one another with a serialization failure; with it they
// take turns, and a spend starts its transaction after the previous one committed.
//
// Why a session lock taken before the transaction, not pg_advisory_xact_lock as the transaction's first statement. A SERIALIZABLE transaction takes its snapshot at its first
// statement, and the lock call is a statement: a transaction that waited for the lock behind a committed writer would read the world as it was before that writer, and its
// first write to a row the writer changed would be refused with 40001, so the lock would serialise nothing (TestAnAdvisoryXactLockAsTheFirstStatementOfASerializable
// TransactionDoesNotRefreshItsSnapshot shows it on the database). The lock is therefore taken on the connection first, as a session lock, and the transactions run on that
// same connection; it is released, on every way out, before the connection goes back to the pool.
//
// The key. The two-key form of the advisory functions, (hashtext(SpendLockNamespace), hashtext(SpendLockName(tenant))). That form is a key space of its own in PostgreSQL
// (pg_locks shows it with objsubid 2; the one-key form has objsubid 1), and every other advisory lock of the system, the Record's, the policy publication's, the migrator's
// and the signing keys', uses the one-key form (grep pg_advisory in services/core), so none of them can be this lock. A hash collision of two tenants' names only makes
// their spends wait for each other. TestAOneKeyAdvisoryLockWithTheSameNumbersIsAnotherLock holds the one-key lock of the same two numbers and shows a spend does not wait.
//
// The wait is bounded by the context. The lock call runs on ctx, which carries the caller's deadline (inTxOn refuses a context without one): when it ends, the waiting call
// is cancelled and the connection is destroyed, so a spend gives up when its caller's time is up and leaves no waiter in the lock queue (TestTheWaitForTheLockEndsWithThe
// Context). A lock_timeout would need a SET statement of its own and a second bound next to the context's; a try-lock loop would poll and queue nobody.
//
// A connection that cannot be shown to hold no lock and no transaction is never returned to the pool: it is closed, and closing the session releases the lock. That covers
// a cancelled lock call (the lock may have been granted as the cancel arrived), an unlock that fails or finds nothing to release, and a transaction that was not rolled back.
//
// A waiting spend holds one connection of the pool, so a burst of spends of one tenant can occupy the pool; the holder needs no connection besides its own.
func (l *Ledger) withSpendLock(ctx context.Context, tenantID string, fn func(*pgxpool.Conn) error) error {
	if err := needDeadline(ctx); err != nil {
		return err
	}
	if l.RollbackTimeout <= 0 {
		return errNoRollbackBound
	}
	conn, err := l.Pool.Acquire(ctx)
	if err != nil {
		return &lockError{text: errPrefix + "no connection could be acquired for the spend lock", cause: err}
	}
	name := SpendLockName(tenantID)
	if _, err := conn.Exec(ctx, spendLockStatement, SpendLockNamespace, name); err != nil {
		l.destroy(ctx, conn)
		return &lockError{text: errPrefix + "the wait for the spend lock did not complete", cause: err}
	}
	ferr := fn(conn)
	uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), l.RollbackTimeout)
	defer cancel()
	var released bool
	if uerr := conn.QueryRow(uctx, spendUnlockStatement, SpendLockNamespace, name).Scan(&released); uerr != nil || !released || conn.Conn().PgConn().TxStatus() != txIdle {
		l.destroy(ctx, conn)
		if ferr == nil {
			ferr = &lockError{text: errPrefix + "the spend lock could not be released cleanly: the connection was closed", cause: uerr}
		}
		return ferr
	}
	conn.Release()
	return ferr
}

// destroy takes the connection out of the pool and closes it, which ends its session and with it every lock of the session. The close is bounded like a rollback.
func (l *Ledger) destroy(ctx context.Context, conn *pgxpool.Conn) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), l.RollbackTimeout)
	defer cancel()
	_ = conn.Hijack().Close(cctx)
}

// lockError is a failure of the spend lock as callers see it (AGENT-13): a fixed public text, with the cause kept for errors.Is and errors.As and never printed. The tenant's ID
// and the driver's message (server and login details) are not in the text. cause may be nil (an unlock that found nothing to release).
type lockError struct {
	text  string
	cause error
}

func (e *lockError) Error() string { return e.text }

func (e *lockError) Unwrap() error { return e.cause }
