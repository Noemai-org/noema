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
