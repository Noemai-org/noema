# UND-13 · Model-drift canary (benchmark half)

Owner: C2 and B (the Founder). Role agent: Benchmark Owner. Status: In progress. REQ-INT-006, D25, AGENT-28.

## Messages
- from: Founder (6 Nov 2026) · to: Benchmark Owner · purpose: build the qualification runner and the record format; C2 builds the model-service check · used for: scope of this PR (`benchmark/qualification/` only).

# Session 2026-11-06 · UND-13

## Session summary
- What changed: `benchmark/qualification/` (qualification record and lookup, `pins.py`, the drift canary format: subset, pinned baseline, comparison and run record in `run.py`; CLI `record`, `check`, `subset`, `baseline`, `compare`; `records/`), two new contracts `contracts/schemas/qualification-run.v1.json` and `qualification-record.v1.json` with fixtures, inventory rows, migration note and regenerated Go and Pydantic types, a CODEOWNERS line for `benchmark/qualification/records/`, one row in `benchmark/README.md`.
- Why: a model or speech version must not be used without a recorded qualification that includes pass^k (REQ-INT-006).
- Risks, and how they are contained: ruling A17 sets the interim rule (complete, Tier 0 zero, drift PASS, pass^k not below the baseline; a first qualification waits for a Founder-set floor); no number is chosen here. Records are unsigned JSON in git (DEBT-17, filed in docs).
- Tests added: `benchmark/qualification/test_qualification.py` (REQ-INT-006), red in CI before the code.
- Regression cases added: none (new module).
- Debt entries: none.
- Conflicts raised: none. Open decision: the Founder-set pass^k floor (A17); until it exists and no baseline record exists, every first qualification is NOT_QUALIFIED.

## Pre-work for the next agent
- Exact next step: C2 wires the model service's configuration check to the same rules as `check_model` (README, "The lookup contract").
- Open questions (owner): the qualification bar (Founders); where records live in production (C2, proposal: `benchmark/qualification/records/`).
- Files to read first: `benchmark/qualification/README.md`, `record.py`.
- Do not touch: `services/model/**` (C2).

## Messages (added 2026-11-06)
- from: Core Engineer 2 · to: Benchmark Owner · purpose: UND-13 split; C2 builds the drift canary in `services/model` and builds to a record format published first · used for: `run.py`, `schemas/qualification-run.v1.schema.json` and the README section "The drift canary" (I chose a schema inside `benchmark/qualification/`, not `contracts/`, so no codegen or AGENT-15 contract change is needed; if C2 wants it in `contracts/`, that is a separate change).

- from: Architect (rulings of 6 Nov 2026, A17) · to: Benchmark Owner · purpose: the schema `qualification-record.v1.json` in `contracts/` with the AGENT-15 steps and a CODEOWNERS line; records in `benchmark/qualification/records/`; interim QUALIFIED rule · used for: the contract move, the record rule and the records folder. The run record's schema moved to `contracts/` as well, because the model service's canary builds to it.

# Session 2026-11-10 · UND-13 (record lineage)

## Session summary
- What changed (PR links): noema#199. SR N1 (the bar is re-read from the baseline record it names by digest) and N2 (the five runs with their digests are in the record); EL findings 1 (kind applied after validation) and 2 (no first-qualification floor for a re-qualification); EL 3 (a huge int is not a rate), C1 items (fixtures valid by the code, `--kind` required on `check`, citation of A43 and DEC-2026-11-06-model-qualification-floor).
- Why: a typed bar and prose-only run evidence were accepted; a malformed revoking record could be skipped.
- Risks, and how they are contained: `qualification-record.v1` is not yet merged, so the two new required members need no version bump; C2's gate in noema#215 must read them (flagged on both PRs).
- Tests added (REQ-INT-006, REQ-BEN-009, AGENT-28): `Lineage` in `test_qualification.py`.
- Regression cases added: the same.
- Debt entries: none. SR's N1 is fixed, not deferred.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: C2 aligns noema#215 to `runs` and the baseline lookup; reviewers re-read #199.
- Open questions (owner): none.
- Files to read first: `benchmark/qualification/record.py`, `README.md`.
- Do not touch: `benchmark/qualification/records/` (CODEOWNERS).
