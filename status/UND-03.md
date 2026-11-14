# Status: UND-03 (provider adapters and failover): the harness masking re-freeze, Benchmark Owner step

Owner of this section: Benchmark Owner role agent. Authority: ruling A162 and `DEC-2026-11-12-harness-masking-refreeze` (docs#158, merged). Needs: noema#468 (adds `masking-limits.v1`) and the Engineering Lead's code half.

## Session 2026-11-13 · UND-03 · harness masking re-freeze

## Session summary
- What changed: the two harness configurations lose their `masking` member; the new dated record `benchmark/decisions/2026-11-13-harness-masking-refreeze.md` carries before and after hashes, the pin of the shared `masking-limits.v1` (floor 4) and which results used floor 8; the 27 Oct record's frozen block holds the new hashes, the old ones under `superseded_harness_configs`, and `masking_limits`; the live-run preflight refuses to start if the pin differs; the verifier accepts a run made under a superseded hash and the report labels it "made under masking floor 8".
- Why: A162 (the PERD wins; prompts are an outward surface).
- Risks, and how they are contained: tightening only. noema#468 merged on 14 Nov 2026 with a loader that accepts a harness config with or without the `masking` member (ignored if present), so the model tests that load the real configs pass (60 passed locally). `main` was merged in and the pinned hashes of `masking-limits.v1` were re-checked against the merged files: unchanged. A follow-up from the Engineering Lead makes the loader refuse the member (order A, noema#487).
- Tests added: `HarnessMaskingRefreeze` (5), `TestSupersededConfiguration` (3), four preflight mutations. Each new guard was broken once by hand and a test went red.
- Regression cases added: none (benchmark tooling; no case).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Engineering Lead's follow-up that makes the harness loader refuse an old configuration that still has the `masking` member (a config without it already loads).
- Open questions (owner): none.
- Files to read first: `benchmark/decisions/2026-11-13-harness-masking-refreeze.md`.
- Do not touch: the frozen hashes, except by a new dated record.
