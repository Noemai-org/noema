# services/model

The Python model service (the Go core is the other deployable). It hosts Understanding, Explanation and Voice, and is the only place with a model or speech provider SDK or credential (INV-29; SoT §35.6, PERD §64.2).

**Status (T5 and T6.3 of DEC-2026-10-20).** Built: the model harness (`harness/`: `generate`, `decide`, templates, masking, accounting, failover, breaker), the three `generate` provider adapters (`providers/llm/`: OpenAI, Anthropic, Google, over the standard library, no vendor SDK), the understanding pipeline and its six decision tasks (`understanding/pipeline/`), deterministic ranking and the D5 recipient matcher (`understanding/resolution/`), and the platform pieces they use (`platform/{clock,config,redaction,telemetry}`). The Evidence API client (`evidence/`, to the candidate-read contract of #42) is built. The OpenAI Decisions adapter (`providers/llm/openai_decisions.py`) is built for synthetic data only. Not built, and why, is in `docs/sessions/`: the schema gate, the lexicon normaliser, `explanation/` and `voice/`. `noema_model/main.py` still serves only `/healthz` and `/readyz`; nothing is wired to HTTP yet. There is no database driver, Temporal client or vendor SDK.

- **Python:** 3.13, pinned by `requires-python` in `pyproject.toml`. Dependencies are pinned with `==`, and `requirements.lock` / `requirements-dev.lock` carry every file hash (`pip install --require-hashes`). Regenerate with `make lock-model`.
- **Build context:** this folder only (DEC-2026-10-13 R2): `docker build services/model`.
- **Tests and types:** `make test` (pytest, including the Hypothesis property tests) and `mypy --strict` through `make lint`. Provider contract tests read synthetic fixtures; no vendor key existed, so no live provider run has happened (`tests/fixtures/providers/README.md`).
- **Why `noema_model/`.** The PERD §64.2 tree shows `understanding/` and `platform/` directly under `services/model/`. A top-level package named `platform` would shadow the standard library's `platform` module for anything that runs with this folder on `sys.path`, so the same packages sit one level down. See the PR for the recorded decision.

## Constraints

- **Decisions adapter: synthetic data only, not for Record-relevant use.** OpenAI documents one model name for the Decisions API (`gpt-6-luna`) and no dated snapshot, so the harness's pinned-version rule is not met and a reply is not reproducible (INV-21). It is public beta; abuse-monitoring retention is up to 30 days unless zero data retention is approved. No non-synthetic data goes to it until the data terms are reviewed (PERD R-18). Nothing routes to it unless a founder-approved config names it.
- **A request with no time window** is refused at the gate as `INPUT_INVALID`, and nothing is read. `NO_TRANSACTION_MATCH` is only for a search that ran and came back empty (REQ-AMB-002).
- **A counterparty filter** that is empty or over 64 characters is refused (`INPUT_INVALID`); it is never truncated or dropped.
- **Bench cost files.** With `BENCH_OUT` set, each call's `CallRecord.to_dict()` is written to `$BENCH_OUT/cost/<correlation_id>-<NNN>.json` (see `harness/README.md`). Accounting only, never proof of an outcome (AGENT-21).
- No threshold, margin, floor, TTL or provider default is set here (AGENT-16); ranking numbers are pack data.

## Open

- Wiring `cost_files_from_environment` and mounting `$BENCH_OUT`: needs whoever owns the service entry point and compose.
- A dated snapshot or an accepted alias for the Decisions API, `native_yes_threshold` and any choice-confidence threshold: founders.
- Data-terms review (PERD R-18) before any non-synthetic data reaches a provider.
- Live-provider runs: not run, no access.
