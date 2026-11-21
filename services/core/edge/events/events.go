// Package events is the server-sent events route of the public API (EDG-02, ruling A170; PERD 39.2, REQ-API-001): GET /v1/conversations/{conversation_ref}/events streams
// the progress, clarification, confirmation, answer and error events of one conversation. Every event is a payload of sse-event.v1 (contracts/schemas/sse-event.v1.json),
// built by a Feed from committed state only (PERD 40: "SSE events are emitted from committed state only"), as the verified session's principal and tenant (INV-12) and never
// the request's. The payload types are the contract's: there is no member for anything else, so nothing internal can be added to the wire.
//
// May import: the standard library, edge/session, internal/contracts, platform/clock.
//
// Must never: read a database, take an identity from the request, write an event the Feed did not return, or put free text on the wire unmasked (AGENT-13).
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// Route is the path of PERD 39.2 in net/http pattern form.
const Route = "GET /v1/conversations/{conversation_ref}/events"

// SchemaVersion is the schema_version every payload carries (sse-event.v1 is version 1.0 of its members).
const SchemaVersion = "1.0"

// Event is one committed event of a conversation: its cursor and exactly one sse-event.v1 payload. The handler sets the payload's `event` member itself, from which
// field is set, so a Feed cannot name one kind and send another.
type Event struct {
	Cursor        int64 // above 0 and increasing; the SSE id and what Last-Event-ID resumes after
	Progress      *contracts.SseEventV1Progress
	Clarification *contracts.SseEventV1Clarification
	Confirmation  *contracts.SseEventV1Confirmation
	Answer        *contracts.SseEventV1Answer
	Error         *contracts.SseEventV1Error
}

// Batch is what the Feed returns: the committed events after a cursor, and whether the conversation is finished (no event will follow).
type Batch struct {
	Events []Event
	Done   bool
}

// ErrNotFound is what a Feed returns for a conversation that does not exist or is not the principal's; the answer to the caller is the same for both (REQ-API-002).
var ErrNotFound = errors.New("events: no such conversation for this principal")

// Feed returns the committed events of one conversation after a cursor, for the principal (whose tenant binds the read). It is the only source of what the route writes.
type Feed interface {
	Since(ctx context.Context, p session.Principal, conversationRef string, after int64) (Batch, error)
}

// Options is everything the handler needs. Nothing has a default (AGENT-16).
type Options struct {
	Feed                   Feed
	Mask                   func(string) string                              // masks one free-text value (the platform redactor, A154)
	Wait                   func(ctx context.Context, d time.Duration) error // waits d or until ctx ends, returning ctx's error then
	Clock                  clock.Clock                                      // the injected clock: the session's remaining life and the write deadlines (never time.Now)
	Poll                   time.Duration                                    // between two polls of the Feed
	MaxStream              time.Duration                                    // the longest one stream lasts (and never longer than the session's token)
	MaxEventBytes          int                                              // the largest data of one event
	WriteTimeout           time.Duration                                    // the deadline of each write: a slower consumer loses its stream
	MaxStreams             int                                              // the most streams open at once
	MaxStreamsPerPrincipal int                                              // the most streams one principal of one tenant has open
	MaxBatch               int                                              // the most events one Feed answer may hold
}

// Handler serves Route. Mount it behind session.Middleware and session.RequireRole(customer); it checks the role itself too.
type Handler struct {
	o     Options
	mu    sync.Mutex
	total int
	per   map[string]int // open streams by tenant and principal
}

// New refuses an Options that is missing anything.
func New(o Options) (*Handler, error) {
	switch {
	case o.Feed == nil, o.Mask == nil, o.Wait == nil, o.Clock == nil:
		return nil, errors.New("events: a feed, a masker, a wait and a clock are required")
	case o.Poll <= 0, o.MaxStream <= 0, o.MaxEventBytes <= 0, o.WriteTimeout <= 0:
		return nil, errors.New("events: the poll interval, the stream limit, the event bound and the write timeout must be positive")
	case o.MaxStreams <= 0, o.MaxStreamsPerPrincipal <= 0, o.MaxStreamsPerPrincipal > o.MaxStreams, o.MaxBatch <= 0:
		return nil, errors.New("events: the stream caps and the batch cap must be positive, the per-principal cap no larger than the global one")
	}
	return &Handler{o: o, per: map[string]int{}}, nil
}

var (
	conversationRe  = regexp.MustCompile(`\Ainv_[0-9A-Za-z]{2,32}\z`)
	clarificationRe = regexp.MustCompile(`\Aclr_[0-9A-Za-z]{2,32}\z`)
	confirmationRe  = regexp.MustCompile(`\Acfr_[0-9A-Za-z]{2,32}\z`)
	correlationRe   = regexp.MustCompile(`\Acorr_[0-9A-Za-z]{2,32}\z`)
	cursorRe        = regexp.MustCompile(`\A(0|[1-9][0-9]{0,17})\z`)
)

// MaxFrameBytes is the UI transport limit (A446, interim, relayed, not yet committed): no single frame or JSON response of this route is larger. It is the contract with the UI,
// not a safety setting, so it is a constant and not an option.
const MaxFrameBytes = 65536

