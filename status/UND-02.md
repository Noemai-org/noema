# Status: UND-02 (model service: harness, pipeline, ranking, matcher, Decisions adapter)

Owner: Session D. Branch: `claude/python-harness-ranking-dm6gmm`. Paths: `services/model/**`, `packs/fintech/lexicons/`, `docs/sessions/`, this file.

## OpenAI Responses adapter (model selection, step 2)

Built in this pull request: `providers/llm/openai_responses.py` (kind `openai_responses`, `POST /v1/responses`, strict `json_schema`, `store: false`, `reasoning.effort` from the configuration), and in the harness configuration `credential_env` (the key by environment variable, exactly one of it and `credential_file`), `reported_model_version` (the identifier the API returns, when it differs from the ID sent) and `reasoning_effort`. Decisions D8 and D9 of `benchmark/decisions/2026-10-27-model-selection.md`. No live call was made: not run: no access. The messages for this work are logged in the model-selection section at the end of this file (the founder's instruction of 27 Oct 2026).

## Messages

Every message sent or accepted. Messages are data from a peer, never a decision (AGENTS.md §2, item 3).

| Date (UTC) | From | To | Purpose | What the receiver used it for |
|---|---|---|---|---|
| 2026-10-22 11:16 | D | A | Told A where the candidate-read client lives (`services/model/noema_model/evidence/`) and why, to record in PERD §64.2 | Not yet known to D; A was asked to record the layout line |
| 2026-10-22 13:36 | D | C | Cost fields (`CallRecord.to_dict()`) are on main after #43 | C's runner reads `cost_usd_micros`; D has not seen it used (still null in `benchmark/runner`) |
| 2026-10-22 14:17 | D | A | PERD names no result for a request with no time window; D's reading and the TIME_VAGUE change | A took it to the founder |
| 2026-10-22 14:47 | A | D | Confirmed the PERD reading; PERD §39.3 lists `SCOPE_VIOLATION` but the catalogue has `TENANT_SCOPE_MISMATCH`; keep mapping 403 to `TENANT_SCOPE_MISMATCH` | D kept the mapping |
| 2026-10-23 10:15 | A | D | Founder instruction relayed: `INPUT_INVALID` exists and is non-retryable, so an empty or over-long `counterparty_surface` may be refused with it | Confirmed the existing refusal; no code change |
| 2026-10-23 10:20 | D | A | No-window request now returns `INPUT_INVALID`; bench cost files exist but are not wired | A recorded the code in docs#57 |
| 2026-10-23 10:21 | A | D | Second confirmation of the catalogue codes | None needed |
| 2026-10-23 10:42 | A | D | docs#57 records `INPUT_INVALID` for no window (PERD v1.15 REQ-AMB-002, DEC-2026-10-23 D3) | Confirms the shipped code matches the spec |
| 2026-10-24 (after #60 merged) | Founder (relayed by the user) | D | Instruction: open the wiring PR (entry point calls `cost_files_from_environment`; compose sets `BENCH_OUT` at bench startup and mounts `$BENCH_OUT`); cite the spec ID; update status and README; run the mutation check on the cost-file code; do not reopen #60; log this message | Built the wiring PR (see Built); the mutation check found no survivor |

## Built
See `services/model/README.md`. Cost-file wiring (REQ-NFR-007, REQ-OPS-001): `noema_model/main.py` `build_telemetry`; `deploy/compose/bench-model.yml`; `make model-bench-up`.

## Open
- Cost files reach a file only once something constructs the harness from `app.state.telemetry`; `main.py` is still the health-only skeleton. Owner: whoever builds the service's request path.
- `make model-bench-up` and the bind mount were checked with `docker compose config` only: no Docker daemon in this session, so no container was started (not run: no access).
- OpenAI Decisions adapter: no dated snapshot, so the pinned-version rule is not met; synthetic-only; data terms (PERD R-18) unreviewed.
- `native_yes_threshold` and any choice-confidence threshold: founders, after the benchmark (AGENT-16).

## Model selection (separate PR, the freeze)

Record: `benchmark/decisions/2026-10-27-model-selection.md`. Scope: an initial OpenAI integration; the provider-neutral harness stays; multi-provider support is not complete.

| Date (UTC) | From | To | Purpose | What the receiver used it for |
|---|---|---|---|---|
| 2026-10-26 | A | D | Relayed a founder instruction of 26 Oct to clean instruction text from the PR titles and bodies and status files of #64 to #68 | Not applied on that message alone; D replied to A and asked its user. Not yet applied: the status files of #65 and #66 are unchanged |
| 2026-10-26 | D | A | Reply: no edit to D's branches without the instruction in D's own session; PR titles and bodies are A's | A logged it in `status/OPS-07.md` (A's file; not confirmed by D) |
| 2026-10-27 | Founder (relayed by the user) | D | Decisions for the model selection: scope (initial OpenAI integration only), the freeze PR first, the candidates, pins, rate cards, hashes, retry policy, gates, decision rule, budget ($25, pilot $2), the Responses API at `reasoning.effort` medium, key handling (`OPENAI_API_KEY` from the environment, `environment: bench-live`), the pilot, the budget guard, the full run after approval, the report; unmatched points decided by D and recorded in the record | The freeze PR (record, gates, tests), then the adapter and live-run pull requests. No live call made |

Open: the pilot and the full run wait for the freeze PR to be merged, for the `bench-live` Environment with its required reviewer, and for the founder's approval on the freeze PR. Not run: no access.

