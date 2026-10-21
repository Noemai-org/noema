// Package proxy is the hostility proxy (REQ-ENV-005): it sits between the system under test and Tushe (and the
// webhook relay), forwards only what its route table lists, rewrites Tushe's test account numbers, and injects the
// faults a PROXY fault plan names. It exists only in the bench and ci profiles. It never marks a response: the system
// under test must not be able to tell an injected fault from a real one. Faults are visible only on the control side,
// in the trace.
package proxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/jsonx"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/plan"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

const (
	maxRequestBody  = 1 << 20
	maxResponseBody = 8 << 20
)

// Options configures a Server.
type Options struct {
	Routes     *routes.Table
	Upstreams  map[string]*url.URL
	Fixtures   map[string]Fixture
	Numbers    *numbers.Mapper
	Client     *http.Client
	BenchToken string
	Log        *slog.Logger
}

// Stats are the counters of the control API.
type Stats struct {
	Requests        atomic.Int64
	Forwarded       atomic.Int64
	NumbersOut      atomic.Int64 // test numbers rewritten in responses
	NumbersIn       atomic.Int64 // synthetic numbers rewritten back in requests
	Unmappable      atomic.Int64
	LeaksBlocked    atomic.Int64 // responses refused because a test number survived: must stay 0
	UpstreamFailure atomic.Int64
}

// Server holds the state of one proxy.
type Server struct {
	opt    Options
	engine *plan.Engine
	clock  *clockHolder
	trace  *trace
	stale  *stale
	held   *held
	stats  Stats
}

// New builds a Server. Every upstream a route names must have a URL.
func New(o Options) (*Server, error) {
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	for _, name := range o.Routes.UpstreamNames() {
		if o.Upstreams[name] == nil {
			return nil, &MissingUpstreamError{name}
		}
	}
	return &Server{opt: o, engine: plan.NewEngine(), clock: newClockHolder(), trace: &trace{}, stale: newStale(), held: newHeld()}, nil
}

// MissingUpstreamError says a route names an upstream with no URL.
type MissingUpstreamError struct{ Name string }

func (e *MissingUpstreamError) Error() string {
	return "the route table uses upstream \"" + e.Name + "\" but no URL is configured for it"
}

var hopByHop = map[string]bool{"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true, "Proxy-Connection": true,
	"Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true, "Accept-Encoding": true, "Content-Length": true, "Content-Encoding": true}

// DataHandler is the side the system under test talks to. It has no credential of its own: Tushe's credentials
// pass through to Tushe, which checks them. The route table is its allowlist.
func (s *Server) DataHandler() http.Handler { return http.HandlerFunc(s.serveData) }

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + msg + `"}` + "\n")) //nolint:errcheck
}

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

func isJSON(contentType string) bool { return strings.Contains(strings.ToLower(contentType), "json") }

func isTextual(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "json") || strings.HasPrefix(ct, "text/") || strings.Contains(ct, "x-www-form-urlencoded")
}

// rewrite applies the number mapping to a string and counts what it did.
func (s *Server) rewrite(text string, dir numbers.Direction) string {
	out, st := s.opt.Numbers.RewriteText(text, dir)
	if dir == numbers.ToSynthetic {
		s.stats.NumbersOut.Add(int64(st.Rewritten))
	} else {
		s.stats.NumbersIn.Add(int64(st.Rewritten))
	}
	s.stats.Unmappable.Add(int64(st.Unmappable))
	return out
}

func (s *Server) rewriteRequestBody(contentType string, body []byte) []byte {
	if len(body) == 0 || !isTextual(contentType) {
		return body
	}
	if isJSON(contentType) {
		if tree, err := jsonx.Decode(body); err == nil {
			return jsonx.Encode(jsonx.MapStrings(tree, func(str string) string { return s.rewrite(str, numbers.ToReal) }))
		}
	}
	return []byte(s.rewrite(string(body), numbers.ToReal))
}

type upstreamResponse struct {
	status int
	header http.Header
	body   []byte
}

