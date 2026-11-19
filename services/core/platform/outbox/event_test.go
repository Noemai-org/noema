package outbox_test

// REQ-VER-005, PERD §56 and §38.2: the one typed write path of an event. These tests need no database. Every refusal says what is wrong and never carries the payload
// (a payload is data and may hold identifiers: AGENT-13, INV-12). All data is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/outbox"
)

func goodEvent() outbox.Event {
	return outbox.Event{OutboxID: oid(1), TenantID: tenantA, Topic: topicV, Payload: payload(1), CreatedAt: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)}
}

func TestAWellFormedEventIsValid(t *testing.T) {
	if err := goodEvent().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, topic := range []string{"outcome.verify", "recovery.requested", "outcome.updated", "a.b.c", "x1.y_2"} {
		e := goodEvent()
		e.Topic = topic
		if err := e.Validate(); err != nil {
			t.Errorf("topic %q refused: %v", topic, err)
		}
	}
}

// Each malformed field is refused with ErrInvalidEvent, before any database call (a nil transaction is never touched).
func TestAMalformedEventIsRefusedBeforeTheDatabaseIsTouched(t *testing.T) {
	cases := map[string]func(*outbox.Event){
		"empty ID":                   func(e *outbox.Event) { e.OutboxID = "" },
		"short ID":                   func(e *outbox.Event) { e.OutboxID = "obx_1" },
		"wrong prefix":               func(e *outbox.Event) { e.OutboxID = "rcp_" + strings.Repeat("0", 26) },
		"lower-case ULID":            func(e *outbox.Event) { e.OutboxID = "obx_" + strings.Repeat("a", 26) },
		"25 characters":              func(e *outbox.Event) { e.OutboxID = "obx_" + strings.Repeat("0", 25) },
		"27 characters":              func(e *outbox.Event) { e.OutboxID = "obx_" + strings.Repeat("0", 27) },
		"a letter outside Crockford": func(e *outbox.Event) { e.OutboxID = "obx_" + strings.Repeat("0", 25) + "U" },
		"empty tenant":               func(e *outbox.Event) { e.TenantID = "" },
		"tenant of the wrong shape":  func(e *outbox.Event) { e.TenantID = "tenant a" },
		"empty topic":                func(e *outbox.Event) { e.Topic = "" },
		"topic without a dot":        func(e *outbox.Event) { e.Topic = "outcome" },
		"upper-case topic":           func(e *outbox.Event) { e.Topic = "Outcome.Verify" },
		"trailing dot":               func(e *outbox.Event) { e.Topic = "outcome." },
		"leading dot":                func(e *outbox.Event) { e.Topic = ".verify" },
		"empty segment":              func(e *outbox.Event) { e.Topic = "outcome..verify" },
		"topic with a space":         func(e *outbox.Event) { e.Topic = "outcome.verify " },
		"topic over 64 characters":   func(e *outbox.Event) { e.Topic = "a." + strings.Repeat("b", 63) },
		"nil payload":                func(e *outbox.Event) { e.Payload = nil },
		"empty payload":              func(e *outbox.Event) { e.Payload = []byte{} },
		"JSON null":                  func(e *outbox.Event) { e.Payload = []byte("null") },
		"JSON array":                 func(e *outbox.Event) { e.Payload = []byte(`[{"a":1}]`) },
		"JSON string":                func(e *outbox.Event) { e.Payload = []byte(`"` + secret + `"`) },
		"JSON number":                func(e *outbox.Event) { e.Payload = []byte("12") },
		"unterminated object":        func(e *outbox.Event) { e.Payload = []byte(`{"note":"` + secret + `"`) },
		"trailing bytes":             func(e *outbox.Event) { e.Payload = []byte(`{"a":1}` + secret) },
		"zero time":                  func(e *outbox.Event) { e.CreatedAt = time.Time{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := goodEvent()
			mutate(&e)
			if err := e.Validate(); !errors.Is(err, outbox.ErrInvalidEvent) {
				t.Fatalf("Validate = %v, want ErrInvalidEvent", err)
			}
			c, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := outbox.Enqueue(c, nil, e)
			if !errors.Is(err, outbox.ErrInvalidEvent) {
				t.Fatalf("Enqueue = %v, want ErrInvalidEvent without touching the transaction", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("the error carries the payload: %q", err)
			}
		})
	}
}

// A context deadline on every call (AGENTS section 7, Go).
func TestEnqueueRefusesAContextWithoutADeadline(t *testing.T) {
	if err := outbox.Enqueue(context.Background(), nil, goodEvent()); !errors.Is(err, outbox.ErrNoDeadline) {
		t.Fatalf("Enqueue = %v, want ErrNoDeadline", err)
	}
}

// Validate reads the event and changes nothing in it.
func TestValidateDoesNotChangeTheEvent(t *testing.T) {
	e := goodEvent()
	before := string(e.Payload)
	_ = e.Validate()
	if string(e.Payload) != before {
		t.Fatal("Validate changed the payload")
	}
}

// The Delivery's idempotency key is the outbox ID, and nothing else: "consumers are idempotent by outbox_id or event_id" (PERD §56).
func TestTheIdempotencyKeyOfADeliveryIsTheOutboxID(t *testing.T) {
	d := outbox.Delivery{OutboxID: oid(7), TenantID: tenantA, Topic: topicV}
	if d.IdempotencyKey() != oid(7) {
		t.Fatalf("IdempotencyKey = %q, want %q", d.IdempotencyKey(), oid(7))
	}
}
