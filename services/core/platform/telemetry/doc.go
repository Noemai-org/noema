// Package telemetry carries the correlation chain: OpenTelemetry traces, Prometheus-style metrics and JSON logs (PERD §44).
//
// May import: platform/redaction.
//
// Must never: log a secret, a full account number, raw audio or an unredacted transcript (AGENT-13).
//
// REC-02 slice 1 (standard library only): the W3C trace context (Tracer, TraceContext, ParseTraceparent), the correlation chain in the context (WithTrace, WithCorrelation), the
// bound logger (Logger), the outgoing headers (Inject) and the edge middleware that starts a new trace per request and never reads a caller's traceparent (Tracer.Middleware).
// The OpenTelemetry SDK, an exporter, metrics and the log linter are not here yet.
package telemetry
