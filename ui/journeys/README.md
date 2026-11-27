# Real-core J1–J8 browser suite

This is the UI presentation and resume leg of the BEN-23 J1–J8 real-core journeys. It consumes BO's generated, input-only manifest and the existing `benchmark.runner` outputs. It does not add expected answers or oracle references to the manifest (INV-22).

The benchmark runner drives each generated case through the public API and records `results/<case_id>.json` plus `correlations.json`. Playwright then opens each completed investigation in the shipped UI using the matching synthetic principal token, validates the generated investigation-response contract and the mounted SSE route/event contract, runs the DOM privacy and axe checks, and verifies J8 in a fresh browser context. The UI's existing initial-submit regression remains in `browser/real-investigation.spec.mjs`; this suite tests the full benchmark-run state as consumed by the UI.

## Prerequisites

1. Already merged: BO's BEN-23 manifest PR #962, EL's events-route composition PR #907, and IL's 15-minute per-principal token issuer PR #983. The manifest path is `benchmark/journeys/j1-j8.v1.json`.
2. Materialise the exact generated suite named by that manifest:
   `python -m benchmark.generator generate --seed 20261020 --out benchmark/out/suite`.
3. Issue the 19 short-lived synthetic customer tokens from the manifest using the A683 target in #983. Run this after the bench stack is up and immediately before the browser checks; each token expires after 900 seconds (15 minutes). The target reads the manifest itself and writes one mode-0400 file per principal into the private `$(BENCH_OUT)/idp/tokens` directory (default: `benchmark/out/idp/tokens`); do not hand-build the principal list or copy tokens elsewhere.

   ```sh
   : "${BENCH_TENANT:?set the approved bench tenant}"
   make bench-journey-tokens \
     BENCH_TENANT="$BENCH_TENANT" \
     BENCH_MAX_BODY_BYTES=32768 \
     BENCH_TOKEN_TTL=900s
   ```

   `BENCH_TOKEN_TTL` is passed as a Go duration, so `900s` is the required representation of 900 seconds. The issuer keeps token files in the private directory (mode 0700); private signing material remains separate and protected.
4. Run the manifest's case IDs through the existing `benchmark.runner` on the bench network with the approved API, mock-case, stand-in and proxy credentials. Preserve its output directory; the UI tests consume `correlations.json` and `results/<case_id>.json`. The runner's required fixture loader and evidence sources must be present; a NOT_RUN/ERROR result is not a passing journey.
5. Start the real-core bench Compose profile with this test-only overlay. Pass `BENCH_COMPOSE` as a Make command-line assignment (not an environment variable; the Makefile's ordinary assignment overrides environment values):

   ```sh
   : "${BENCH_TENANT:?set the approved bench tenant}"
   make bench-up \
     BENCH_TENANT="$BENCH_TENANT" \
     BENCH_MAX_BODY_BYTES=32768 \
     BENCH_COMPOSE='docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/bench-model.yml -f deploy/compose/bench-stack.yml -f ui/browser-stack/compose.real-core.yml --profile bench'
   ```

   The overlay proxies same-origin `/v1/` from the UI container to `core:8080` over the existing `edge_net`; it does not publish core or add a network. SSE buffering is disabled.

## A715-approved UI safeguards

- Push-to-talk recording stops after 30 seconds at the UI, even if the host requests a longer duration. A shorter host-supplied limit remains authoritative. Capture remains disabled without a voice sink.
- Each SSE frame, including an incomplete frame, is bounded to 65,536 bytes before parsing. An oversized frame is never delivered; the reconnect helper falls back to the authoritative investigation GET at the existing 2,000 ms polling cadence, within the existing 60,000 ms polling ceiling. The shipped overlay remains polling-only; this does not claim that it consumes SSE.
- Tokens are read directly from the #983 output directory. Never copy token contents into source, fixtures, test output, environment dumps, CI logs, screenshots, Playwright traces, videos or reports. Do not attach token-bearing browser storage state or network traces.

## A658-approved settings

- UI polling interval: 2,000 ms.
- UI polling and initial-POST ceiling: 60,000 ms.
- The eight required `NOEMA_EVENTS_*` settings and their approved bounds are: `POLL_MS` 2,000–60,000; `MAX_STREAM_MS` 60,000–900,000; `WRITE_TIMEOUT_MS` 1,000–30,000; `READ_TIMEOUT_MS` 500–10,000; `MAX_EVENT_BYTES` 1,024–1,048,576; `MAX_STREAMS` 1–1,000; `MAX_STREAMS_PER_PRINCIPAL` 1–10 and no greater than `MAX_STREAMS`; `MAX_BATCH` 1–500. The approved cross-checks are fail-closed: `POLL_MS` below `MAX_STREAM_MS`; `READ_TIMEOUT_MS` not above `POLL_MS`. All eight settings are required; the core refuses missing/invalid settings and `/readyz` must be ready.

## Run

From the repository root, materialise and run the BEN-23 cases using the existing benchmark runner. Then from `ui/`:

```sh
NOEMA_UI_URL=http://127.0.0.1:8082 \
NOEMA_UI_E2E_RUN_DIR=../benchmark/out/ui-j1-j8-run \
npm run test:stack
```

Optional `NOEMA_UI_E2E_MANIFEST` overrides the committed BEN-23 manifest path. The token directory defaults to `../benchmark/out/idp/tokens` (the `make bench-journey-tokens` output); set `NOEMA_UI_E2E_TOKEN_DIR` only when `BENCH_OUT` points elsewhere. The test reads token files in memory only; do not log, persist, screenshot or attach token values. The test fails closed if a case has no completed runner result, correlation ID or principal token. It adds no expected output to BEN-23; it asserts only core-issued public stage/contract data and the real SSE frame contract.

## Known backend/runner gates

After #979 merged on 27 Nov, the runner supports `session_resume:same_channel` as well as `session_start`, `clock_advance`, `confirm` and `kill_switch`; J8 now has a runner driver. #979 also adds a `WebhookSender` port for `settlement_updated` and `reversal_posted`, but the environment still needs to supply a concrete sender. #1029 (open) implements the runner sender through mock-case's bench control routes; #1024 (open) configures the bench-only relay and its shared SYNTHETIC signing secret. J5 and J6 remain explicit errors until both are integrated, the bench relay is actually configured, and the caller supplies the required SYNTHETIC payload file. No Tushe payload schema is assumed. J3–J6 also depend on production write/Record paths; #907 still names write ports as gaps, so verify those paths at the current main head before interpreting the journeys as acceptance. J6 is intentionally thin (one case) until after G1. These tests do not fake any of those capabilities.

This suite is customer-surface evidence, not a substitute for the benchmark's Record/oracle assertions or G1 evidence. No Android/WebView acceptance is claimed.
