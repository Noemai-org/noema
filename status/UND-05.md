
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

# Session 2026-11-14 15:45 WAT · UND-05 · the assembler (compiler slice 2, noema#581)

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

# Session 2026-11-15 · UND-05 · the compiler (compile slice 5)

## Session summary
- What changed (PR to follow, after noema#581 and noema#584 merge): `understanding/compiler/compiler.py` (`PipelineCompiler`, `TenantScopeMismatch`, the `Minter` protocol), `tests/test_compile_compiler.py` (22 tests), the compiler README, this note.
- Why: the Founder's order of 14 Nov 2026 ("Compiler joining the assembler, pack loader and lexicon"). The route had no `Compiler` behind it.
- Risks, and how they are contained: it is untrusted-output code and `CompileService` still holds its bytes to the schema gate (the tests run it behind a real `CompileService` and `SchemaGate`); no identity from text or from the model; no provider call outside the harness; not wired in `main.py` (mTLS, noema#555).
- Tests added (REQ-INT-001, REQ-INT-002, REQ-INT-003, REQ-INT-005; INV-01, INV-02, INV-12, INV-21, AGENT-09): 22; 17 hand mutants on `compiler.py`, all killed (one needed a test that the provider sees the canonical text verbatim).
- Regression cases added: a voice request is refused before any provider call; a tenant the core refuses is never a provider failure; the open intent never reaches a provider.
- Defaults taken (AGENT-02): voice refused; `open_intent` not read; candidate set version 1; accept-all pipeline `Gate` seam (the schema gate follows); identifiers and the candidate set id from a `Minter`.
- Debt: none. Conflicts raised: none. Open question: noema#580.
- Security Reviewer's blocking finding (noema#598): the assembler copied the model's surface forms. Now `assemble.py` takes a surface form only if it is non-empty, at most 64 characters (the lexicon's bound), a substring of the canonical text and holds no digit run over four (digits separated by single spaces or hyphens count as one run). A Party or TimeRef slot whose surface fails is left out; a Quantity keeps the lexicon's value and loses only its surface form. Tests first: red at `2bc9992a` and `c9cc7dbd` (the second after the compiler test was corrected, as the first failed on the input gate, not its own assertion); 12 hand mutants on the new code, all killed.
- Consequence to decide (Architect): a Quantity whose own written amount has five or more digits ("12345") keeps its value and loses its surface form, because the Record's guard would refuse a digit run over four (noema#536). Masking the form instead is the alternative.

## Pre-work for the next agent
- Exact next step: the `Minter` and the wiring in `main.py` (after mTLS, noema#555), then the `benchmark/compile_acceptance` runner (noema#546).
- Files to read first: `compiler/README.md`, `compiler/compiler.py`.
- Do not touch: the generated contracts; the pack's numbers.
