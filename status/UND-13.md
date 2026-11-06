# Status · UND-13 (Core Engineer 2, with the Benchmark Owner) · model-drift canary

Updated 2026-11-06. Plan task UND-13 (owner C2, B; REQ-INT-006; Needs TST-02, BEN-30). Ruling A17.

## Session 2026-11-06 · slice 1: the record schema
- What changed: `contracts/schemas/qualification-record.v1.json` (new), fixtures (4 valid, 12 invalid), INVENTORY row, regenerated Go and Pydantic types and the shape-refusal list, `benchmark/qualification/` (README and `records/`), and a CODEOWNERS line for `/benchmark/qualification/` (named by A17; both Founders approve).
- The schema: record ID, provider, pinned model and speech model IDs, run ID, commit SHA, qualification-set digest, cases run, run complete, Tier 0 failures, the drift check (PASS, DRIFT with the differing slots, PROVIDER_UNAVAILABLE), pass^k counts (k, cases, successes, baseline or null) and the verdict (QUALIFIED, NOT_QUALIFIED, PENDING_FLOOR). Conditional rules: QUALIFIED needs a complete run, zero Tier 0 failures, drift PASS and a baseline; PENDING_FLOOR has no baseline.
- Not in the schema: the pass^k bar (pack data set by a decision record, A17) and any held-out or oracle material (INV-22).
- Split with the Benchmark Owner (posted on noema#138): they build the runner and the record format's producer; I build the model-service-side drift check, to this schema.
- Order of work: schema and fixtures were written together and checked by `tools/contracts/validate.py` (it requires a valid and an invalid fixture and an INVENTORY row per schema); there is no separate tests-only commit.
- Not built: the drift check in `services/model` and the harness refusal of an unqualified model ID (next slices; the Python code needs the Benchmark Owner's qualification subset to run).
