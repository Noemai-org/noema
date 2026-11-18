package outbox_test

// PLT-08, REQ-AGT-005, AGENT-16: the dispatcher's budget and timeout are configuration with no default (an agent never invents a retry budget or a timeout), and a
// handler table that could drop an event silently is refused. These tests need no database.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/outbox"
)

func okHandler() outbox.Handler {
	return outbox.HandlerFunc(func(context.Context, outbox.Delivery) error { return nil })
}

func goodConfig() outbox.Config {
	return outbox.Config{TenantID: tenantA, Handlers: map[string]outbox.Handler{topicV: okHandler()}, MaxEvents: 10, DeliveryTimeout: time.Second}
}

func TestADispatcherNeedsACompleteConfiguration(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC))
	cases := map[string]func(*outbox.Config){
		"no tenant":             func(c *outbox.Config) { c.TenantID = "" },
		"tenant of a bad shape": func(c *outbox.Config) { c.TenantID = "a" },
		"no handlers":           func(c *outbox.Config) { c.Handlers = nil },
		"empty handlers":        func(c *outbox.Config) { c.Handlers = map[string]outbox.Handler{} },
		"a nil handler":         func(c *outbox.Config) { c.Handlers = map[string]outbox.Handler{topicV: nil} },
		"a bad topic key":       func(c *outbox.Config) { c.Handlers = map[string]outbox.Handler{"Outcome": okHandler()} },
		"zero MaxEvents":        func(c *outbox.Config) { c.MaxEvents = 0 },
		"negative MaxEvents":    func(c *outbox.Config) { c.MaxEvents = -1 },
		"zero timeout":          func(c *outbox.Config) { c.DeliveryTimeout = 0 },
		"negative timeout":      func(c *outbox.Config) { c.DeliveryTimeout = -time.Second },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := goodConfig()
			mutate(&cfg)
			d, err := outbox.New(nopBeginner{}, clk, cfg)
			if !errors.Is(err, outbox.ErrConfig) || d != nil {
				t.Fatalf("New = %v, %v; want ErrConfig and no dispatcher", d, err)
			}
		})
	}
	if _, err := outbox.New(nil, clk, goodConfig()); !errors.Is(err, outbox.ErrConfig) {
		t.Fatalf("a nil database: %v, want ErrConfig", err)
	}
	if _, err := outbox.New(nopBeginner{}, nil, goodConfig()); !errors.Is(err, outbox.ErrConfig) {
		t.Fatalf("a nil clock: %v, want ErrConfig", err)
	}
	if _, err := outbox.New(nopBeginner{}, clk, goodConfig()); err != nil {
		t.Fatalf("a complete configuration: %v", err)
	}
}

// A context without a deadline is refused before any database call.
func TestRunOnceRefusesAContextWithoutADeadline(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC))
	d, err := outbox.New(nopBeginner{}, clk, goodConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RunOnce(context.Background()); !errors.Is(err, outbox.ErrNoDeadline) {
		t.Fatalf("RunOnce = %v, want ErrNoDeadline", err)
	}
}

// DeliveryFailure's text names the outbox ID and the topic and never the cause's text or a payload, and the cause stays reachable (no silent failure, no leak).
func TestADeliveryFailureHidesItsCauseTextAndKeepsTheCause(t *testing.T) {
	cause := errors.New("handler said " + secret)
	f := outbox.DeliveryFailure{OutboxID: oid(3), Topic: topicV, Err: cause}
	if !errors.Is(f, cause) {
		t.Fatal("the cause is not reachable with errors.Is")
	}
	text := f.Error()
	for _, must := range []string{oid(3), topicV} {
		if !strings.Contains(text, must) {
			t.Errorf("%q does not name %q", text, must)
		}
	}
	if strings.Contains(text, secret) {
		t.Fatalf("the failure text carries the cause's text: %q", text)
	}
}
