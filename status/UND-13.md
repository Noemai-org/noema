# UND-13 · Model-drift canary (benchmark half)

Owner: C2 and B (the Founder). Role agent: Benchmark Owner. Status: In progress. REQ-INT-006, D25, AGENT-28.

## Messages
- from: Founder (6 Nov 2026) · to: Benchmark Owner · purpose: build the qualification runner and the record format; C2 builds the model-service check · used for: scope of this PR (`benchmark/qualification/` only).

# Session 2026-11-06 · UND-13

## Session summary
- What changed: `benchmark/qualification/` (record format `qualification-record.v1`, `build_record`, `validate_record`, `verify_against_scorecard`, `check_model`, CLI `record` and `check`), one row in `benchmark/README.md`.
- Why: a model or speech version must not be used without a recorded qualification that includes pass^k (REQ-INT-006).
- Risks, and how they are contained: the verdict uses only three conditions (complete, Tier 0 at zero, pass^k available); no threshold is chosen. The quality bar is a founder decision (see Conflicts).
- Tests added: `benchmark/qualification/test_qualification.py` (REQ-INT-006), red in CI before the code.
- Regression cases added: none (new module).
- Debt entries: none.
- Conflicts raised: none. Open decision: the minimum pass^k or other bar for a QUALIFIED verdict is not set by any committed record; the module records the numbers and leaves the bar to the decision record named in each record.

## Pre-work for the next agent
- Exact next step: C2 wires the model service's configuration check to the same rules as `check_model` (README, "The lookup contract").
- Open questions (owner): the qualification bar (Founders); where records live in production (C2, proposal: `benchmark/qualification/records/`).
- Files to read first: `benchmark/qualification/README.md`, `record.py`.
- Do not touch: `services/model/**` (C2).
