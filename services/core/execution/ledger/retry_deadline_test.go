package ledger_test

// A776 (C1, interim, relayed), REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-08: the context deadline bounds the spend attempts, proved on a fake clock. The earlier form of this
// test used a 500 ms wall-clock timeout, a 50 ms server sleep per attempt, a 300 ms real settle wait and a 10 s real guard: on a loaded runner the deadline could end before the first
// INSERT was issued, or the settle wait could miss a late statement, so the test failed with the code unchanged. Here the deadline is on a fake clock (platform/clock.Manual) that the
// test advances when the client issues its third INSERT: the order of events does not depend on how fast the machine or the server is. Everything is SYNTHETIC.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// clockDeadline is a context whose deadline is an instant of a fake clock (Deadline reports it: the ledger requires a context with a deadline). It ends when the clock reaches the instant and Check is called (the test calls it right after it advances the
// clock).
type clockDeadline struct {
	clk  *clock.Manual
	at   time.Time
	mu   sync.Mutex
	done chan struct{}
	err  error
}

func newClockDeadline(clk *clock.Manual, d time.Duration) *clockDeadline {
	return &clockDeadline{clk: clk, at: clk.Now().Add(d), done: make(chan struct{})}
}

func (c *clockDeadline) Check() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil && !c.clk.Now().Before(c.at) {
		c.err = context.DeadlineExceeded
		close(c.done)
	}
}

func (c *clockDeadline) Deadline() (time.Time, bool) { return c.at, true }
func (c *clockDeadline) Done() <-chan struct{}       { return c.done }
func (c *clockDeadline) Value(any) any               { return nil }
func (c *clockDeadline) Err() error                  { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *clockDeadline) ended() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// The deadline, a fake-clock instant, ends the spend: with a thousand attempts allowed and every one refused with 40001, the spend returns an error when the clock passes the
// deadline, after a bounded number of attempts and with no statement of the client after the return (ruling A611: every statement the client issues is counted, not only the INSERT).
func TestTheContextDeadlineOnAFakeClockStopsTheAttempts(t *testing.T) {
	w := newWorld(t)
	faultOnSpend(t, w, "40001", 1000000, 0)
	pool, issued := poolCountingStatements(t, w)
	led, _ := ledgerOn(t, w, pool)
	led.SerializationAttempts = 1000
	g := w.issue(t, 1)

	clk := clock.NewManual(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)) // far in the real future, so a driver that reads Deadline never sees it as past
	dl := newClockDeadline(clk, 500*time.Millisecond)
	const deadlineAtInsert = 3
	issued.onInsert = func(n int64) {
		if n == deadlineAtInsert {
			clk.Advance(time.Second) // the client has issued its third INSERT: the fake clock passes the deadline
			dl.Check()
		}
	}

	res, err := led.Spend(dl, cmdFor(g))
	if err == nil || res.Spent {
		t.Fatalf("Spend = %+v, %v; want an error", res, err)
	}
	if !isDidNotCommit40001(err) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("the error is neither the did-not-commit error nor the context's: %v", err)
	}
	if !dl.ended() {
		t.Errorf("the deadline never ended the context although the fake clock passed it")
	}
	atReturn, insertsAtReturn := issued.all.Load(), issued.inserts.Load()
	if insertsAtReturn < deadlineAtInsert || insertsAtReturn > deadlineAtInsert+1 {
		t.Fatalf("%d spend attempts reached their INSERT; want the %d that ran up to the deadline and at most one more", insertsAtReturn, deadlineAtInsert)
	}
	pool.Close() // returns when every connection is released: nothing of the client is left to issue a statement
	if later := issued.all.Load(); later != atReturn {
		t.Fatalf("the client issued %d statements after Spend returned (%d at the return, %d later)", later-atReturn, atReturn, later)
	}
}
