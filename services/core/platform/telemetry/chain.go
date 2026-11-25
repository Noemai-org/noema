package telemetry

// chain.go: the correlation chain of one request, carried in its context, and the two ways it leaves the process: a bound logger and the headers of an outgoing call
// (REC-02; PERD REQ-AUD-004 "HTTP headers (Noema-Correlation-Id and W3C traceparent), ... log fields"; REQ-SEC-003; AGENT-13).
//
// The chain carries identifiers only: the correlation ID the edge minted and the trace identifier. It never takes either from a request header (a caller could send an
// account-number-shaped value, noema#458), and it never carries a tenant, a principal or any text of the customer.

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
)

// CorrelationHeader is the header of the correlation ID (PERD 39.1, REQ-AUD-004); TraceparentHeader is W3C's.
const (
	CorrelationHeader = "Noema-Correlation-Id"
	TraceparentHeader = "traceparent"
)

var correlationShape = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)

type chainKey struct{}

type chain struct {
	corr   string
	trace  TraceContext
	traced bool
	tracer *Tracer
}

func chainOf(ctx context.Context) chain {
	c, _ := ctx.Value(chainKey{}).(chain)
	return c
}

// WithTrace puts a trace context and the tracer that makes its children into ctx. The correlation ID, if one is already there, is kept.
func WithTrace(ctx context.Context, tr *Tracer, tc TraceContext) context.Context {
	c := chainOf(ctx)
	c.trace, c.traced, c.tracer = tc, true, tr
	return context.WithValue(ctx, chainKey{}, c)
}

// WithCorrelation puts the correlation ID the edge minted into ctx. A value that is not corr_ plus 2 to 32 letters or digits is not kept: the chain is then without an ID and
// nothing is sent or logged under a made-up one. The trace, if one is already there, is kept.
func WithCorrelation(ctx context.Context, corr string) context.Context {
	c := chainOf(ctx)
	if !correlationShape.MatchString(corr) {
		c.corr = ""
	} else {
		c.corr = corr
	}
	return context.WithValue(ctx, chainKey{}, c)
}

// CorrelationFrom returns the correlation ID in ctx, or "".
func CorrelationFrom(ctx context.Context) string { return chainOf(ctx).corr }

// TraceFrom returns the trace context in ctx.
func TraceFrom(ctx context.Context) (TraceContext, bool) {
	c := chainOf(ctx)
	return c.trace, c.traced
}

// Logger returns base with the correlation ID of ctx bound, when ctx has one. A request-path call site uses it so that it cannot forget the field (REQ-AUD-004: every
// request-path log line carries correlation_id). Pass a logger whose handler is the redactor's (platform/redaction).
//
// The trace identifier is deliberately not a log field. The redactor masks every run of more than four digits (PERD 44.2), and a random 32-character hex identifier has such a
// run often, so a logged trace_id would arrive mangled and could not be searched. A typed exemption for the key would change the redactor's AGENT-16 list; that is a Founder
// decision (open entry in the PR). The trace carries its own identifier on the span.
func Logger(ctx context.Context, base *slog.Logger) *slog.Logger {
	if c := chainOf(ctx); c.corr != "" {
		base = base.With("correlation_id", c.corr)
	}
	return base
}

// Inject sets the headers of an outgoing call on the internal link: Noema-Correlation-Id from the chain, and a traceparent whose trace identifier is the request's and whose
// span identifier is new (the call is a child span). Nothing is set for a part the chain lacks, and a tracer that cannot draw an identifier sets no traceparent: telemetry never
// stops a call. tracestate is never sent. The caller's own incoming traceparent is not used.
func Inject(ctx context.Context, h http.Header) {
	c := chainOf(ctx)
	if c.corr != "" {
		h.Set(CorrelationHeader, c.corr)
	}
	if c.traced && c.tracer != nil {
		if child, err := c.tracer.Child(c.trace); err == nil {
			h.Set(TraceparentHeader, child.Traceparent())
		}
	}
}

// Middleware starts a new trace for each request and puts it in the context. A traceparent or tracestate header on the incoming request is not read: the caller of the edge is
// untrusted and could pin or forge a trace (the same reasoning as the correlation ID). If an identifier cannot be drawn the request goes on without a trace.
func (t *Tracer) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc, err := t.Root(); err == nil {
				r = r.WithContext(WithTrace(r.Context(), t, tc))
			}
			next.ServeHTTP(w, r)
		})
	}
}
