package outbox_test

// PLT-08, REQ-VER-005, PERD §56 ("a dispatcher delivers at least once, and consumers are idempotent by outbox_id or event_id"), INV-12, INV-30, AGENT-08: the dispatcher
// on a real PostgreSQL server. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/outbox"
)

// The event is marked dispatched after its delivery, never before, and with the injected clock's instant.
func TestAnEventIsMarkedDispatchedOnlyAfterItIsDelivered(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	var during *time.Time
	ran := false
	h := outbox.HandlerFunc(func(c context.Context, d outbox.Delivery) error {
		ran = true
		return w.admin.QueryRow(c, `SELECT dispatched_at FROM outbox_event WHERE outbox_id = $1`, d.OutboxID).Scan(&during)
	})
	at := time.Date(2026, 11, 17, 12, 30, 0, 0, time.UTC)
	w.clk.Advance(at.Sub(w.clk.Now()))
	res, err := w.dispatcher(w.orchA, tenantA, h, 10).RunOnce(ctx(t))
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !ran || during != nil {
		t.Fatalf("while the handler ran, dispatched_at was %v (handler ran: %v), want NULL", during, ran)
	}
	if len(res.Delivered) != 1 || res.Delivered[0] != oid(1) || len(res.Failed) != 0 {
		t.Fatalf("result %+v", res)
	}
	var after time.Time
	if err := w.admin.QueryRow(ctx(t), `SELECT dispatched_at FROM outbox_event WHERE outbox_id = $1`, oid(1)).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(at) {
		t.Fatalf("dispatched_at = %v, want the clock's %v", after, at)
	}
}

// Oldest first, one delivery per event, the outbox ID as the key, and everything the consumer needs.
func TestEventsAreDeliveredOldestFirstWithTheOutboxIDAsTheKey(t *testing.T) {
	w := newWorld(t)
	for _, n := range []int{3, 1, 2} { // created in this order, so created_at order is 3, 1, 2
		w.enqueue(w.execA, tenantA, n, topicV)
	}
	rec := &recorder{}
	res, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{oid(3), oid(1), oid(2)}
	if got := rec.ids(); strings.Join(got, ",") != strings.Join(want, ",") || strings.Join(res.Delivered, ",") != strings.Join(want, ",") {
		t.Fatalf("delivered %v, result %v, want %v", got, res.Delivered, want)
	}
	d := rec.seen[0]
	if d.IdempotencyKey() != oid(3) || d.TenantID != tenantA || d.Topic != topicV || d.CreatedAt.IsZero() {
		t.Fatalf("delivery %+v", d)
	}
	var obj map[string]any
	if err := json.Unmarshal(d.Payload, &obj); err != nil || obj["note"] != secret {
		t.Fatalf("payload %s (%v)", d.Payload, err)
	}
	if left := w.pending(tenantA); len(left) != 0 {
		t.Fatalf("still pending: %v", left)
	}
	// Nothing is delivered twice by a second pass.
	if _, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t)); err != nil || len(rec.ids()) != 3 {
		t.Fatalf("second pass: %v, %d deliveries in all, want 3", err, len(rec.ids()))
	}
}