type call struct {
	w       http.ResponseWriter
	r       *http.Request
	rt      *routes.Route
	dec     plan.Decision
	eff     *plan.Effect
	now     time.Time
	method  string
	target  *url.URL
	header  http.Header
	body    []byte
	staleID string
}

func (s *Server) serveData(w http.ResponseWriter, r *http.Request) {
	s.stats.Requests.Add(1)
	rt := s.opt.Routes.Match(r)
	if rt == nil {
		writeError(w, http.StatusNotFound, "no such route")
		return
	}
	reqBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	up := s.opt.Upstreams[rt.Upstream]
	target := *up
	target.Path = strings.TrimRight(up.Path, "/") + s.rewrite(r.URL.Path, numbers.ToReal)
	target.RawQuery = s.rewrite(r.URL.RawQuery, numbers.ToReal)
	attrs := rt.Extract(r)
	for k, v := range attrs {
		attrs[k] = s.rewrite(v, numbers.ToReal) // plans are written with Tushe's own values
	}
	dec := s.engine.Next(rt.Target, rt.Operation, attrs)
	now, _ := s.clock.now()
	c := &call{w: w, r: r, rt: rt, dec: dec, eff: dec.Effect, now: now, method: r.Method, target: &target,
		body: s.rewriteRequestBody(r.Header.Get("Content-Type"), reqBody)}
	c.header = r.Header.Clone()
	for h := range hopByHop {
		c.header.Del(h)
	}
	c.header.Del("X-Forwarded-For")
	c.staleID = r.Method + " " + target.String()
	s.apply(c)
}

func (s *Server) record(c *call, detail map[string]any) {
	if c.eff == nil {
		return
	}
	s.trace.add(TraceEntry{Target: c.rt.Target, Operation: c.rt.Operation, CallNo: c.dec.CallNo, FaultIndex: c.eff.Index, Effect: c.eff.Name, Params: c.eff.Params, Detail: detail})
}

func abort() { panic(http.ErrAbortHandler) }

// apply runs the fault, if any, around the forward and delivers the answer.
func (s *Server) apply(c *call) {
	ctx := c.r.Context()
	e := c.eff
	if e != nil {
		switch e.Name {
		case plan.Latency, plan.NearTimeoutBefore, plan.NearTimeoutAfter:
			s.record(c, nil)
			if !sleep(ctx, e.Delay) {
				return
			}
		case plan.TimeoutBeforeCommit:
			s.record(c, nil)
			sleep(ctx, e.Delay)
			abort()
		case plan.ServiceUnavailable:
			s.record(c, nil)
			writeError(c.w, http.StatusServiceUnavailable, "service unavailable")
			return
		case plan.HTTP5XX:
			if !e.AfterCommit {
				s.record(c, nil)
				writeError(c.w, e.Status, "upstream failure")
				return
			}
		case plan.HTTP429:
			s.record(c, nil)
			if e.RetryAfter != nil {
				c.w.Header().Set("Retry-After", itoa(*e.RetryAfter))
			}
			writeError(c.w, http.StatusTooManyRequests, "too many requests")
			return
		case plan.ImpossibleState:
			fx, ok := s.opt.Fixtures[e.Fixture]
			if !ok {
				writeError(c.w, http.StatusInternalServerError, "fixture missing")
				return
			}
			s.record(c, map[string]any{"fixture": e.Fixture})
			s.deliverFixture(c, fx)
			return
		}
	}
	resp, err := s.forward(ctx, c)
	if err != nil {
		s.stats.UpstreamFailure.Add(1)
		s.opt.Log.Warn("upstream unavailable", "upstream", c.rt.Upstream, "err", err.Error())
		writeError(c.w, http.StatusBadGateway, "upstream unavailable")
		return
	}
	s.stats.Forwarded.Add(1)
	if c.method == http.MethodGet {
		s.stale.put(c.staleID, cached{at: c.now, status: resp.status, header: resp.header, body: resp.body})
	}
	if e != nil {
		switch e.Name {
		case plan.TimeoutAfterCommit:
			s.record(c, nil)
			sleep(ctx, e.Delay)
			abort()
		case plan.DropResponse:
			s.record(c, nil)
			abort()
		case plan.HTTP5XX: // after_commit: the write went through, the caller is told it failed
			s.record(c, nil)
			writeError(c.w, e.Status, "upstream failure")
			return
		}
	}
	s.deliver(c, resp)
}

