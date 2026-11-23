# Status · REC-02 · correlation chain, `traceparent`, logs and spans

Updated 2026-11-23 (refreshed by the Repo Steward; see the session section at the end) · Author: Core Engineer 2 (C2) · State: PLAN ONLY. No code in this change. It is the PR plan the Founder asked for on 17 Nov 2026 ("REC-02 traceparent: PR plan, then tests"). It decides nothing: every open point below has an owner.

## Task card

- **Task:** REC-02. **Owner:** C2 + AG. **Needs:** PLT-01 (done). **Week/date in `tasks.csv`:** 3 / 2026-11-04 (late).
- **PERD REQ:** REQ-AUD-004, REQ-OBS-002, REQ-SEC-003. **SoT:** PLAT-NFR-15 (SoT §21.2: "Observability: one correlation chain across all four powers and the Record"), AGENT-13, AGENT-21.
- **Done when (plan):** one trace spans the core and the model service on a test call; the log linter passes.
- **Allowlist (proposed):** `services/core/platform/telemetry/`, `services/core/edge/` (header handling only), `services/core/orchestrator/portadapters/` (the model client's outgoing headers), `services/model/noema_model/platform/telemetry/`, `services/model/noema_model/main.py` (middleware hook only), `tools/lint/` (the log rule), and the READMEs and `status/REC-02.md`. Nothing in `contracts/`, `deploy/`, `.github/`, `packs/`, trust, execution or outcome.

## What the requirements say (read from PERD v1.28, 23 Nov 2026)

- **REQ-AUD-004 (rationale PLAT-NFR-15):** the full SoT §43.11 chain (`correlation_id`, `request_id`, `session_id`, `intent_id`, `decision_id`, `grant_id`, `receipt_id`, `verdict_id`, `case_id`, `record_event_id`; the PERD adds `investigation_id` [REC]) is propagated in four places: HTTP headers (`Noema-Correlation-Id` and W3C `traceparent`), Temporal workflow memo and search attributes, log fields, and trace attributes. It contains no PII. **100% of request-path log lines carry `correlation_id`**, checked by a log linter in CI.
- **REQ-OBS-002:** one trace runs edge, model service, Trust, Execution, Outcome, explanation.
- **REQ-SEC-003:** logs go through the redactor (masking, secret keys); a log line is diagnostic, never proof of an outcome (AGENT-21).

## What is on `main` today (facts, re-read 23 Nov 2026 at `cca1ff55`; the first version was read at `af7b6fde` on 17 Nov)

| Where | State |
|---|---|
| `services/core/platform/telemetry/` | Empty boundary package (`doc.go`, README says "REC-02"). No tracer, no metrics, no log helper. |
| `services/core/platform/redaction/` | The mandatory redactor is merged (SEC-03, #457): `redaction.go` with its tests. |
| Core logging | 72 `slog.` call sites in non-test Go files under `services/core` (it was 11 on 17 Nov). None is checked for `correlation_id` by a linter. |
| `traceparent` | Nowhere in the Go, Python, JSON or TypeScript sources. |
| Header `Noema-Correlation-Id` | The constant exists: `services/core/edge/session/http.go:16` (`CorrelationHeader`), and `record/reconstruct/handler.go:95` and `synthetic/mock_case` read it. The edge never adopts a caller's value (`http.go:132`). |
| OpenTelemetry in the core | None: `services/core/go.mod` has no `go.opentelemetry.io` module. |
| Model service | `noema_model/platform/telemetry/__init__.py` has `event(name, correlation_id, fields)` and `span(name, correlation_id, attributes)` over `opentelemetry-api==1.45.1` (the only OpenTelemetry package in `pyproject.toml`; no SDK, no exporter). |
| Correlation id | The core mints it at the edge and never adopts a caller's (`edge/session/http.go`, `cmd/core/conversations.go`); `conversation_ref` and `investigation_id` are `inv_` + the ULID of it (A234). |

## Design (proposed, all subject to the open points)

1. **One trace root per request, minted at the edge.** The edge starts a new trace for each request. A `traceparent` header on a public request is **not adopted** (same reasoning as the correlation id: the caller is untrusted and could pin or forge a trace). It is recorded, if at all, as a span attribute link `caller.traceparent` after redaction (open point 3).
2. **Outgoing calls carry `traceparent`, `tracestate` omitted, and `Noema-Correlation-Id`** (the header of REQ-AUD-004; the first version of this plan wrote `X-Correlation-Id`, which the PERD and the code do not use). The model client (`modelclient.go` and the port adapters) sets them from the request context. The model service's middleware reads both, **only on the mTLS listener**, starts a child span, and puts `correlation_id` into every log line through the existing `telemetry.event`.
3. **Spans, enum-only attributes.** The span names follow the stage names (`edge.request`, `model.compile`, `trust.decide`, `execution.dispatch`, `outcome.verify`, `explain.compose`). Attributes are enum-like or IDs; never customer text, account numbers or model output (PERD §44, REQ-SEC-003). The trace-attribute filter of #457 is the guard.
4. **Logs.** One helper in `platform/telemetry` returns an `slog.Logger` already bound to the correlation chain from the context, so a request-path call site cannot forget the field. The `slog` handler of #457 stays the redaction point.
5. **Log linter (CI).** A rule in `tools/lint` that fails on a request-path `slog.*` call (Go) or `logging`/`telemetry.event` call (Python) that does not take the bound logger or carry `correlation_id`. "Request path" is defined by package list in one file (`tools/lint/request_path.txt`), so a new package is an explicit decision. This is the "100%" in REQ-AUD-004 made checkable.
6. **Exporter.** No collector is in the compose files today. The test profile writes spans as JSON lines to a file (OpenTelemetry's stdout/file exporter); production export is not part of this task (open point 5).

## Tests first (each names its ID)

| Test | Proves | Layer |
|---|---|---|
| A request with a hostile `traceparent` gets a new trace id; the id the caller sent is not the one in any span or log | REQ-AUD-004, REQ-SEC-003 | Go, `edge` |
| The model client's outgoing request carries a `traceparent` whose trace id equals the request's span, and the `Noema-Correlation-Id` header | REQ-AUD-004, REQ-OBS-002 | Go, portadapter |
| The model service continues the trace it is given and refuses to continue one that arrived without mTLS (the listener setting of #635) | REQ-OBS-002 | Python |
| **Acceptance:** one test call through a real core and a real model service in the compose test profile produces spans with one trace id from both processes | REQ-OBS-002 (the plan's Done when) | compose job |
| The log linter fails on a seeded bad call site in each language and passes on the tree | REQ-AUD-004 | `tools/lint` self-test |
| A log line with a secret-named key or a 16-digit run in a span attribute is masked or dropped | REQ-SEC-003 | Go and Python |
| Property: for any generated request sequence, every log line emitted on the request path carries the same `correlation_id` as the request | REQ-AUD-004 | Hypothesis / `rapid` (see open point 2) |

Record events: none are added; the trace never replaces the Record (AGENT-21). Faults: a model service that is down must still produce the edge span with an error status and the correlation id in the log (the fault comes from the hostility proxy or a stopped service, not a mock, AGENT-12).

## Open points (the answers found on 23 Nov 2026 are marked; none is made here by this file)

1. **Go OpenTelemetry module. Answered:** OpenTelemetry is approved (ruling A320, interim, `DEC-2026-11-18-architect-rulings-a317-a328`; OD-P-97; PERD §36.1), and the Go dependency is pinned with no exporter module (Architect ruling R6 (f) and R6 of `DEC-2026-11-04-architect-rulings`). The AGENT-14 [OPEN] entry is not needed; the PR names the exact pinned modules (API and SDK, no exporter).
2. **Go property-testing library. Answered:** `pgregory.net/rapid` is approved (`DEC-2026-11-03-rapid-test-dependency`), is in `go.mod` and in use, and is recorded as adopted (A320, OD-P-98). The property test uses it.
3. **Caller `traceparent`:** drop it, or keep it as a redacted link attribute? Proposed default: drop it entirely (smallest safe). **Still open. Owner: Security Reviewer.**
4. **Order with #457. Answered:** #457 is merged, so the core half builds on its redactor and `slog` handler.
5. **Exporter in production profile.** Not decided; the plan's Done when needs only the test profile. **Still open. Owner: Architect (PERD §44 / §65).**
6. **Python SDK dependency. Answered in principle:** A320 approves OpenTelemetry (PERD §36.1). The model service has `opentelemetry-api==1.45.1` only; the span tests need `opentelemetry-sdk`, pinned and with no exporter, named in the PR (AGENT-14).
7. **New: Temporal memo and search attributes.** REQ-AUD-004 also lists the Temporal workflow memo and search attributes as places the chain is carried. This plan covers headers, logs and spans only. Does REC-02 cover the Temporal half, or is it another task? **Owner: Architect.**

## Pre-work for the next agent

- Exact next step: points 1, 2, 4 and 6 are answered; points 3, 5 and 7 are still open (the Steward has not judged whether they block the red tests; 3 and 7 may). Branch `test/REC-02-trace-propagation` from `main` and push the red tests of the table above only (no implementation), read the whole red run, then implement.
- Files to read first: `services/core/platform/telemetry/README.md`, `services/model/noema_model/platform/telemetry/__init__.py`, `services/core/edge/session/http.go`, `services/core/cmd/core/modelclient.go`, PERD REQ-AUD-004 and REQ-OBS-002.
- Do not touch: `contracts/`, `deploy/`, `.github/`, trust, execution, outcome internals.

## Session 2026-11-17 · C2

- What changed: this file only (docs, `status/`). Debt entries: none. Conflicts raised: none.
- Why: the Founder's order to plan REC-02 before writing tests; the facts above come from reading `main`, not from the task row, which still says "In progress" with no PR.

## Session 2026-11-23 · Repo Steward (refresh)

- What changed: this file only (docs, `status/`). The SoT ID is PLAT-NFR-15 (SoT §21.2). The header is `Noema-Correlation-Id`, not `X-Correlation-Id` (PERD REQ-AUD-004; `edge/session/http.go:16`). REQ-AUD-004 is quoted from PERD v1.28 (the first version read v1.24), including the Temporal memo and search attributes. The facts table is re-read at `cca1ff55` (#457 merged, 72 `slog.` sites, header constant exists, no OpenTelemetry in the core). Open points 1, 2, 4 and 6 are marked answered with their source (A320, OD-P-97, OD-P-98, R6, #457); a new point 7 (Temporal) is added.
- Not changed: the design and the tests (C2's), apart from the header name. Nothing here decides an open point.
- Debt entries: none. Conflicts raised: none.