// A handler that fails leaves its event pending; the other events of the pass go on; the failure is reported without the payload and with its cause reachable.
func TestAFailingHandlerLeavesTheEventPendingAndTheOthersGoOn(t *testing.T) {
	w := newWorld(t)
	for n := 1; n <= 3; n++ {
		w.enqueue(w.execA, tenantA, n, topicV)
	}
	cause := errors.New("target said no: " + secret)
	h := outbox.HandlerFunc(func(_ context.Context, d outbox.Delivery) error {
		if d.OutboxID == oid(2) {
			return cause
		}
		return nil
	})
	res, err := w.dispatcher(w.orchA, tenantA, h, 10).RunOnce(ctx(t))
	if !errors.Is(err, outbox.ErrDeliveryFailed) || !errors.Is(err, cause) {
		t.Fatalf("RunOnce = %v, want ErrDeliveryFailed with the cause reachable", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error text carries the payload or the cause text: %q", err)
	}
	if strings.Join(res.Delivered, ",") != oid(1)+","+oid(3) || len(res.Failed) != 1 || res.Failed[0].OutboxID != oid(2) || res.Failed[0].Topic != topicV {
		t.Fatalf("result %+v", res)
	}
	if got := w.pending(tenantA); len(got) != 1 || got[0] != oid(2) {
		t.Fatalf("pending = %v, want only %s", got, oid(2))
	}
}

// A failing event is tried once per pass: a poison event cannot hold a pass in a loop.
func TestAFailingEventIsTriedOncePerPass(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	rec := &recorder{err: errors.New("down")}
	if _, err := w.dispatcher(w.orchA, tenantA, rec, 50).RunOnce(ctx(t)); !errors.Is(err, outbox.ErrDeliveryFailed) {
		t.Fatalf("RunOnce = %v", err)
	}
	if n := len(rec.ids()); n != 1 {
		t.Fatalf("%d deliveries in one pass, want 1", n)
	}
}

// At least once: a failed delivery is delivered again on the next pass with the same key, and then marked.
func TestAFailedDeliveryIsDeliveredAgainWithTheSameKey(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	rec := &recorder{err: errors.New("down")}
	d := w.dispatcher(w.orchA, tenantA, rec, 10)
	if _, err := d.RunOnce(ctx(t)); err == nil {
		t.Fatal("no error")
	}
	rec.mu.Lock()
	rec.err = nil
	rec.mu.Unlock()
	if _, err := d.RunOnce(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if got := rec.ids(); len(got) != 2 || got[0] != oid(1) || got[1] != oid(1) {
		t.Fatalf("deliveries %v, want the same ID twice", got)
	}
	if left := w.pending(tenantA); len(left) != 0 {
		t.Fatalf("still pending: %v", left)
	}
}

// An event whose topic has no handler is never marked: nobody received it. The events with a handler go on.
func TestATopicWithoutAHandlerStaysPending(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, "recovery.requested") // no handler configured for it
	w.enqueue(w.execA, tenantA, 2, topicV)
	rec := &recorder{}
	res, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t))
	if !errors.Is(err, outbox.ErrDeliveryFailed) || !errors.Is(err, outbox.ErrNoHandler) {
		t.Fatalf("RunOnce = %v, want ErrDeliveryFailed wrapping ErrNoHandler", err)
	}
	if len(res.Failed) != 1 || res.Failed[0].OutboxID != oid(1) || strings.Join(res.Delivered, ",") != oid(2) {
		t.Fatalf("result %+v", res)
	}
	if got := w.pending(tenantA); len(got) != 1 || got[0] != oid(1) {
		t.Fatalf("pending = %v, want the event without a handler", got)
	}
}

// A pass is bounded by MaxEvents; the rest waits for the next pass.
func TestAPassDeliversAtMostMaxEvents(t *testing.T) {
	w := newWorld(t)
	for n := 1; n <= 5; n++ {
		w.enqueue(w.execA, tenantA, n, topicV)
	}
	rec := &recorder{}
	d := w.dispatcher(w.orchA, tenantA, rec, 2)
	for i, want := range []int{2, 2, 1, 0} {
		res, err := d.RunOnce(ctx(t))
		if err != nil || len(res.Delivered) != want {
			t.Fatalf("pass %d: %d delivered (%v), want %d", i+1, len(res.Delivered), err, want)
		}
	}
	if got := rec.ids(); len(got) != 5 {
		t.Fatalf("deliveries %v", got)
	}
}

