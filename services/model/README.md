# services/model

The Python model service (the Go core is the other deployable). It hosts Understanding, Explanation and Voice, and is the only place with a model or speech provider SDK or credential (INV-29; SoT §35.6, PERD §64.2).

**Status (T5 and T6.3 of DEC-2026-10-20).** Built: the model harness (`harness/`: `generate`, `decide`, templates, masking, accounting, failover, breaker), the `generate` provider adapters (`providers/llm/`: OpenAI Chat, OpenAI Responses, Anthropic, Google, over the standard library, no vendor SDK), the understanding pipeline and its six decision tasks (`understanding/pipeline/`), deterministic ranking and the D5 recipient matcher (`understanding/resolution/`), and the platform pieces they use (`platform/{clock,config,redaction,telemetry}`). The Evidence API client (`evidence/`, to the candidate-read contract of #42) is built. The OpenAI Decisions adapter (`providers/llm/openai_decisions.py`) is built for synthetic data only. Not built, and why, is in `status/`: the schema gate, the lexicon normaliser, `explanation/` and `voice/`. `noema_model/main.py` builds the harness from its mounted configuration (`harness/assemble.py`) and serves only `/healthz` and `/readyz`; no request endpoint exists yet. There is no database driver, Temporal client or vendor SDK.

- **Python:** 3.13, pinned by `requires-python` in `pyproject.toml`. Dependencies are pinned with `==`, and `requirements.lock` / `requirements-dev.lock` carry every file hash (`pip install --require-hashes`). Regenerate with `make lock-model`.
- **Build context:** this folder only (DEC-2026-10-13 R2): `docker build services/model`.
- **Tests and types:** `make test` (pytest, including the Hypothesis property tests) and `mypy --strict` through `make lint`. Provider contract tests read synthetic fixtures; no vendor key existed, so no live provider run has happened (`tests/fixtures/providers/README.md`). `tests/test_proposer_swap.py` writes the proposals of the proposer swap test to `synthetic/proposer_swap/` (see its README); no network.
- **Why `noema_model/`.** The PERD §64.2 tree shows `understanding/` and `platform/` directly under `services/model/`. A top-level package named `platform` would shadow the standard library's `platform` module for anything that runs with this folder on `sys.path`, so the same packages sit one level down. See the PR for the recorded decision.

## Constraints

- **Readiness.** `/readyz` is 200 only when the harness was built from the file named by `NOEMA_MODEL_HARNESS_CONFIG`. A missing, unreadable, invalid or credential-less configuration leaves no harness and a reason (never a value from the file or a credential), and emits a `HARNESS_CONFIG_REJECTED` event. Credentials are read only from the files the configuration names (INV-29).
- **Decisions adapter: synthetic data only, not for Record-relevant use.** OpenAI documents one model name for the Decisions API (`gpt-6-luna`) and no dated snapshot, so the harness's pinned-version rule is not met and a reply is not reproducible (INV-21). It is public beta; abuse-monitoring retention is up to 30 days unless zero data retention is approved. No non-synthetic data goes to it until the data terms are reviewed (PERD R-18). Nothing routes to it unless a founder-approved config names it.
- **A request with no time window** is refused at the gate as `INPUT_INVALID`, and nothing is read. `NO_TRANSACTION_MATCH` is only for a search that ran and came back empty (REQ-AMB-002).
- **A counterparty filter** that is empty or over 64 characters is refused (`INPUT_INVALID`); it is never truncated or dropped.
- **Bench cost files.** `main.py` builds the service's telemetry from the environment (`build_telemetry`). Only `make model-bench-up` sets `BENCH_OUT` (to `/bench-out`, a bind mount of the Makefile's `BENCH_OUT`, with the invoking user's uid), through `deploy/compose/bench-model.yml`; `make up` and CI set nothing and mount nothing writable. With `BENCH_OUT` set, each call's `CallRecord.to_dict()` is written to `$BENCH_OUT/cost/<correlation_id>-<NNN>.json` (see `harness/README.md`). A correlation id outside `[A-Za-z0-9_-]` (or empty, or over 246 characters) and the 1000th call for one id are refused with `CostFileRefused`, not normalised. Accounting only, never proof of an outcome (AGENT-21).
- No threshold, margin, floor, TTL or provider default is set here (AGENT-16); ranking numbers are pack data.

## Open

- No request endpoint: `POST /internal/v1/understanding:compile` (PERD §39.3) needs the lexicon normaliser, the schema gate and a mounted pack, none built. The `Harness` exists on `app.state.harness` once configured, so a call made by a later endpoint would write its cost file under a bench run.
- No harness configuration is mounted by compose; its values (providers, pins, routes, rates) are founder decisions (AGENT-16). `NOEMA_MODEL_HARNESS_CONFIG` names the file; until it is set `/readyz` is 503 with the reason.
- `make model-bench-up` (compose override `deploy/compose/bench-model.yml`) was checked with `docker compose config`; no container was started here, so the bind mount and the uid mapping are untested end to end (not run: no Docker daemon).
- A dated snapshot or an accepted alias for the Decisions API, `native_yes_threshold` and any choice-confidence threshold: founders.
- Data-terms review (PERD R-18) before any non-synthetic data reaches a provider.
- Live-provider runs: not run, no access.
- OpenAI Responses adapter: fixtures only, from the reference of 2026-10-27; no live call. The model selection that uses it is frozen in `benchmark/decisions/2026-10-27-model-selection.md`. Multi-provider support is not complete: Anthropic and Google have fixtures only.
