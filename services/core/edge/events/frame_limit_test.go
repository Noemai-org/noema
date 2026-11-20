package events_test

// EDG-02, A446 (interim, relayed, not yet committed): the UI transport limit is 65,536 bytes for any single response or frame, and the server never exceeds it. The size of a
// frame is the whole frame (id, event and data lines and the blank line). A frame over the limit is refused: the stream ends with the fixed error frame and nothing of the
// event is written. REQ-API-001, INV-12, AGENT-13. The data bound MaxEventBytes is set far above the limit here so that the limit is the only thing under test.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const wide = 1 << 24

func widen(o *events.Options) { o.MaxEventBytes = wide }

func answerWith(cursor int64, pad int) events.Event {
	e := ev(cursor, kindAnswer)
	e.Answer.Answer.Found = []string{strings.Repeat("a", pad)}
	return e
}

func stream(t *testing.T, evs ...events.Event) string {
	t.Helper()
	f := &fakeFeed{batches: []events.Batch{{Events: evs, Done: true}}}
	return newRigWith(t, f, nil, widen).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
}

// frames splits a stream body into its frames, each with its closing blank line.
func frames(body string) []string {
	var out []string
	for _, p := range strings.SplitAfter(body, "\n\n") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// padFor returns the pad that makes the frame of an answer with cursor 1 exactly size bytes.
func padFor(t *testing.T, size int) int {
	t.Helper()
	base := len(stream(t, answerWith(1, 0)))
	if base == 0 || base > size {
		t.Fatalf("base frame %d", base)
	}
	return size - base
}

func TestAFrameOfExactlyTheLimitIsWrittenAndOneByteMoreIsRefused(t *testing.T) { // A446
	if events.MaxFrameBytes != 65536 {
		t.Fatalf("the UI transport limit is 65,536 bytes: %d", events.MaxFrameBytes)
	}
	at := stream(t, answerWith(1, padFor(t, events.MaxFrameBytes)))
	if len(at) != events.MaxFrameBytes || !strings.HasPrefix(at, "id: 1\nevent: answer\n") {
		t.Fatalf("a frame of exactly the limit must pass whole: %d bytes", len(at))
	}
	over := stream(t, answerWith(1, padFor(t, events.MaxFrameBytes)+1))
	if !isFault(over, "INTERNAL_SAFETY_BLOCK") || len(over) > events.MaxFrameBytes || strings.Contains(over, "aaaa") {
		t.Fatalf("one byte over the limit must end the stream with the fixed error and nothing of the event: %d bytes", len(over))
	}
}

func TestAnOversizeFrameEndsTheStreamAfterTheFramesBeforeIt(t *testing.T) { // A446
	body := stream(t, ev(1, kindProgress), answerWith(2, events.MaxFrameBytes), ev(3, kindProgress))
	fs := frames(body)
	if len(fs) != 2 || !strings.HasPrefix(fs[0], "id: 1\n") || !isFault(fs[1], "INTERNAL_SAFETY_BLOCK") {
		t.Fatalf("%q", body)
	}
}

// No frame of any stream, of any payload size, is over the limit.
func TestNoFrameEverExceedsTheLimit(t *testing.T) { // A446
	base := len(stream(t, answerWith(1, 0)))
	rapid.Check(t, func(rt *rapid.T) {
		pad := rapid.OneOf(rapid.IntRange(0, 2*events.MaxFrameBytes), rapid.IntRange(events.MaxFrameBytes-base-8, events.MaxFrameBytes-base+8)).Draw(rt, "pad")
		var evs []events.Event
		for i, c := 0, int64(1); i < rapid.IntRange(1, 4).Draw(rt, "n"); i, c = i+1, c+1 {
			if rapid.Bool().Draw(rt, "wide") {
				evs = append(evs, answerWith(c, pad))
			} else {
				evs = append(evs, clarification(c, strings.Repeat("q", rapid.IntRange(1, 2*events.MaxFrameBytes).Draw(rt, "q")), "clr_AA11"))
			}
		}
		body := stream(t, evs...)
		for _, f := range frames(body) {
			if len(f) > events.MaxFrameBytes {
				rt.Fatalf("a frame of %d bytes", len(f))
			}
		}
		if len(body) == 0 {
			rt.Fatal("nothing written")
		}
	})
}

// The JSON error response of the route holds no variable text of its own; what varies is the correlation ID of the response header, which is bounded all the same.
func TestAnErrorResponseNeverExceedsTheLimitEvenWithAHugeCorrelationHeader(t *testing.T) { // A446
	h, err := events.New(baseOptions(&fakeFeed{}, func(context.Context, time.Duration) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rec.Header().Set(session.CorrelationHeader, "corr_"+strings.Repeat("A", 3*events.MaxFrameBytes))
	h.ServeHTTP(deadlineRec{rec}, httptest.NewRequest(http.MethodGet, path, nil)) // no session: the 401
	if rec.Code != http.StatusUnauthorized || rec.Body.Len() > events.MaxFrameBytes || !strings.Contains(rec.Body.String(), string(contracts.ErrorsV3ErrorCodeUNAUTHENTICATED)) {
		t.Fatalf("%d, %d bytes", rec.Code, rec.Body.Len())
	}
}

// The error frame that ends a stream carries the response's correlation ID; one that would push the frame over the limit is dropped and the limit holds.
func TestTheStreamsErrorFrameNeverExceedsTheLimitEvenWithAHugeCorrelationID(t *testing.T) { // A446
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	v, err := session.NewVerifier(session.Config{Keys: mapKey(iss), Audience: edgetest.Audience, Tenants: []string{edgetest.Tenant}, MaxLifetime: session.CeilingLifetime, Skew: time.Minute, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFeed{batches: []events.Batch{{Events: []events.Event{answerWith(1, events.MaxFrameBytes)}, Done: true}}}
	o := baseOptions(f, func(context.Context, time.Duration) error { return nil })
	o.MaxEventBytes = wide
	h, err := events.New(o)
	if err != nil {
		t.Fatal(err)
	}
	huge := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // after the session middleware: a correlation ID this large is not one it would issue
		w.Header().Set(session.CorrelationHeader, "corr_"+strings.Repeat("A", 3*events.MaxFrameBytes))
		h.ServeHTTP(w, r)
	})
	mux := http.NewServeMux()
	mux.Handle(events.Route, session.Middleware(v, ids.New(clk, nil))(session.RequireRole(ids.New(clk, nil), session.RoleCustomer)(huge)))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+iss.Token(edgetest.Kid, edgetest.Good(epoch, session.RoleCustomer)))
	rec := newRec()
	mux.ServeHTTP(rec, req)
	if body := rec.Body.String(); !isFault(body, "INTERNAL_SAFETY_BLOCK") || len(body) > events.MaxFrameBytes {
		t.Fatalf("%d bytes", len(body))
	}
}