// A handler that outlives DeliveryTimeout is cut off by its context; the event stays pending and the failure is reported.
func TestAHandlerThatOutlivesTheTimeoutFailsAndTheEventStaysPending(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	h := outbox.HandlerFunc(func(c context.Context, _ outbox.Delivery) error {
		<-c.Done()
		return c.Err()
	})
	d, err := outbox.New(w.orchA, w.clk, outbox.Config{TenantID: tenantA, Handlers: map[string]outbox.Handler{topicV: h}, MaxEvents: 5, DeliveryTimeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.RunOnce(ctx(t))
	if !errors.Is(err, outbox.ErrDeliveryFailed) || !errors.Is(err, context.DeadlineExceeded) || len(res.Failed) != 1 {
		t.Fatalf("RunOnce = %v, %+v", err, res)
	}
	if got := w.pending(tenantA); len(got) != 1 {
		t.Fatalf("pending = %v", got)
	}
}

// Two dispatchers do not deliver one event together: a row a dispatcher holds is skipped by the other (SKIP LOCKED), so with no crash an event is delivered once.
func TestASecondDispatcherSkipsAnEventTheFirstIsDelivering(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	inside, release := make(chan struct{}), make(chan struct{})
	slow := outbox.HandlerFunc(func(context.Context, outbox.Delivery) error {
		close(inside)
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := w.dispatcher(w.orchA, tenantA, slow, 10).RunOnce(ctx(t))
		done <- err
	}()
	<-inside
	rec := &recorder{}
	res, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t))
	if err != nil || len(res.Delivered) != 0 || len(rec.ids()) != 0 {
		t.Fatalf("the second dispatcher: %v, %+v, %v; want it to skip the held event", err, res, rec.ids())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first dispatcher: %v", err)
	}
	if left := w.pending(tenantA); len(left) != 0 {
		t.Fatalf("still pending: %v", left)
	}
}

// Several dispatchers over many events: every event is delivered, and when nothing crashes, once.
func TestManyDispatchersDeliverEveryEventExactlyOnceWhenNothingCrashes(t *testing.T) {
	w := newWorld(t)
	const events = 24
	for n := 1; n <= events; n++ {
		w.enqueue(w.execA, tenantA, n, topicV)
	}
	var mu sync.Mutex
	counts := map[string]int{}
	h := outbox.HandlerFunc(func(_ context.Context, d outbox.Delivery) error {
		mu.Lock()
		counts[d.OutboxID]++
		mu.Unlock()
		return nil
	})
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.dispatcher(w.orchA, tenantA, h, events).RunOnce(ctx(t))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a dispatcher: %v", err)
		}
	}
	if len(counts) != events {
		t.Fatalf("%d events delivered, want %d", len(counts), events)
	}
	for id, n := range counts {
		if n != 1 {
			t.Errorf("%s delivered %d times", id, n)
		}
	}
	if left := w.pending(tenantA); len(left) != 0 {
		t.Fatalf("still pending: %v", left)
	}
}

// Nothing pending: nothing delivered, no error.
func TestAPassWithNothingPendingDeliversNothing(t *testing.T) {
	w := newWorld(t)
	rec := &recorder{}
	res, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t))
	if err != nil || len(res.Delivered) != 0 || len(res.Failed) != 0 || len(rec.ids()) != 0 {
		t.Fatalf("RunOnce = %+v, %v", res, err)
	}
}

// INV-12, REQ-SEC-004: a dispatcher on tenant A's login delivers tenant A's events only, and marks only those.
func TestADispatcherDeliversOnlyItsOwnTenantsEvents(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	w.enqueue(w.execB, tenantB, 2, topicV)
	rec := &recorder{}
	if _, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if got := rec.ids(); len(got) != 1 || got[0] != oid(1) || rec.seen[0].TenantID != tenantA {
		t.Fatalf("delivered %v", got)
	}
	if got := w.pending(tenantB); len(got) != 1 || got[0] != oid(2) {
		t.Fatalf("tenant B's event: pending = %v, want it untouched", got)
	}
}

// A dispatcher configured for tenant B on tenant A's login finds nothing (the login's row security and the tenant filter both say no): it never delivers a foreign event.
func TestADispatcherOnTheWrongLoginDeliversNothing(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	w.enqueue(w.execB, tenantB, 2, topicV)
	rec := &recorder{}
	res, err := w.dispatcher(w.orchA, tenantB, rec, 10).RunOnce(ctx(t))
	if err != nil || len(res.Delivered) != 0 || len(rec.ids()) != 0 {
		t.Fatalf("RunOnce = %+v, %v, %v", res, err, rec.ids())
	}
	if n := len(w.pending(tenantA)) + len(w.pending(tenantB)); n != 2 {
		t.Fatalf("%d pending, want both untouched", n)
	}
}

// Tenant A's orchestrator login cannot see or mark tenant B's event by hand either (the database, not the dispatcher, is the boundary).
func TestAnOrchestratorLoginCannotSeeOrMarkAnotherTenantsEvent(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execB, tenantB, 1, topicV)
	var n int
	if err := w.orchA.QueryRow(ctx(t), `SELECT count(*) FROM outbox_event`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("tenant A's login sees %d events (%v), want 0", n, err)
	}
	tag, err := w.orchA.Exec(ctx(t), `UPDATE outbox_event SET dispatched_at = now() WHERE tenant_id = $1`, tenantB)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("update: %v, %d rows, want none", err, tag.RowsAffected())
	}
	if got := w.pending(tenantB); len(got) != 1 {
		t.Fatalf("pending = %v", got)
	}
}
