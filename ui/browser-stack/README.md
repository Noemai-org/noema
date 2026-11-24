# Real-core J1–J8 browser suite

This is the UI presentation and resume leg of the BEN-23 J1–J8 real-core journeys. It consumes BO's generated, input-only manifest and the existing `benchmark.runner` outputs. It does not add expected answers or oracle references to the manifest (INV-22).

The benchmark runner drives each generated case through the public API and records `results/<case_id>.json` plus `correlations.json`. Playwright then opens each completed investigation in the shipped UI using the matching synthetic principal token, validates the generated investigation-response contract and the mounted SSE route/event contract, runs the DOM privacy and axe checks, and verifies J8 in a fresh browser context. The UI's existing initial-submit regression remains in `browser/real-investigation.spec.mjs`; this suite tests the full benchmark-run state as consumed by the UI.

## Prerequisites

1. Merge BO's BEN-23 manifest PR #962 and EL's events-route composition PR #907. The manifest path is `benchmark/journeys/j1-j8.v1.json`.
2. Materialise the exact generated suite named by that manifest:
   `python -m benchmark.generator generate --seed 20261020 --out benchmark/out/suite`.
3. IL supplies a short-lived synthetic customer token file for each of the 19 distinct `principal_id` values in the manifest, under one token directory. The existing bench issuer writes one file per principal (mode 0400) at `benchmark/out/idp/tokens/<principal_id>`. To derive the exact principal list without hand-copying it:

   ```sh
   export BENCH_PRINCIPALS="$(python - <<'PY'
   import json
   with open("benchmark/journeys/j1-j8.v1.json", encoding="utf-8") as f:
       manifest = json.load(f)
   print(" ".join(sorted({f"{case['principal_id']}:{case['roles'][0]}" for journey in manifest["journeys"] for case in journey["cases"]})))
   PY
   )"
   : "${BENCH_TOKEN_TTL:?set an approved short-lived TTL; the issuer caps it at 15m}"
   ```

   The existing `make bench-tokens` target consumes these variables. Do not use one principal's token for another case; preserve the matching synthetic issuer key and core public-key configuration.
4. Run the manifest's case IDs through the existing `benchmark.runner` on the bench network with the approved API, mock-case, stand-in and proxy credentials. Preserve its output directory; the UI tests consume `correlations.json` and `results/<case_id>.json`. The runner's required fixture loader and evidence sources must be present; a NOT_RUN/ERROR result is not a passing journey.
5. Start the real-core bench Compose profile with this test-only overlay. Pass `BENCH_COMPOSE` as a Make command-line assignment (not an environment variable; the Makefile's ordinary assignment overrides environment values):

   ```sh
   : "${BENCH_MAX_BODY_BYTES:?set the approved bench body limit}"
   : "${BENCH_TENANT:?set the approved bench tenant}"
   make bench-up BENCH_TENANT="$BENCH_TENANT" BENCH_COMPOSE='docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/bench-model.yml -f deploy/compose/bench-stack.yml -f ui/browser-stack/compose.real-core.yml --profile bench'
   ```

   The overlay proxies same-origin `/v1/` from the UI container to `core:8080` over the existing `edge_net`; it does not publish core or add a network. SSE buffering is disabled.

## A654 settings

- UI polling interval: 2,000 ms.
- UI polling and initial-POST ceiling: 60,000 ms.
- EL's #907 body names all eight required `NOEMA_EVENTS_*` settings and their proposed ranges: `POLL_MS` 2,000–60,000; `MAX_STREAM_MS` 60,000–900,000; `WRITE_TIMEOUT_MS` 1,000–30,000; `READ_TIMEOUT_MS` 500–10,000; `MAX_EVENT_BYTES` 1,024–1,048,576; `MAX_STREAMS` 1–1,000; `MAX_STREAMS_PER_PRINCIPAL` 1–10 and no greater than `MAX_STREAMS`; `MAX_BATCH` 1–500. The #907 body explicitly labels the per-setting ranges and cross-checks as proposals for Architect approval; no value is invented here. The core refuses missing/invalid settings and `/readyz` must be ready.

## Run

From the repository root, materialise and run the BEN-23 cases using the existing benchmark runner. Then from `ui/`:

```sh
NOEMA_UI_URL=http://127.0.0.1:8082 \
NOEMA_UI_E2E_RUN_DIR=../benchmark/out/ui-j1-j8-run \
NOEMA_UI_E2E_TOKEN_DIR=../benchmark/out/idp/tokens \
npm run test:stack
```

Optional `NOEMA_UI_E2E_MANIFEST` overrides the committed BEN-23 manifest path. The test fails closed if a case has no completed runner result, correlation ID or principal token. It adds no expected output to BEN-23; it asserts only core-issued public stage/contract data and the real SSE frame contract.

## Known backend/runner gates

The current runner's `drivers.py` supports `session_start`, `clock_advance`, `confirm` and `kill_switch`. It refuses other timeline events as `EVENT_KIND_UNSUPPORTED` (never skips them). BEN-23 currently includes `webhook:settlement_updated` in J5, `webhook:reversal_posted` in J6, and `session_resume:same_channel` in J8. Those journeys remain red until their real event drivers exist. J3–J6 also depend on the production write seam/Record path; #907 itself names that as a gap. J6 is intentionally thin (one case) until after G1. These tests do not fake any of those capabilities.

This suite is customer-surface evidence, not a substitute for the benchmark's Record/oracle assertions or G1 evidence. No Android/WebView acceptance is claimed.
