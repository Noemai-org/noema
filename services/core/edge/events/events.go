// Package events is the server-sent events route of the public API (EDG-02, ruling A170; PERD 39.2, REQ-API-001): GET /v1/conversations/{conversation_ref}/events streams
// the progress, clarification, confirmation, answer and error events of one conversation. It writes only what a Feed has returned as committed state (PERD 40: "SSE
// events are emitted from committed state only"), as the verified session's principal and tenant (INV-12) and never the request's.
//
// May import: the standard library, edge/session, internal/contracts.
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
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// Route is the path of PERD 39.2 in net/http pattern form.
const Route = "GET /v1/conversations/{conversation_ref}/events"

// Kind is an event name of PERD 39.2.
type Kind string

// The five event kinds.
const (
	KindProgress      Kind = "progress"
	KindClarification Kind = "clarification"
	KindConfirmation  Kind = "confirmation"
	KindAnswer        Kind = "answer"
	KindError         Kind = "error"
)

const maxKeys = 16 // the most members of each map of one event: a bound on the frame, not a policy

// Event is one committed event of a conversation. The fields are typed so that nothing needs a list of exemptions: Refs are typed IDs (validated, never masked), Numbers
// are integers, At is an instant; only Fields are free text, and they are masked.
type Event struct {
	Cursor  int64             // above 0 and increasing; the SSE id and what Last-Event-ID resumes after
	Kind    Kind              //
	At      time.Time         // when it was committed
	Refs    map[string]string // key to typed ID, for example "investigation_id": "inv_01J9"
	Fields  map[string]string // key to free text, masked before it is written
	Numbers map[string]int64  // key to number
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
	Feed          Feed
	Mask          func(string) string                              // masks one free-text value (the platform redactor, A154)
	Wait          func(ctx context.Context, d time.Duration) error // waits d or until ctx ends, returning ctx's error then
	Poll          time.Duration                                    // between two polls of the Feed
	MaxStream     time.Duration                                    // the longest one stream lasts
	MaxEventBytes int                                              // the largest data of one event
}

// Handler serves Route. Mount it behind session.Middleware and session.RequireRole(customer).
type Handler struct{ o Options }

// New refuses an Options that is missing anything.
func New(o Options) (*Handler, error) {
	switch {
	case o.Feed == nil, o.Mask == nil, o.Wait == nil:
		return nil, errors.New("events: a feed, a masker and a wait are required")
	case o.Poll <= 0, o.MaxStream <= 0, o.MaxEventBytes <= 0:
		return nil, errors.New("events: the poll interval, the stream limit and the event bound must be positive")
	}
	return &Handler{o: o}, nil
}

var (
	conversationRe = regexp.MustCompile(`\Ainv_[0-9A-Za-z]{2,32}\z`)
	keyRe          = regexp.MustCompile(`\A[a-z][a-z0-9_]{0,31}\z`)
	typedIDRe      = regexp.MustCompile(`\A[a-z]{2,6}_[0-9A-Za-z_]{2,64}\z`)
	cursorRe       = regexp.MustCompile(`\A(0|[1-9][0-9]{0,17})\z`)
)

