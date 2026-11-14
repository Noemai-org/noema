
# Session 2026-11-10 · UND-05 · a hard stop for the live run (DRAFT; the USD 5 figure is not yet in docs/decisions)

## Session summary
- What changed: `benchmark/bakeoff/live_run.py` gets `--hard-stop-micros` (optional, positive integer; the guard's limit is the smaller of the phase cap and the hard stop, so it can only tighten; prior spending counts). `.github/workflows/bench-live.yml` gets a required `hard_stop_usd` input (default 5), validated as a positive whole number in the Run step and converted to micro-dollars. The workflow still reads only `OPENAI_API_KEY`; no other secret was added.
- Why: Architect sync of 10 Nov 2026: one OpenAI-only synthetic run with a USD 5 hard stop (A79 under the USD 25 cap is committed; the USD 5 figure is the sync's and is not yet in docs/decisions). Security Reviewer reviews the workflow change before any value is added (noema#338).
- Risks, and how they are contained: workflow_dispatch only; `environment: bench-live` (required reviewer); the key reaches one step; the hard stop cannot raise the phase cap; the phase caps ($2, $25) in `gates.py` are untouched. A live run needs the Founder to place `OPENAI_API_KEY` in `bench-live` and confirm the name on noema#338; nothing was run.
- Tests added (REQ-BEN-005, REQ-NFR-007, REQ-AGT-005): `HardStop` in `benchmark/bakeoff/test_live_run.py` (red first, 520c041): below-cap hard stop is the limit; above-cap cannot loosen (full and pilot); none leaves the cap; prior spending counts; non-positive and non-integer refused.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: Security Reviewer reads the workflow diff (noema#338); the Founder confirms the key name; no live call before both.
- Files to read first: `.github/workflows/bench-live.yml`, `benchmark/bakeoff/live_run.py`.
- Do not touch: `benchmark/bakeoff/gates.py` caps.

# Session 2026-11-13 21:30 WAT · UND-05 · the model service's compile route (slice 1, noema#544)

## Session summary
- What changed (PR noema#544): `POST /internal/v1/understanding:compile` and `CompileService` in `services/model/noema_model/understanding/compiler/` (route, service, README); `create_app(compile_service=None)` mounts it (no service: 503 `DEPENDENCY_UNAVAILABLE`, never 404); the model-side schema gate moves to `intent-object.v3` / `intent-slot.v2` (ruling A173: confidence as integer basis points, `schema_version` `3.0`).
- Why: the core's client calls this route and the model service had none (G1 needs US-01 through the public API, noema#436). Ruling A188 asks for the compiler against `intent-object.v3`.
- Risks, and how they are contained: the route never repairs, signs or hashes a proposal (A166); a reply the gate refuses is a `PROVIDER_UNAVAILABLE` (502, retryable) with no reason in the answer (AGENT-13); a bug is a 500 `INTERNAL_SAFETY_BLOCK`, never hidden as a provider failure; size bound and deadline come from configuration, no default (AGENT-16). The core's gate still decodes v1: its move to v3 is a separate change of mine.
- Tests added (REQ-INT-001, REQ-INT-002, REQ-API-002; INV-02, INV-12, AGENT-09): `tests/test_compile_route.py` (written first; red run 43506966574, 12 failed and 1090 passed), `tests/test_intent_gate.py` moved to v3 (non-integer and out-of-range confidence is a shape refusal; schema version 3.1 refused).
- Regression cases added: duplicate-member request is refused as `INPUT_INVALID` (it first produced a 500); a canonical input of major 3 is refused.
- Debt entries: none. Conflicts raised: none. The red commit's docstring named a core path and failed `py-cross-service` (arch-lint); the implementation commit rewords it.

## Pre-work for the next agent
- Exact next step: the compiler itself (prompt templates, lexicon, assembly of the `intent-object.v3` proposal, the `providers/` call) behind the `Compiler` protocol; wire a `CompileService` in `main` once a real pack loader exists (PCK-01); then the core's gate and proposal move to v3.
- Open questions (owner): `principal_ref` in the canonical input must carry the session's `usr_` id (C1, input gateway); noema#528 (recorded-replay provider) and noema#546 (compile acceptance) for the live run.
- Files to read first: `services/model/noema_model/understanding/compiler/README.md`, `.../gate/README.md`.
- Do not touch: the generated contracts; the gate's reason set.

# Session 2026-11-14 11:00 WAT · UND-05 · the Security Reviewer's NOT READY on noema#544

- **Finding 2 (unbounded request body): fixed.** `CompileService` takes `max_request_bytes` (required, positive, no default). The route answers 503 before reading anything when no service is wired; otherwise it refuses a declared length over the bound without reading a byte (413, `INPUT_INVALID`), cuts a body with no declared length as it streams, and refuses a malformed `Content-Length`. Failing first: `tests/test_compile_route.py` at `3b0b2ffa` (the `model` job failed, run 43586455374). Six mutants of the new code were run: five killed by the route tests; one (the declared-length check ignored) survived until an ASGI-level test that the receive channel is never touched was added, and is now killed.
- **Finding 1 (mTLS on the model listener): not fixable in this PR.** It needs `services/model/Dockerfile` and `deploy/compose` (IL's, protected). Tracked on noema#555; stated in `compiler/README.md` as the first precondition before any PR sets `compile_service` in `main.py`. The route compiles nothing until then.
- **Notes 3 to 6:** recorded as preconditions in `compiler/README.md`; none is reachable while no compiler is wired.
- Debt: none. Conflicts raised: none.

# Session 2026-11-14 15:50 WAT · UND-05 · the pack loader (compiler slice 3, noema#582)

## Session summary
- What changed (PR noema#582): `understanding/compiler/packconfig.py` (`load_pipeline_config`), `tests/test_pipeline_config_loader.py`, the compiler README.
- Why: the Founder's order of 14 Nov 2026 ("then the compiler"). `PipelineConfig` was built only in `tests/support_pipeline.py`; nothing read the pack in production.
- Risks, and how they are contained: no value is written in code or defaulted (AGENT-16); a pack that is not whole fails at load. The yes threshold of a native decision model stays unset. A matcher reference outside the pack's folder is refused. Nothing is wired.
- Tests added (REQ-AMB-002, REQ-AMB-003, REQ-INT-005; INV-14, INV-21, AGENT-16): 44, red first (run 43606750507, `model` failed at collection with `ModuleNotFoundError`). Nineteen hand mutations: sixteen killed, three survive and are equivalent (a bool, a negative margin and a limit of 0 are each refused again downstream by `RankingConfig`, so the loader's own check is a second line).
- Regression cases added: a margin or a limit written in `understanding.ranking` is refused (one source only); a matcher file that is a symlink out of the pack folder is refused.
- Debt entries: none. Conflicts raised: none.
- Defaults taken (stated in the PR): a JSON number for the margin is read as its shortest decimal; exactly one candidate node is allowed.

## Pre-work for the next agent
- Exact next step: the minimal lexicon (pack data, `[synthetic seed]`) and `Normalise`, then the `Compiler`, then `benchmark/compile_acceptance` (plan on noema#546).
- Open questions (owner): noema#580 (who sets `intent_id`, `created_at`, `raw_ref`; Architect, EL).
- Files to read first: `compiler/README.md`, `compiler/packconfig.py`, `packs/fintech/understanding.md`.
- Do not touch: the generated contracts; the pack's numbers.
