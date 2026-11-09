package ledger

// REQ-EXE-004, INV-07, EC-E16 (a transaction left open holds a connection and the Record-first rule). A24 (ruling of 6 Nov 2026): a rollback after a cancelled context is bounded, context.WithTimeout(context.WithoutCancel(ctx), N), with N from
// configuration. It must still be tried when the caller's context is already done (otherwise a connection is returned to the pool inside a
// transaction), and it must not wait forever when the server does not answer. Everything is SYNTHETIC.

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// stallRelay is a TCP relay in front of the test database. After stall() it reads and discards both directions: the server never answers, and the
// client's connection stays open, which is how a partitioned database looks to a driver. The fault is on the real connection, not in a stand-in.
type stallRelay struct {
	ln      net.Listener
	stalled atomic.Bool
}

func newStallRelay(t *testing.T, target string) *stallRelay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &stallRelay{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", target)
			if err != nil {
				_ = c.Close()
				continue
			}
			t.Cleanup(func() { _ = c.Close(); _ = up.Close() })
			go r.pipe(c, up)
			go r.pipe(up, c)
		}
	}()
	return r
}

func (r *stallRelay) pipe(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 && !r.stalled.Load() {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (r *stallRelay) stall() { r.stalled.Store(true) }

// REQ-EXE-004, INV-07, EC-E16, A24: against a real PostgreSQL connection whose server stops answering, a failed step's rollback is abandoned after
// the configured bound, even though the caller's own context is far from its deadline. The step cancels its context after the stall, as a
// shutdown or a request timeout would, so the rollback also runs on a cancelled parent.
func TestAHungRollbackOnARealConnectionTimesOutAfterTheConfiguredBound(t *testing.T) {
	_, dsn := testpg.NewDatabase(t)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	relay := newStallRelay(t, u.Host)
	u.Host = relay.ln.Addr().String()
	parent, cancelParent := context.WithTimeout(context.Background(), time.Minute)
	defer cancelParent()
	pool, err := pgxpool.New(parent, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { go pool.Close() }() // Close waits for the stalled connection; the test has what it needs by then
	l := &Ledger{Pool: pool, RollbackTimeout: 300 * time.Millisecond}
	stepCtx, cancelStep := context.WithCancel(parent)
	result := make(chan error, 1)
	go func() {
		result <- l.inTx(stepCtx, pgx.ReadCommitted, func(tx pgx.Tx) error {
			if _, err := tx.Exec(stepCtx, `SELECT 1`); err != nil {
				return err
			}
			relay.stall()
			cancelStep()
			return errors.New("the step failed")
		})
	}()
	select {
	case err := <-result:
		if err == nil || err.Error() != "the step failed" {
			t.Fatalf("inTx = %v, want the step's own error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a rollback on a stalled real connection was never abandoned")
	}
}

type quickTx struct{ err error }

func (q quickTx) Rollback(context.Context) error { return q.err }

// REQ-EXE-004, INV-07, EC-E16, A24.
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

// REQ-EXE-004, INV-07, EC-E16, A24.
func TestALedgerWithoutARollbackBoundRefusesToStartATransaction(t *testing.T) {
	l := &Ledger{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := l.inTx(ctx, pgx.ReadCommitted, nil)
	if err == nil || !errors.Is(err, errNoRollbackBound) {
		t.Fatalf("inTx with no rollback bound: %v, want errNoRollbackBound", err)
	}
}