const (
	faultFrame       = "event: error\ndata: {\"code\":\"STREAM_FAULT\"}\n\n"
	unavailableFrame = "event: error\ndata: {\"code\":\"STREAM_UNAVAILABLE\"}\n\n"
	keepalive        = ": keepalive\n\n"
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := session.FromContext(r.Context())
	if !ok {
		fail(w, http.StatusUnauthorized, contracts.ErrorsV2ErrorCodeUNAUTHENTICATED, "A session is required.")
		return
	}
	ref := r.PathValue("conversation_ref")
	if !conversationRe.MatchString(ref) {
		fail(w, http.StatusNotFound, contracts.ErrorsV2ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.")
		return
	}
	after := int64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if !cursorRe.MatchString(v) {
			fail(w, http.StatusBadRequest, contracts.ErrorsV2ErrorCodeINPUTINVALID, "The request is not valid.")
			return
		}
		after, _ = strconv.ParseInt(v, 10, 64) // the pattern bounds it below 10^18
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, contracts.ErrorsV2ErrorCodeINTERNALSAFETYBLOCK, "The request cannot be served.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.o.MaxStream)
	defer cancel()
	batch, err := h.o.Feed.Since(ctx, p, ref, after)
	switch {
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, contracts.ErrorsV2ErrorCodeNOTFOUNDORNOTAUTHORIZED, "Not found.")
		return
	case err != nil:
		fail(w, http.StatusServiceUnavailable, contracts.ErrorsV2ErrorCodeDEPENDENCYUNAVAILABLE, "The service is not available.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	last := after
	for {
		for _, e := range batch.Events {
			frame, err := h.frame(e, last)
			if err != nil {
				write(w, fl, faultFrame)
				return
			}
			write(w, fl, frame)
			last = e.Cursor
		}
		if batch.Done {
			return
		}
		if len(batch.Events) == 0 {
			write(w, fl, keepalive)
		}
		if h.o.Wait(ctx, h.o.Poll) != nil {
			return
		}
		batch, err = h.o.Feed.Since(ctx, p, ref, last)
		if err != nil {
			if ctx.Err() == nil {
				write(w, fl, unavailableFrame)
			}
			return
		}
	}
}

// frame validates one event and renders it as an SSE frame. Anything it cannot vouch for is an error and nothing of the event is written.
func (h *Handler) frame(e Event, last int64) (string, error) {
	switch e.Kind {
	case KindProgress, KindClarification, KindConfirmation, KindAnswer, KindError:
	default:
		return "", errors.New("kind")
	}
	if e.Cursor <= last || e.At.IsZero() || len(e.Refs) > maxKeys || len(e.Fields) > maxKeys || len(e.Numbers) > maxKeys {
		return "", errors.New("shape")
	}
	data := struct {
		At      string            `json:"at"`
		Fields  map[string]string `json:"fields"`
		Numbers map[string]int64  `json:"numbers"`
		Refs    map[string]string `json:"refs"`
	}{At: e.At.UTC().Format(time.RFC3339), Fields: map[string]string{}, Numbers: map[string]int64{}, Refs: map[string]string{}}
	for k, v := range e.Refs {
		if !keyRe.MatchString(k) || !typedIDRe.MatchString(v) {
			return "", errors.New("ref")
		}
		data.Refs[k] = v
	}
	for k, v := range e.Numbers {
		if !keyRe.MatchString(k) {
			return "", errors.New("number")
		}
		data.Numbers[k] = v
	}
	for k, v := range e.Fields {
		m := h.o.Mask(v)
		if !keyRe.MatchString(k) || !utf8.ValidString(m) || strings.IndexFunc(m, unicode.IsControl) >= 0 {
			return "", errors.New("field")
		}
		data.Fields[k] = m
	}
	raw, err := json.Marshal(data)
	if err != nil || len(raw) > h.o.MaxEventBytes {
		return "", errors.New("size")
	}
	return fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", e.Cursor, e.Kind, raw), nil
}

func write(w http.ResponseWriter, fl http.Flusher, s string) {
	if _, err := w.Write([]byte(s)); err == nil { // a client that has gone is not an error to report: the next poll sees its context end
		fl.Flush()
	}
}

func fail(w http.ResponseWriter, status int, code contracts.ErrorsV2ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	corr := w.Header().Get(session.CorrelationHeader)
	body := contracts.ErrorsV2ErrorEnvelope{Error: contracts.ErrorsV2ErrorBody{Code: code, Message: message, CorrelationId: contracts.CommonV1CorrelationId(corr)}}
	_ = json.NewEncoder(w).Encode(body) // the status is already sent; a failed write has no one to tell
}
