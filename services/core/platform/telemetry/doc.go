// Package telemetry carries the correlation chain: OpenTelemetry traces, Prometheus-style metrics and JSON logs (PERD §44).
//
// May import: platform/redaction.
//
// Must never: log a secret, a full account number, raw audio or an unredacted transcript (AGENT-13).
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package telemetry
