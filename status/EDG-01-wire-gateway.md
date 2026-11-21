# EDG-01 · lane wire_gateway

State: in review (draft PR noema#722). Owner: lane agent for the Engineering Lead. Unreviewed work: EL re-reads it.

# Session 2026-11-18 · EDG-01 · wire_gateway (REQ-API-001, REQ-INP-002; INV-12, INV-02, AGENT-10)

## Session summary
- What changed (PR links): noema#722. `services/core/cmd/core/wire_gateway.go`: `buildGateway`, `tenantRecorder`, `tenantIndex`, `withGateway`, `withGatewayReadiness`; `wire_gateway_test.go`. No other file.
- Why: the composition root needs the Input Gateway with a per-tenant Recorder and Index (a tenant's login appends and reads only its own events), the `POST /v1/conversations` mount, and a `/readyz` that names the gateway while it is missing. Rulings are interim, relayed, not yet committed.
- Risks, and how they are contained: a tenant with no core-edge login is `ErrNoGatewayForTenant` (the gateway answers 503, nothing is asked of any database); a tenant with a core-edge login and no record_read login stops startup; every setting is required and none has a default (AGENT-16); no message carries a DSN or key material (AGENT-13). Pools connect lazily. The index reuses the Record reader's per-tenant record_read pools (one pool set), so `records` must be the value `buildRecordReader` returns.
- Tests added (REQ-API-001, REQ-INP-002, INV-12, AGENT-10, AGENT-16): settings none, partial, invalid; per-tenant routing and refusal; readiness names the gateway; the mounted route records only as the session's tenant (a tenant named in the body is refused, ops 404, no session 401). The Record-backed behaviour (RLS, the core-edge login) is the `inputgateway` tests' and needs PostgreSQL: CI only.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.
- New settings (secret-bearing names need EL approval, status rule 7): `NOEMA_EDGE_DSN_DIR`, `NOEMA_EDGE_KEY_DIR`, `NOEMA_EDGE_KEY_MAX_ACTIVE`, `NOEMA_INPUT_HASH_KEY_FILE`, `NOEMA_INPUT_DEDUP_WINDOW`, `NOEMA_INPUT_RECORD_TIMEOUT`; the body bound reuses `NOEMA_MAX_BODY_BYTES`.

## Pre-work for the next agent
- Exact next step (final main.go, EL): `gw, closeGW, err := buildGateway(ctx, env, clk, ids, components.Records, start)`; `components = withGateway(components, gw)` before `buildAPI`; `newMux(withGatewayReadiness(composeReadiness(components, api), components), api)`. `start` is the orchestrator seam (the investigation store's `Start` then the runner), idempotent per `Turn.Input`. `missing()` in compose.go does not name the gateway, so `withGatewayReadiness` wraps it; folding it into `missing()` is a compose.go edit.
- Open questions (owner): errors.v3 for this route (EL); the file mode of the hash key file (Architect, as noema#535).
- Files to read first: `cmd/core/wire_gateway.go`, `inputgateway/README.md`.
- Do not touch: `tenantRecorder.For` and `tenantIndex.Find` must never return another tenant's recorder.

# Session 2026-11-19 · EDG-01 · wire_gateway production Start (REQ-INP-002, REQ-INP-003, REQ-API-001; INV-12, INV-30, AGENT-10, AGENT-16; rulings A330, A332, A168 interim, relayed, not yet committed)

## Session summary
- What changed: `wire_gateway_start.go` (new): `turnStarter` (the gateway's `Start`: `investigations.Store.Start` per tenant, then ONE bounded worker pool runs `WriteRunner.RunWrite`), `parseStartConfig`, `closeInReverse`, `gatewayService` (`Gateway`, `Ready`, `Close`), `buildGatewayService`, `withGatewayServiceReadiness`, `recorderStarts`; `wire_gateway.go`: the missing-seam error is now `errNoSeam` and names the four settings.
- Why: the gateway took `start` as a parameter; this is the production one. Idempotent per recorded input: a delivery whose investigation already moved (version above 0) runs nothing; one that is in flight in this process runs nothing; one that never moved (a crash before the first move) is run. A full queue is `ErrStartBusy` (503), leaves nothing in flight, and the client's retry is run. Every store call and every run has a deadline; the drain is bounded by the run timeout (the runs are not cancelled by the process context).
- New settings (A330, no default): `NOEMA_INVESTIGATION_WORKERS`, `NOEMA_INVESTIGATION_QUEUE`, `NOEMA_INVESTIGATION_RUN_TIMEOUT_MS`, `NOEMA_INVESTIGATION_START_TIMEOUT_MS`: all four or none; a count is 1 to 65536 (structural ceiling), a bound is whole milliseconds.
- Tests: REQ-INP-003, INV-12, AGENT-16 (22 tests; fakes of the store and the runner, no database). Hand mutation: 17 mutants, 17 killed (5 needed new tests, added).
- Safer readings taken: (1) a turn with no principal kind, or a correlation that is not `corr_`+ULID, is refused before anything is asked; (2) the role of the run's session is the identity's principal kind; (3) a run that errors or panics is reported to a required `failed` callback and is NOT retried (a run that moved is not run again; resuming it is STM-01's `ResumeWrite`, not here); (4) the pool is in-process only: two core processes could each run an input that never moved (the store's compare-and-swap on the version refuses the second move).
- Conflicts raised: none. Debt: none.

## Pre-work for the next agent
- Exact next step (final main.go, EL): `svc, err := buildGatewayService(ctx, env, clk, ids, components.Records, recorderStarts{rec}, writeRunner, logFailed)`; `components = withGateway(components, svc.Gateway)` (svc may be nil); `newMux(withGatewayServiceReadiness(composeReadiness(components, api), svc), api)`; `defer svc.Close()` AFTER the HTTP server's shutdown. `rec` is buildRecorder's value, `writeRunner` an `*orchestrator.WriteRunner` built from the trust, executor and verifier adapters, `logFailed` a logger of the investigation ID and the error class only.
- Open: errors.v3 for the route needs noema#647 (open on 19 Nov): `inputgateway/handler.go` and `conversations.go failStart` still answer errors.v2.
- Do not touch: the `inflight` release must stay after the run, never before.
