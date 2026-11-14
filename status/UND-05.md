
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

# Session 2026-11-14 16:05 WAT · UND-05 · the minimal lexicon and normaliser (compiler slice 4, noema#584)

## Session summary
- What changed (PR noema#584): `packs/fintech/lexicons/fin.lex.en_ng.json` (pack data, every entry a `[synthetic seed]`), `understanding/compiler/lexicon.py` (`Lexicon`, `Normaliser`), `tests/test_lexicon.py`, the compiler README.
- Why: the Founder's order of 14 Nov 2026 ("then the compiler"). `Normalise` was a seam with no implementation; G-A to G-E need an amount and a window. The pack's README and `understanding.md` assign `fin.lex.*` to UND-05.
- Risks, and how they are contained: nothing is invented (a value the customer's own words do not hold is left unread, never taken from the model); no threshold is decided here (every number is a seed for a benchmark and a founder decision, AGENT-16); no dependency (a fixed offset, not `zoneinfo`, AGENT-14); no float (INV-21). Not wired.
- Tests added (REQ-INT-003; INV-14, INV-21): 71, red first. Twenty-three hand mutations, all killed (two extra tests were added after two survived: the length bound and a clock of 24:30).
- Regression cases added: a model's amount, time or name never reaches the reading unless the customer's own words hold it.
- Debt entries: none. Conflicts raised: none. Open: noema#583 (the file's shape; Architect, EL).

## Pre-work for the next agent
- Exact next step: the `Compiler` (canonical input to `PipelineInput`, `run_pipeline` with this normaliser and the pack loader of noema#582, then `assemble` of noema#581), then `benchmark/compile_acceptance`. `compile_service` stays unwired until noema#555 closes.
- Open questions (owner): noema#580 (who sets `intent_id`, `created_at`, `raw_ref`); noema#583 (the lexicon's shape); the G-B flag (`COUNTERPARTY_MULTIPLE_MATCH` against the pack's `MULTIPLE_TRANSACTIONS_MATCH`) (Architect).
- Files to read first: `compiler/README.md`, `compiler/lexicon.py`, `packs/fintech/lexicons/fin.lex.en_ng.json`.
- Do not touch: the generated contracts; the seeds' numbers.
