package ledger

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// EnvSerializationAttempts names the environment setting that holds Ledger.SerializationAttempts: the total number of attempts Spend makes when the database refuses
// a spend with a serialization failure, the first included (noema#714, Architect rulings A317 to A328, relayed, interim). The composition root that builds the Ledger
// reads it with ParseSerializationAttempts. It has no default (AGENT-16).
const EnvSerializationAttempts = "NOEMA_DB_SERIALIZATION_ATTEMPTS"

var errNoSerializationAttempts = errors.New(errPrefix + EnvSerializationAttempts + " is required: SerializationAttempts must be at least 1, and has no default")

// attemptsPattern is a positive decimal integer with no sign, no leading zero and no space around it.
var attemptsPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// ParseSerializationAttempts reads the value of EnvSerializationAttempts. A missing, empty or invalid value is an error that names the setting, so that the process
// refuses to configure the ledger instead of falling back to a number of its own (AGENT-16).
func ParseSerializationAttempts(raw string) (int, error) {
	if !attemptsPattern.MatchString(raw) {
		return 0, fmt.Errorf("%s%s must be a whole number of at least 1 (no default), got %q", errPrefix, EnvSerializationAttempts, raw)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s%s is out of range, got %q: %w", errPrefix, EnvSerializationAttempts, raw, err)
	}
	return n, nil
}

// spendWithRetry takes the tenant's spend lock (withSpendLock) and, holding it, runs the whole spend transaction, and runs it again, as a fresh transaction, while the
// database refuses it with a serialization failure (SQLSTATE 40001) and attempts remain. A serialization failure, at a statement or at the commit, rolled the transaction
// back: nothing was spent, written or dispatched, so trying again is safe, and the first statement of the new transaction reads the grant again, so a grant that an earlier
// winner spent is answered as a replay. No other error is retried (a unique violation, a deadlock and every other error end the call as before). The context bounds the wait
// for the lock and all the attempts: no attempt starts once it is done. There is no pause between attempts: the transaction that made this one fail has already committed.
//
// The lock removes the failure for the spends of one tenant, which are the ones that raced each other on the Record chain head; the retry covers the writers that do not take
// the lock (Trust's and Execution's other appends) and any other serialization failure.
func (l *Ledger) spendWithRetry(ctx context.Context, cmd controller.SpendCommand) (controller.SpendResult, error) {
	var res controller.SpendResult
	err := l.withSpendLock(ctx, cmd.Grant.TenantID, func(conn *pgxpool.Conn) error {
		var err error
		for attempt := 1; ; attempt++ {
			res = controller.SpendResult{}
			err = l.inTxOn(ctx, conn, pgx.Serializable, func(tx pgx.Tx) error {
				r, serr := l.spend(ctx, tx, cmd)
				res = r
				if serr == nil && !r.Spent {
					return errNothingWritten
				}
				return serr
			})
			if pgCode(err) != pgSerializationFailure || attempt >= l.SerializationAttempts || ctx.Err() != nil {
				return err
			}
		}
	})
	return res, err
}
