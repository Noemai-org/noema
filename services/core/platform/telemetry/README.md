# platform/telemetry

**What it does.** The correlation chain of one request and how it leaves the process: a W3C trace context minted at the edge (a caller's `traceparent` is never adopted), the correlation ID in the context, a logger bound to the correlation ID, and the headers of an outgoing call on the internal link (`Noema-Correlation-Id` and a child `traceparent`).

**REQs.** REQ-AUD-004, REQ-OBS-002, REQ-SEC-003 (slice 1 of REC-02). Standard library only; no OpenTelemetry module, no exporter.

**Not here yet.** The OpenTelemetry SDK and an exporter (open point 5 of `status/REC-02.md`), metrics, the log linter, the Temporal memo and search attributes (open point 7). The trace identifier is not a log field: the redactor masks digit runs of more than four, which a 32-character hex identifier often has (see `status/REC-02-slice1.md`).

**Mount.** `Tracer.Middleware()` goes outside `edge/session.Middleware` (the composition in `cmd/core` is EL's); `session.Middleware` then puts the correlation ID it minted into the same chain, and `understanding.Client.Compile` sends both to the model service.

**Tests.** `go test ./platform/telemetry/...` from `services/core/`.
