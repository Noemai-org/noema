# Status · TRU-01 · The seven-outcome type; unresolved intent can only build CLARIFY or ESCALATE

Updated 2026-11-03. Owner: C1+AG in tasks.csv; executed by the Engineering Lead role agent (TRU-01 to TRU-12 under its role prompt).

## State
- In review (draft PR from `feat/TRU-01-decision-basis`). The seven outcomes and the constructors that make an ambiguous ALLOW unrepresentable were on `main`; this task adds the Decision's basis (pack version, policy version per rule, evidence snapshot and freshness, evaluation time).

## Open
- Needs CON-03 (codegen), `Ready`, not started. The Decision is domain code, not a contract type, so nothing here waits on it; the Decision Record contract fields arrive with TRU-05.
- `Evaluate` (TRU-02, noema#109) will refuse an ALLOW or CONFIRM without a complete basis once both are on `main`: a small follow-up commit on that branch.

# Session 2026-11-03 · TRU-01

## Session summary
- What changed (PR links): `trust/decide/basis.go` (new), `basis_test.go` (new), `decision.go` (one field), trust README, this file.
- Why: REQ-TRU-001 (attribution of every decision); the Founders' work order of 3 Nov, item 4 ("finish TRU-01: policy version, pack version and evidence on the Decision").
- Risks, and how they are contained: the basis is a value the caller supplies; Trust does not check that the snapshot ID or evidence IDs exist (that is the Decision Record's write, TRU-05). The shape of `evidence_freshness[]` is not specified beyond its name; it holds the evidence ID and its effective time, which is all REQ-TRU-001 and the Evidence contract name.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-001 (basis carried, copies, refusals, ALLOW/CONFIRM need a full basis, other outcomes the minimum); REQ-AMB-001, INV-06 (a 5,000-run property with testing/quick: an unresolved intent yields only CLARIFY or ESCALATE with any basis attached; this also answers docs#77's note that the TRU-01 test was a seeded loop, not a property test). Seen failing first (did not compile). Mutation check: 5 of 5 caught.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: TRU-05 (the Decision Record written before grants) builds the record from Decision + Basis; TRU-02's `Evaluate` adds the "ALLOW needs a complete basis" acceptance rule after both PRs merge.
- Do not touch: noema#94, noema#90.

## Session 2026-11-04 · the unresolved-intent property was tautological (Architect's decision 3)
- Finding: `TestAmbiguityNeverYieldsAnotherOutcomeWithAnyBasis` (basis_test.go) only called `Clarify` and `Escalate` on an unresolved intent and checked the outcome those two set, so it could not fail. Measured on 4 Nov 2026: three mutants of `Admit` survived it on its own (ignore the material flags; treat an Absent slot as resolved; treat an Ambiguous slot as resolved). (The package as a whole did catch them, through `TestAmbiguousIntentsNeverYieldAnotherOutcome` in decide_test.go, which compares `Admit` with an independent clean/not-clean reference; that test is unchanged.)
- Fix: the property now carries an independent reference of "clean" (no material flag, every required slot Resolved); asserts that `Admit` gives exactly one side and the side the reference gives; that versions are carried on both sides; that every constructor of an unresolved intent yields only CLARIFY or ESCALATE with the intent's version, the reason it was given, and no authority beyond the technical layer (policy and business NOT_EVALUATED, so nothing here can become an ALLOW); and that the generator reached every way an intent is unresolved (flag-only, Unresolved, Ambiguous, Absent slots) or the test fails. 20,000 seeded cases.
- Mutation check, the test run alone: 7 of 7 mutants of `Admit` and 3 of 3 of the two constructors caught (before: 3 of the `Admit` mutants survived).
- TRU-01 stays In progress until this merges (the Architect's rule); the board is the owner's to move.
