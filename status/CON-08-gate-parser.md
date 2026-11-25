# Status · CON-08 (Core Engineer 1) · the gate reads the supersedes link with the one intent-reference parser

Updated 2026-11-24. Plan task CON-08: owner C1; REQ-API-001; ruling A337 (interim, relayed); Needs CON-03 (Done); done when "a shared parser with a test ... the Trust adapter and the Record's check use it and no second parser exists".

## State: draft pull request noema#942. CON-08 is not Done until merged evidence.

## Session 2026-11-24 · CON-08

### Session summary
- What changed (PR links): noema#942. `services/core/understanding/gate.go` reads `supersedes` with `contracts.ParseIntentRef` and its own `intentRefRe` is gone; tests in `understanding/gate_test.go` and `internal/contracts/intentref_test.go`.
- Why: `contracts.ParseIntentRef` (CON-08, A319) was already the parser of the Trust intake, the Trust port, the evidence adapter and the Interpreter port, and `record/payloadguard` agrees with it (test `TestTheRecordsTypedIDCheckExemptsEveryReferenceTheParserAcceptsAndNothingItRefuses`). One second reader was left: the second schema gate's `intentRefRe` (`@([0-9]+)`), which admitted `@01` and `@007`.
- Risks, and how they are contained: a model that wrote a leading-zero or ten-digit previous version is now refused with `SUPERSEDES` (before: admitted). The contract's own pattern already refuses those; nothing the contract holds is refused.
- Tests added (REQ-API-001, REQ-INT-001): three gate cases (`@01`, `@007`, a previous version above the form's bound) and `TestNoSecondParserOfTheIntentReferenceExists` (scans production Go under `services/core` for a pattern that reads the whole form).
- Regression cases added: the three gate cases. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for CON-08's done-when once #942 merges; the Steward marks it.
- Do not touch: `internal/contracts/intentref.go` without its tests (A319, A337).
