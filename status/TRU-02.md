# Status · TRU-02 · Gate order T1 to T15, evaluation trace, panic becomes UNKNOWN

Updated 2026-11-01. Owner: C1 in tasks.csv (C1); executed by the Engineering Lead role agent, which owns TRU-01 to TRU-12 under its role prompt.

## State
- In progress. PR from `feat/TRU-02-gate-order` (second branch approved by the Architect, 1 Nov 2026).
- Built: `decide.Evaluate`, the runner and its trace. Not built: the step functions T7 to T15 (TRU-03 to TRU-11) and the Trust intake that assembles them.

## Open
- Needs TRU-01, which docs#77 lists as In progress (its ambiguity test uses seeded `math/rand`, not a property library). This PR's property test uses the standard library's `testing/quick`, which adds no dependency.
- §23.3's T10 row names ESCALATE only, while PERD v1.19 REQ-RES-005 (D10) puts WAIT inside a convergence window. The runner follows the §23.3 row, so a WAIT at T10 is refused and becomes UNKNOWN (the safer reading). Raised for a decision in the PR.
- The "plus an alert" of REQ-TRU-002's failure behaviour is not built (no alert path exists; see DEBT-02).

# Session 2026-11-01 17:30 WAT · TRU-02

## Session summary
- What changed (PR links): `services/core/trust/decide/evaluate.go` (new), `gate.go` (T7 to T15), `evaluate_test.go` (new); trust README; this file.
- Why: TRU-02 (REQ-TRU-002, REQ-NFR-002, INV-16); the Architect's order of 1 Nov 2026 (the Trust parts of the G1 slice first; the runner is under every decision path, including US-01).
- Risks, and how they are contained: Evaluate trusts that each step's ports enforce their own deadlines; it checks the context only between steps. A step that blocks forever blocks the evaluation. The step tasks must pass a context with a deadline to every port.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-002 (order and trace; no read before a T4 to T6 REJECT, US-10; a panic, an error, a done context and a missing step at every step; the §23.3 outcome table per step; no decision after T15; a zero decision); INV-16, REQ-NFR-002 (a chaos property over 20,000 seeded vectors). Seen failing first: they did not compile against the old code. Mutation check: 6 mutants, 5 caught; the survivor rewrites a line that never runs.
- Regression cases added: none (no bug fixed).
- Debt entries: none.
- Conflicts raised: §23.3 T10 against REQ-RES-005 (above).

## Pre-work for the next agent
- Exact next step: TRU-05 (the Decision Record before grants, T15), then TRU-04 (T8 ownership), as the G1 slice needs.
- Open questions (owner): the T10 WAIT reading (Architect).
- Files to read first: `trust/decide/evaluate.go`, PERD v1.19 §23.3.
- Do not touch: `noema#94`, `noema#90`.

## Review finding, 3 Nov 2026 (Codex, blocking) and its fix
- Finding: `Evaluate` checked the context only before each step, so a step whose deadline passed while it ran could still return ALLOW at T15 and be accepted (INV-16).
- Verified: true; reproduced by `TestAContextThatExpiresWhileAStepRunsNeverYieldsItsResult` (failing first, commit `0b73159`).
- Fix: a context check right after the step returns, before its result is interpreted; a deadline is recorded at the step that was running. `TestADoneContextStopsBeforeTheNextStep` now expects the deadline at the step that cancelled the context (T(n-1)), not the next one: a stricter rule, not a weaker test.
