package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/faultplan"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
)

var (
	idRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)
	keyRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	longRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	corrRE = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	hashRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const (
	maxEpoch    = 9007199254740991 // 2^53-1, the integer ceiling of every Noema contract
	defaultPage = 50
	maxPage     = 200 // transport sanity bound on one page
	// dbTimeout bounds every database call. It is a transport sanity bound, not a product threshold, and it does
	// not cover the delays a fault plan asks for: those wait on the request itself.
	dbTimeout = 30 * time.Second
)

func withDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, dbTimeout)
}

// run executes an operation under a database deadline.
func (c *opCall) exec(ctx context.Context, cmd store.Cmd, eff *faultplan.Effect) reply {
	ctx, cancel := withDeadline(ctx)
	defer cancel()
	return c.run(ctx, cmd, eff)
}

// reply is what an operation produced, before any fault decides whether the caller sees it.
type reply struct {
	status int
	body   any
	header map[string]string
	err    *apiError
}

// opCall is one parsed, valid call. attrs feed the fault matcher; the rest feeds the log.
type opCall struct {
	attrs  map[string]string
	tenant string
	caseID string
	key    string
	run    func(ctx context.Context, cmd store.Cmd, eff *faultplan.Effect) reply
}

type parser func(w http.ResponseWriter, r *http.Request) (*opCall, *apiError)

// serve counts the call, applies the fault the plan assigns to it, runs the operation and answers.
//
// Faults act on the transport around the command, never inside the command's logic: before-commit faults leave
// no trace in the data, after-commit faults let the command commit and then lose or replace the response.
func (s *Server) serve(op string, parse parser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		corr := correlationID(r)
		call, perr := parse(w, r)
		var attrs map[string]string
		if call != nil {
			attrs = call.attrs
		}
		dec := s.engine.Next(op, attrs)
		at, _ := s.now()
		cmd := store.Cmd{At: at, Seed: dec.Seed, ScenarioID: dec.ScenarioID, FaultPlanID: dec.PlanID, Operation: op, CallNo: dec.CallNo, CorrelationID: corr}
		ctx := r.Context()
		if perr != nil { // a malformed call is counted but never receives a fault
			if s.logOnly(ctx, w, corr, cmd, "", "", "", perr.Status, perr.Code) {
				writeErr(w, corr, perr)
			}
			return
		}
		eff := dec.Effect
		if eff != nil {
			idx := eff.Index
			cmd.FaultIndex, cmd.FaultEffect, cmd.FaultParams = &idx, eff.Name, eff.Params
		}
		switch {
		case eff == nil:
			s.respond(w, corr, call.exec(ctx, cmd, nil))
		case eff.Name == faultplan.EffectLatency:
			if !sleep(ctx, eff.Delay) {
				return
			}
			s.respond(w, corr, call.exec(ctx, cmd, eff))
		case eff.Name == faultplan.EffectTimeoutBeforeCommit:
			cmd.Suppressed = true
			if !s.logOnly(ctx, w, corr, cmd, call.tenant, call.caseID, call.key, 0, "") {
				return
			}
			sleep(ctx, eff.Delay)
			panic(http.ErrAbortHandler)
		case eff.Name == faultplan.EffectHTTP429:
			e := newErr(429, "RATE_LIMITED", "too many requests", true)
			e.RetryAfter = eff.RetryAfter
			if s.logOnly(ctx, w, corr, cmd, call.tenant, call.caseID, call.key, 429, e.Code) {
				writeErr(w, corr, e)
			}
		case eff.Name == faultplan.EffectHTTP5XX && !eff.AfterCmt:
			e := unavailable(eff.Status)
			if s.logOnly(ctx, w, corr, cmd, call.tenant, call.caseID, call.key, eff.Status, e.Code) {
				writeErr(w, corr, e)
			}
		case eff.Name == faultplan.EffectHTTP5XX:
			cmd.StatusOverride = eff.Status
			if rep := call.exec(ctx, cmd, eff); rep.err != nil && rep.err.Status >= 500 {
				s.respond(w, corr, rep) // the command itself failed: report that, not the injected status
			} else {
				writeErr(w, corr, unavailable(eff.Status))
			}
		case eff.Name == faultplan.EffectTimeoutAfterCommit || eff.Name == faultplan.EffectDropResponse:
			cmd.Suppressed = true
			if rep := call.exec(ctx, cmd, eff); rep.err != nil && rep.err.Status >= 500 {
				s.respond(w, corr, rep)
				return
			}
			sleep(ctx, eff.Delay)
			panic(http.ErrAbortHandler)
		default: // PAGINATE, CONCURRENT_MODIFICATION: the operation itself reads the effect
			s.respond(w, corr, call.exec(ctx, cmd, eff))
		}
	}
}

func unavailable(status int) *apiError {
	return newErr(status, "UNAVAILABLE", "the case system is temporarily unavailable", true)
}

