# Status · CON-06 cross-assignment negative tests

Updated 2026-11-17. Owner: C1.

# Session 2026-11-17 · CON-06 (OD-P-10, DEC-2026-10-17 section 3)

## Session summary
- What changed: `services/core/internal/contracts/cross_assignment_test.go` (Go) and `services/model/tests/test_cross_assignment.py` (Python). Every closed list of `enums.v1.json` is tried against every other: a value of one is refused by each other list that does not hold the same word (1,188 value-and-list pairs, in Go and in Python); a registry names each closed list, so a new one fails until it is added. The two pairs of OD-P-10 are named: `OUTCOME_UNKNOWN` is an `ExecutionState` and `EXECUTION_OUTCOME_UNKNOWN` is not; `S0` to `S3` are finding severities and `S3_financial_pii` is not (Python also tests the other direction, `DataSensitivity`, a pattern).
- Why: the plan's done-when "each cross-assignment negative test fails validation in Go and in Python; the names themselves are unchanged".
- Tests added (REQ-API-001): above. Seen: no red run, because the contract is already distinct; checked with mutants instead: `EXECUTION_OUTCOME_UNKNOWN` added to the Go `ExecutionState` decoder fails the Go matrix and the named case; the same word added to the Pydantic `EnumsV1ExecutionState` fails two Python tests.
- Findings for the Architect (not changed here): (1) the generated Go types do not check patterns (tools/codegen/README.md), so `DataSensitivity` is checked in Python and by the contract validator only; a Go consumer cannot refuse `S3` as a sensitivity. (2) The synthetic transaction classification (`classification` in `context-snapshot.v1` and `benchmark-case.v1`) is a free string matching `^[A-Z][A-Z0-9_]*$`, so it accepts `OUTCOME_UNKNOWN` (an `ExecutionState`) and every other upper-case word: OD-P-10's "distinct enum type per field" is not true for that field. The smallest safe default is a `TransactionClassification` closed list of the twelve transaction classifications of the PERD v1.18 table in `enums.v1.json`, used by both fields: a contract change with the AGENT-15 bump and a migration note. Not built without the Architect's ruling.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Architect rules on finding (2); then the enum, the two `$ref`s, a fixture that refuses `OUTCOME_UNKNOWN` as a classification, regenerate.
- Do not touch: generated files by hand.

# Session 2026-11-17 · CON-06 the transaction classification as its own closed list (noema#665; OD-P-10, A272, A280)
- What changed: `enums.v1.json` `TransactionClassification` (the twelve of PERD v1.18, equal to `reconcile.Classification`); `context-snapshot.v1` and `benchmark-case.v2` refer to it; registry row and drift mapping; the CON-06 matrices of both languages hold it. `benchmark-case.v1` is not edited: a test pins it, and v2 supersedes it.
- Tests (REQ-API-001, REQ-RES-001): two invalid fixtures (`OUTCOME_UNKNOWN` as a classification), `TestTheClassificationsOfTheReconcilerAreExactlyTheContractsTransactionClassification`; red first at `cecd857f` (the `contracts` job and one Go shard; nothing else).
- Left for the Architect: `benchmark-case-result.v1/v2`, `benchmark-arm-result.v1`, `explanation-facts.v2` (a pack-named classification) and `reconstruction.v1/v2/v3` still hold the pattern; each is the same one-line change once scoped.