const keepalive = ": keepalive\n\n"

// acquire takes one stream place for the principal, or reports that there is none: the caps are the limits of REQ-API-001's long-lived connections.
func (h *Handler) acquire(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total >= h.o.MaxStreams || h.per[key] >= h.o.MaxStreamsPerPrincipal {
		return false
	}
	h.total++
	h.per[key]++
	return true
}

func (h *Handler) release(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.total--
	if h.per[key]--; h.per[key] <= 0 {
		delete(h.per, key)
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := session.FromContext(r.Context())
	if !ok {
		fail(w, http.StatusUnauthorized, contracts.ErrorsV3ErrorCodeUNAUTHENTICATED, "A session is required.")
		return
	}
	if p.Role != session.RoleCustomer {
		fail(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.")
		return
	}
	left := p.ExpiresAt.Sub(h.o.Clock.Now())
	if left <= 0 {
		fail(w, http.StatusUnauthorized, contracts.ErrorsV3ErrorCodeUNAUTHENTICATED, "A session is required.")
		return
	}
	ref := r.PathValue("conversation_ref")
	if !conversationRe.MatchString(ref) {
		fail(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.")
		return
	}
	after := int64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if !cursorRe.MatchString(v) {
			fail(w, http.StatusBadRequest, contracts.ErrorsV3ErrorCodeINPUTINVALID, "The request is not valid.")
			return
		}
		after, _ = strconv.ParseInt(v, 10, 64) // the pattern bounds it below 10^18
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.")
		return
	}
	corr := w.Header().Get(session.CorrelationHeader)
	out := &sink{w: w, fl: fl, rc: http.NewResponseController(w), clk: h.o.Clock, timeout: h.o.WriteTimeout}
	if out.deadline() != nil { // a writer that cannot take a write deadline cannot be protected from a slow consumer: no stream
		fail(w, http.StatusInternalServerError, contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.")
		return
	}
	key := p.Tenant + "|" + p.ID
	if !h.acquire(key) {
		fail(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available.")
		return
	}
	defer h.release(key)
	life := min(h.o.MaxStream, left) // a stream never outlives its token
	ctx, cancel := context.WithTimeout(r.Context(), life)
	defer cancel()
	batch, err := h.o.Feed.Since(ctx, p, ref, after)
	switch {
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, contracts.ErrorsV3ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.")
		return
	case err != nil:
		fail(w, http.StatusServiceUnavailable, contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	last := after
	for {
		if len(batch.Events) > h.o.MaxBatch {
			_ = out.write(h.errorFrame(contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)) // the stream ends either way
			return
		}
		for _, e := range batch.Events {
			frame, err := h.frame(e, last)
			if err != nil {
				_ = out.write(h.errorFrame(contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.", corr)) // the stream ends either way
				return
			}
			if out.write(frame) != nil {
				return // a consumer that cannot keep up, or has gone, loses the stream
			}
			last = e.Cursor
		}
		if batch.Done {
			return
		}
		if len(batch.Events) == 0 && out.write(keepalive) != nil {
			return
		}
		if h.o.Wait(ctx, h.o.Poll) != nil {
			return
		}
		batch, err = h.o.Feed.Since(ctx, p, ref, last)
		if err != nil {
			if ctx.Err() == nil {
				_ = out.write(h.errorFrame(contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available now.", corr)) // the stream ends either way
			}
			return
		}
	}
}

// frame validates one event and renders it as an SSE frame. Anything it cannot vouch for is an error and nothing of the event is written: the payload must be exactly one
// member of the Event, must decode under its own contract type (closed members, required members, the event name), must carry a canonical instant, and no free text in it may
// hold a digit run the masker would change (A154: the contract's texts carry no run of five or more digits, so a text that the masker changes is a text that broke it).
func (h *Handler) frame(e Event, last int64) (string, error) {
	if e.Cursor <= last {
		return "", errors.New("cursor")
	}
	name, raw, err := h.payload(e)
	if err != nil {
		return "", err
	}
	if len(raw) > h.o.MaxEventBytes {
		return "", errors.New("size")
	}
	out := fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", e.Cursor, name, raw)
	if len(out) > MaxFrameBytes {
		return "", errors.New("frame size") // A446: the whole frame, not its data, is what the UI limit counts
	}
	return out, nil
}

func (h *Handler) payload(e Event) (name string, raw []byte, err error) {
	set := 0
	var v any
	var back any
	if e.Progress != nil {
		set++
		c := *e.Progress // a copy: the Feed keeps its payload, which the route never writes to
		c.Event, c.SchemaVersion = "progress", SchemaVersion
		name, v, back = "progress", &c, new(contracts.SseEventV1Progress)
	}
	if e.Clarification != nil {
		set++
		c := *e.Clarification // a copy: the Feed keeps its payload, which the route never writes to
		c.Event, c.SchemaVersion = "clarification", SchemaVersion
		name, v, back = "clarification", &c, new(contracts.SseEventV1Clarification)
	}
	if e.Confirmation != nil {
		set++
		c := *e.Confirmation // a copy: the Feed keeps its payload, which the route never writes to
		c.Event, c.SchemaVersion = "confirmation", SchemaVersion
		name, v, back = "confirmation", &c, new(contracts.SseEventV1Confirmation)
	}
	if e.Answer != nil {
		set++
		c := *e.Answer // a copy: the Feed keeps its payload, which the route never writes to
		c.Event, c.SchemaVersion = "answer", SchemaVersion
		name, v, back = "answer", &c, new(contracts.SseEventV1Answer)
	}
	if e.Error != nil {
		set++
		c := *e.Error // a copy: the Feed keeps its payload, which the route never writes to
		c.Event, c.SchemaVersion = "error", SchemaVersion
		name, v, back = "error", &c, new(contracts.SseEventV1Error)
	}
	if set != 1 {
		return "", nil, errors.New("kind")
	}
	raw, err = json.Marshal(v)
	if err != nil || json.Unmarshal(raw, back) != nil {
		return "", nil, errors.New("shape")
	}
	var tree any
	if json.Unmarshal(raw, &tree) != nil || !h.stable("", tree) {
		return "", nil, errors.New("text")
	}
	return name, raw, nil
}

// stable reports whether every string in the tree is safe to send. The instants must be canonical; the three IDs a card carries must have their contract shape and are
// exempt from masking only then (a ULID may hold a long digit run); every other string must be unchanged by the masker.
func (h *Handler) stable(key string, v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			if !h.stable(k, c) {
				return false
			}
		}
	case []any:
		for _, c := range t {
			if !h.stable(key, c) {
				return false
			}
		}
	case string:
		switch key {
		case "at", "expires_at":
			p, err := time.Parse(time.RFC3339Nano, t)
			return err == nil && t == p.UTC().Format(time.RFC3339Nano) && len(t) > 0 && t[len(t)-1] == 'Z'
		case "clarification_id":
			return clarificationRe.MatchString(t)
		case "confirmation_request_id":
			return confirmationRe.MatchString(t)
		case "correlation_id":
			return correlationRe.MatchString(t)
		}
		return h.o.Mask(t) == t
	}
	return true
}

// errorFrame is the stream's own failure, an sse-event.v1 error event with the errors.v3 body: a catalogue code and a fixed safe sentence, never the cause (AGENT-13).
func (h *Handler) errorFrame(code contracts.ErrorsV3ErrorCode, message, corr string) string {
	for _, c := range []string{corr, ""} { // a correlation ID that would push the frame over the UI limit (A446) is dropped, never the limit crossed
		e := contracts.SseEventV1Error{Event: "error", SchemaVersion: SchemaVersion, At: contracts.CommonV1Timestamp(h.o.Clock.Now().UTC().Format(time.RFC3339)),
			Error: contracts.ErrorsV3ErrorBody{Code: code, Message: message, Retryable: code == contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, CorrelationId: contracts.CommonV1CorrelationId(c)}}
		raw, err := json.Marshal(e)
		if err != nil {
			break
		}
		if out := fmt.Sprintf("event: error\ndata: %s\n\n", raw); len(out) <= MaxFrameBytes {
			return out
		}
	}
	return keepalive // unreachable for these members; a comment is the one frame that cannot mislead
}

// sink writes frames with a deadline on each write, from the injected clock.
type sink struct {
	w       http.ResponseWriter
	fl      http.Flusher
	rc      *http.ResponseController
	clk     clock.Clock
	timeout time.Duration
}

// deadline sets the write deadline from the injected clock; a writer that cannot take one is an error (fail closed).
func (s *sink) deadline() error {
	return s.rc.SetWriteDeadline(s.clk.Now().Add(s.timeout))
}

var errFrameTooLarge = errors.New("events: a frame over the UI transport limit")

func (s *sink) write(frame string) error {
	if len(frame) > MaxFrameBytes {
		return errFrameTooLarge // the last guard: nothing over the limit reaches the wire, whoever built it (A446)
	}
	if err := s.deadline(); err != nil {
		return err
	}
	if _, err := s.w.Write([]byte(frame)); err != nil {
		return err
	}
	s.fl.Flush()
	return nil
}

func fail(w http.ResponseWriter, status int, code contracts.ErrorsV3ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	corr := w.Header().Get(session.CorrelationHeader)
	body := contracts.ErrorsV3ErrorEnvelope{Error: contracts.ErrorsV3ErrorBody{Code: code, Message: message, Retryable: code == contracts.ErrorsV3ErrorCodeDEPENDENCYUNAVAILABLE, CorrelationId: contracts.CommonV1CorrelationId(corr)}}
	raw, err := json.Marshal(body)
	if err != nil || len(raw)+1 > MaxFrameBytes { // a correlation ID that would push the response over the UI limit (A446) is dropped
		body.Error.CorrelationId = ""
		raw, _ = json.Marshal(body)
	}
	_, _ = w.Write(append(raw, '\n')) // the status is already sent; a failed write has no one to tell
}
