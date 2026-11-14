
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

# Session 2026-11-14 16:00 WAT · UND-05 · the assembler (compiler slice 2, noema#581)

## Session summary
- What changed (PR noema#581): `understanding/compiler/assemble.py`, a pure function from the pipeline's result and the canonical input to the bytes of an `intent-object.v3` proposal; `tests/test_compile_assemble.py`; the compiler README.
- Why: the Founder's order of 14 Nov 2026 ("then the compiler"). The route (#544) has no `Compiler` behind it, and nothing turned a `PipelineResult` into a proposal. The plan of five slices is on noema#546.
- Risks, and how they are contained: the proposal is untrusted and crosses only the schema gate; it carries no identity beyond the canonical input's, no `sig`, no outcome. The amount and the window are the lexicon's, a mention the lexicon cannot read is left out (never guessed). A rejected result is never assembled. Nothing is wired: `compile_service` stays unwired until noema#555.
- Tests added (REQ-INT-001, REQ-INT-002, REQ-INT-003, REQ-INT-005; INV-01, INV-02, INV-06, INV-12, INV-21): 27, red first (run 43606420465, `model` failed at collection). Sixteen hand mutations all killed; six extra tests were added for the three that survived the first 21 (a float read as binary, a window in another zone, unsorted flags) and for the clamp, the empty surface form and two slots of one lexicon type.
- Regression cases added: a model's amount never reaches the proposal; a rejected result is never assembled.
- Debt entries: none.
- Conflicts raised: none. Open question: noema#580 (who sets `intent_id`, `created_at`, `raw_ref`).
- Test corrected, not weakened: one test (a time with no window) asserted a result the pipeline never produces for the investigate node (it refuses a candidate search with no window, REQ-CTX-006); it now builds that result from a real one and asserts the same rule.

## Pre-work for the next agent
- Exact next step: slice 3, the pack loader (`PipelineConfig` from `packs/fintech_v1.json`), then the minimal lexicon and `Normalise`, then the `Compiler`, then `benchmark/compile_acceptance` (plan on noema#546).
- Open questions (owner): noema#580 (Architect, EL). The G-B golden example expects `COUNTERPARTY_MULTIPLE_MATCH`, but the pack's `state_flags` map the ranking's AMBIGUOUS to `MULTIPLE_TRANSACTIONS_MATCH` and `packs/fintech/understanding.md` says the engine has no counterparty-level ranking: that example cannot pass as the pack stands (Architect).
- Files to read first: `compiler/README.md`, `compiler/assemble.py`, `tests/test_compile_assemble.py`.
- Do not touch: the generated contracts; the gate's reason set.