func (s *Server) respond(w http.ResponseWriter, corr string, rep reply) {
	if rep.err != nil {
		writeErr(w, corr, rep.err)
		return
	}
	for k, v := range rep.header {
		w.Header().Set(k, v)
	}
	writeJSON(w, rep.status, rep.body)
}

// logOnly records a command the store never saw. It reports false (after answering 500) when it cannot.
func (s *Server) logOnly(ctx context.Context, w http.ResponseWriter, corr string, cmd store.Cmd, tenant, caseID, key string, status int, code string) bool {
	ctx, cancel := withDeadline(ctx)
	defer cancel()
	if err := s.st.LogOnly(ctx, cmd, tenant, caseID, key, status, code); err != nil {
		s.log.Error("command log write failed", "operation", cmd.Operation, "err", err)
		writeErr(w, corr, internalErr())
		return false
	}
	return true
}

func internalErr() *apiError {
	return newErr(500, "INTERNAL", "the case system failed to process the request", false)
}

// sleep waits for d or until the caller goes away; it reports whether the full time elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// dbFailure turns a store error into the generic 500 and logs the cause (never shown to the caller).
func (s *Server) dbFailure(op string, err error) reply {
	s.log.Error("store failure", "operation", op, "err", err)
	return reply{err: internalErr()}
}

// outcomeReply maps a store outcome to the HTTP answer. noun names the object in a 404.
func outcomeReply(oc store.Outcome, body any, noun string) reply {
	switch oc {
	case store.Created:
		return reply{status: 201, body: body}
	case store.Replayed:
		return reply{status: 200, body: body, header: map[string]string{"Idempotent-Replay": "true"}}
	case store.Found, store.Routed:
		return reply{status: 200, body: body}
	case store.NotFound:
		return reply{err: newErr(404, "NOT_FOUND", "no such "+noun, false)}
	case store.IdempotencyConflict:
		return reply{err: newErr(409, "IDEMPOTENCY_CONFLICT", "this idempotency key was already used with a different payload", false)}
	}
	return reply{err: internalErr()}
}

// ---- request parsing ----

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) *apiError {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return newErr(415, "UNSUPPORTED_MEDIA_TYPE", "send application/json", false)
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			return newErr(413, "PAYLOAD_TOO_LARGE", "the request body is too large", false)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return badRequest(strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`), "unknown field")
		}
		return badRequest("body", "not a valid JSON object of the documented shape")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return badRequest("body", "trailing data after the JSON object")
	}
	return nil
}

// queryParams returns the query as single values and refuses repeats and names outside allowed.
func queryParams(r *http.Request, allowed ...string) (map[string]string, *apiError) {
	out := map[string]string{}
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	for k, v := range r.URL.Query() {
		if !ok[k] {
			return nil, badRequest(k, "unknown query parameter")
		}
		if len(v) != 1 {
			return nil, badRequest(k, "given more than once")
		}
		out[k] = v[0]
	}
	return out, nil
}

func check(re *regexp.Regexp, field, v string) *apiError {
	if !re.MatchString(v) {
		return badRequest(field, "missing or malformed")
	}
	return nil
}

func idempotencyKey(r *http.Request) (string, *apiError) {
	k := r.Header.Get("Idempotency-Key")
	if !keyRE.MatchString(k) {
		return "", badRequest("Idempotency-Key", "header is required and must be 1 to 128 characters of letters, digits and _.:-")
	}
	return k, nil
}

// ---- paging ----

func encodeCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("c1:" + strconv.FormatInt(seq, 10)))
}

func decodeCursor(s string) (int64, *apiError) {
	if s == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil && strings.HasPrefix(string(raw), "c1:") {
		if n, err := strconv.ParseInt(string(raw[3:]), 10, 64); err == nil && n >= 0 {
			return n, nil
		}
	}
	return 0, badRequest("cursor", "not a cursor this service issued")
}

// pageSize applies the client's limit, the service bound and a PAGINATE fault, whichever is smallest.
func pageSize(limit string, eff *faultplan.Effect) (int, *apiError) {
	n := defaultPage
	if limit != "" {
		v, err := strconv.Atoi(limit)
		if err != nil || v < 1 || v > maxPage {
			return 0, badRequest("limit", "must be an integer from 1 to "+strconv.Itoa(maxPage))
		}
		n = v
	}
	if eff != nil && eff.Name == faultplan.EffectPaginate && eff.PageSize < n {
		n = eff.PageSize
	}
	return n, nil
}

type page struct {
	Items      any    `json:"items"`
	Complete   bool   `json:"complete"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func newPage(items any, more bool, lastSeq int64) page {
	p := page{Items: items, Complete: !more}
	if more {
		p.NextCursor = encodeCursor(lastSeq)
	}
	return p
}
