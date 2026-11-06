package ledger

// A24 (ruling of 6 Nov 2026): a rollback after a cancelled context is bounded, context.WithTimeout(context.WithoutCancel(ctx), N), with N from
// configuration. It must still be tried when the caller's context is already done (otherwise a connection is returned to the pool inside a
// transaction), and it must not wait forever when the server does not answer. Everything is SYNTHETIC.

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type hungTx struct {
	sawDeadline bool
	sawCancel   bool
}

// Rollback blocks until the context it is given ends: a server that does not answer.
func (h *hungTx) Rollback(ctx context.Context) error {
	_, h.sawDeadline = ctx.Deadline()
	h.sawCancel = ctx.Err() != nil
	<-ctx.Done()
	return ctx.Err()
}

func TestAHungRollbackTimesOutAfterTheConfiguredBound(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel() // the caller's context is already cancelled: the rollback must still be tried
	h := &hungTx{}
	done := make(chan struct{})
	go func() { rollbackBounded(parent, h, 50*time.Millisecond); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hung rollback was never abandoned")
	}
	if !h.sawDeadline {
		t.Error("the rollback context has no deadline")
	}
	if h.sawCancel {
		t.Error("the rollback context was already cancelled: the caller's cancellation was inherited")
	}
}

type quickTx struct{ err error }

func (q quickTx) Rollback(context.Context) error { return q.err }

func TestARollbackThatAnswersReturnsAtOnce(t *testing.T) {
	done := make(chan struct{})
	go func() {
		rollbackBounded(context.Background(), quickTx{}, time.Minute)
		rollbackBounded(context.Background(), quickTx{err: errors.New("already closed")}, time.Minute)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("an answered rollback waited for its bound")
	}
}

func TestALedgerWithoutARollbackBoundRefusesToStartATransaction(t *testing.T) {
	l := &Ledger{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := l.inTx(ctx, pgx.ReadCommitted, nil)
	if err == nil || !errors.Is(err, errNoRollbackBound) {
		t.Fatalf("inTx with no rollback bound: %v, want errNoRollbackBound", err)
	}
}
