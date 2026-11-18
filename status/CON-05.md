# Status · CON-05 · money lint (C1+AG; the Repo Steward's session)

Updated 2026-11-17. Plan task CON-05 (REQ-DATA-002; DEC-2026-10-16 money-and-value-lists M1, M4, M6).

## State
- Four rules added to `tools/lint`: `money-literal-multiplier` and `money-decimal-field` (Go), `py-money-multiplier` (Python) and `money-sql-type` (migrations). Each has a canary that fails CI, and `clean__money-allowed` is a positive control. The tree is clean. `test_money_rules.py` covers the edges, wired into the `arch-lint` workflow.
- The Go and Python float rules (`money-float-area`, `money-float-field`, `py-money-field`) were already there (PLT-03).

## Open
- None for CON-05's done-when. Whether a money value may be a `Decimal` in the lexicon's spoken-amount conversion (M5) is allowed by name: the rules read `amount*` and `*minor` only.

## Session 2026-11-17 · CON-05

### Session summary
- What changed: see State. Files: `tools/lint/golint/rules.go`, `tools/lint/pyrules.py`, `tools/lint/run.py` (a `clean__*` tree is expected to fire nothing), `tools/lint/test_money_rules.py`, the canaries, `tools/lint/README.md`, `.github/workflows/arch-lint.yml` (one step).
- Why: the audit of 17 Nov found no rule for NUMERIC, REAL or MONEY in migrations, none for Decimal in the core, and none for a literal exponent multiplier (M1).
- Risks, and how they are contained: a name-based rule can miss a money value with another name (it flags `amount*` and `*minor` only); a power of ten on a non-money name is allowed by design. The tree was clean before and after.
- Tests added (REQ-DATA-002): the four canaries, `clean__money-allowed`, and `test_money_rules.py` (nine tests, 56 cases). Red first: the three Go and SQL canaries failed ("rules fired none") before the rules existed. Mutants: a Python power-of-ten test forced false, the Go threshold raised to 1000 and the SQL cast pattern removed each fail the tests.
- Regression cases added: the canaries and the test file.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for CON-05. The Steward marks it Done on merge with the check-run link.
- Files to read first: `tools/lint/README.md`.
- Do not touch: `tools/lint/canaries` by hand except to add a canary with its rule.
