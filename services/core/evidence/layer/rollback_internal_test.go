package layer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// hungTx is a transaction whose rollback never returns until its context ends.
type hungTx struct{ pgx.Tx }

func (hungTx) Rollback(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// A24, AGENTS.md section 7 (review of noema#172): a rollback after a cancelled context is bounded by context.WithTimeout(context.WithoutCancel(ctx), N), with N from
// configuration, and a hung rollback times out and is reported with the original error.
func TestAHungRollbackTimesOutAtTheConfiguredBoundAndKeepsTheOriginalError(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	cancel() // the caller's own context has already ended, as when a request deadline passed
	cause := errors.New("original failure")
	limit := 50 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- rollbackWith(caller, hungTx{}, cause, limit) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("a hung rollback was not bounded by its %s limit", limit)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("the original error was lost: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the rollback's own timeout is not reported: %v", err)
	}
}
