# Status · REC-02 slice 1 · the correlation chain, W3C traceparent and the model call's headers

Updated 2026-11-24 · Author: Core Engineer 2 (C2, AI agent) · Plan: `status/REC-02.md` on noema#685 (not yet merged: this file has its own name so the two cannot clash).

## Session 2026-11-24 · REC-02 slice 1

- What changed: Go `platform/telemetry` (trace context, chain, bound logger, `Inject`, `Tracer.Middleware`), one line in `edge/session` (the correlation ID also goes into the chain), one line in `understanding/client.go` (`Inject` before the call); Python `platform/telemetry/tracecontext.py` and the compile route (continues a well-formed `traceparent`, adopts no malformed one, span `model.compile` carries the request's own correlation ID).
- Why: REQ-AUD-004 (headers and log fields), REQ-OBS-002 (one trace core to model service), REQ-SEC-003; plan points 1, 2, 4, 6 answered, 3 answered here by the plan's own default (a caller's traceparent is dropped).
- Risks, and how they are contained: a caller never sets the trace or the correlation ID (tests); telemetry never stops a call (a tracer that cannot draw an identifier sets no header); the model service continues only an exact W3C value, on the mTLS listener only.
- Tests added (REQ-AUD-004, REQ-OBS-002, REQ-SEC-003): `platform/telemetry/{trace,chain}_test.go`, `understanding/client_trace_test.go`, `edge/session/correlation_chain_test.go`, `services/model/tests/test_trace_continuation.py`. Hand mutants: adopt the caller's traceparent, send the parent's span id, accept upper-case hex, drop `Inject` from the client, keep any correlation value, loosen the Python regex, take the span's correlation ID from the header: all killed.
- Regression cases added: the tests above.
- Debt entries: none.
- Conflicts raised: none. One finding for an OPEN entry (text below).

## Finding, for the Steward's register (OPEN, safest default stated)

**The redactor masks a logged trace identifier.** The first version of `TestBoundLoggerCarriesTheChainAndPassesThroughTheRedactor` expected `trace_id` on the log line; the line carried `***************8090a0b0c0d0e0f10`. PERD 44.2 masks every digit run of more than four, and a random 32-hex identifier has such a run often. A typed exemption for the key `trace_id` would change the redactor's AGENT-16 list (Founder approval and a decision record). Safest default (built): the trace identifier is not a log field; the log line carries `correlation_id` (REQ-AUD-004's 100% rule), and the trace carries its own identifier. Evidence that would change it: a decision that logs must be joinable to traces by `trace_id` without the collector.

## Not in this slice, with owners

OpenTelemetry SDK modules (Go and Python) and any exporter: open point 5 (Architect), approved in principle by A320, no exporter. The Temporal memo and search attributes: open point 7 (Architect). The log linter and its request-path package list: next slice. Mounting `Tracer.Middleware` in `cmd/core`: EL's composition. The compose acceptance (one trace id from both processes): next slice, after the mount. A caller's `traceparent` as a redacted link attribute: open point 3 (SR); this slice drops it.

## Pre-work for the next agent

- Exact next step: mount `Tracer.Middleware(crypto/rand.Reader)` in the composition (EL), then the compose acceptance and the log linter.
- Files to read first: `services/core/platform/telemetry/chain.go`, `services/model/noema_model/platform/telemetry/tracecontext.py`.
- Do not touch: `contracts/`, `deploy/`, `.github/`.
