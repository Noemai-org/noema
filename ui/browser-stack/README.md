# Real-core J1–J8 browser suite

`journeys.spec.mjs` is the opt-in Playwright suite for REQ-UI-001 / PERD §11.1 and §40.4. It uses the shipped UI and real core, not a local contract server. It submits through the shipped UI and validates the core-issued conversation response. It separately probes the core-issued SSE URL for a real 200 text/event-stream response with no-store (the current UI consumer still polls investigation state). It checks public-stage copy and fixture-provided customer text, then runs DOM privacy and axe checks. J8 resumes the same investigation in a fresh browser context.

## Stack

Use the existing bench profile (real core, synthetic IdP/session, stand-in and fixture-control services). Include this test-only overlay; it proxies same-origin `/v1/` to core over the UI's existing edge_net. It does not publish core or add a network.

```sh
: "${BENCH_MAX_BODY_BYTES:?set the already-approved bench body limit}"
: "${NOEMA_UI_E2E_MAX_POLLING_MS:?set the host-approved UI polling bound}"
BENCH_PRINCIPALS=ui-e2e:customer \
BENCH_TOKEN_TTL=15m \
BENCH_COMPOSE='docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/bench-model.yml -f deploy/compose/bench-stack.yml -f ui/browser-stack/compose.real-core.yml --profile bench' \
make bench-up BENCH_TENANT=ten_alpha
```

No body-limit, event-route or polling value is introduced here. The eight NOEMA_EVENTS_* settings must be supplied by the composition owner and /readyz must be ready. The synthetic token is written to `benchmark/out/idp/tokens/ui-e2e`.

## Inputs

- `NOEMA_UI_URL=http://127.0.0.1:8082`
- `NOEMA_UI_E2E_SESSION_TOKEN`: short-lived token file above; never commit it.
- `NOEMA_UI_E2E_MAX_POLLING_MS`: host-approved positive integer.
- `NOEMA_UI_E2E_MANIFEST`: local manifest with schema_version `noema-ui-journeys.v1`; J1–J8 each contain `message`, `expected_stage` (one of six public stages), and exact `expected_text`.

The manifest describes already-seeded synthetic scenarios. The suite does not seed or mutate stand-in data, access oracle data, or invent fixture prompts. Run from ui/: `npm ci --ignore-scripts && npm run test:stack`. Missing inputs fail closed; the real route must return 200 text/event-stream with Cache-Control no-store.

## Boundary

This is customer-surface evidence, not benchmark Record/oracle evidence for dispatch counts, no-write guarantees, tenant isolation or causal reconstruction. J2/J6 need real card events; J3–J6 need their real write/watch/confirmation paths. Missing capabilities fail; they are not mocked.