func (s *Server) forward(ctx context.Context, c *call) (*upstreamResponse, error) {
	req, err := http.NewRequestWithContext(ctx, c.method, c.target.String(), bytes.NewReader(c.body))
	if err != nil {
		return nil, err
	}
	req.Header = c.header.Clone()
	req.ContentLength = int64(len(c.body))
	res, err := s.opt.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBody))
	if err != nil {
		return nil, err
	}
	return &upstreamResponse{status: res.StatusCode, header: res.Header, body: body}, nil
}

func (s *Server) deliverFixture(c *call, fx Fixture) {
	h := http.Header{"Content-Type": {"application/json"}}
	for k, v := range fx.Headers {
		h.Set(k, v)
	}
	body := jsonx.MapStrings(fx.Body, func(str string) string { return s.rewrite(str, numbers.ToSynthetic) })
	s.finish(c, &upstreamResponse{status: fx.Status, header: h}, jsonx.Encode(body))
}

// deliver applies the response-side faults and the number rewrite, and writes the answer.
func (s *Server) deliver(c *call, resp *upstreamResponse) {
	e := c.eff
	body := resp.body
	detail := map[string]any{}
	if e != nil && e.Name == plan.StaleRead {
		cutoff := c.now.Add(-time.Duration(e.Seconds) * time.Second)
		if old, ok := s.stale.atLeastAsOld(c.staleID, cutoff); ok {
			resp = &upstreamResponse{status: old.status, header: http.Header(old.header), body: old.body}
			body = old.body
			detail["served"] = "stale"
		} else {
			detail["served"] = "live"
		}
	}
	if isJSON(resp.header.Get("Content-Type")) {
		if tree, err := jsonx.Decode(body); err == nil {
			tree = s.faultTree(c, tree, detail)
			tree = jsonx.MapStrings(tree, func(str string) string { return s.rewrite(str, numbers.ToSynthetic) })
			body = jsonx.Encode(tree)
		} else {
			body = []byte(s.rewrite(string(body), numbers.ToSynthetic))
			if e != nil && e.Name != plan.StaleRead && e.Name != plan.Malformed {
				detail["applied"] = false
			}
		}
	} else {
		body = []byte(s.rewrite(string(body), numbers.ToSynthetic))
		if e != nil && e.Name != plan.StaleRead && e.Name != plan.Malformed {
			detail["applied"] = false
		}
	}
	if e != nil && e.Name == plan.Malformed {
		body = body[:len(body)/2]
		detail["bytes"] = len(body)
	}
	if e != nil {
		if len(detail) == 0 {
			detail = nil
		}
		s.record(c, detail)
	}
	s.finish(c, resp, body)
}

// finish checks that no test number leaves, copies the headers and writes the response.
func (s *Server) finish(c *call, resp *upstreamResponse, body []byte) {
	hdr := http.Header{}
	for k, vs := range resp.header {
		if hopByHop[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			if http.CanonicalHeaderKey(k) == "Location" {
				v = s.rewrite(v, numbers.ToSynthetic)
			}
			hdr.Add(k, v)
		}
	}
	leaked := numbers.ContainsTestNumber(string(body))
	for _, vs := range hdr {
		for _, v := range vs {
			leaked = leaked || numbers.ContainsTestNumber(v)
		}
	}
	if leaked {
		s.stats.LeaksBlocked.Add(1)
		s.opt.Log.Error("a test number survived the rewrite; the response was refused")
		writeError(c.w, http.StatusBadGateway, "response refused")
		return
	}
	out := c.w.Header()
	for k, vs := range hdr {
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	out.Set("Content-Length", itoa(len(body)))
	c.w.WriteHeader(resp.status)
	c.w.Write(body) //nolint:errcheck
}
