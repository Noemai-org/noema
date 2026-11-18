# Status · STM-01-decide-decision-id · Trust adapter `Decide` returns the decision id

Updated 2026-11-20. Owner: Engineering Lead. REQ-TRU-002, REQ-NFR-002; INV-03, INV-21. Ruling A456 (interim, relayed, recorded in docs#213, not yet merged): Decide returns the decision id for every recorded decision; grants exist only for ALLOW.

## State

- Red commit: tests only (`port_test.go`). Green: local, pushed by EL.
- Scope: `orchestrator/portadapters/trust/port.go` (`Decide`) and its tests. Not touched: `trust/intake` (noema#755), `RecorderAdapter` code.

## Session 2026-11-20 · STM-01-decide-decision-id

## Session summary
- What changed (PR links): see the PR of branch feat/STM-01-decide-decision-id.
- Why: A456. Decide returned an id only for ALLOW, so a recorded CONFIRM or REJECT lost its decision id; a grant on a non-ALLOW was passed through silently.
- Risks, and how they are contained: a grant on a non-ALLOW is now ErrBadAnswer (fail closed, INV-03); an unrecorded outcome still returns no id and the adapter invents none.
- Tests added (REQ-TRU-002, REQ-NFR-002; INV-03, INV-21): `TestDecideReturnsTheDecisionIDOfEveryRecordedDecision`, `TestDecideRefusesAGrantOnAnythingButAnAllow`; `TestDecideMapsEveryOutcomeAndNeverInventsOne` kept for the unrecorded case.
- Regression cases added: none beyond the tests. Debt entries: none.
- Conflicts raised: none. See Open question 1.

## Pre-work for the next agent
- Exact next step: after noema#755 merges, check the recorded REJECT of a declined answer reaches Decide with its id.
- Open questions (EL): (1) `RecorderAdapter.request` (recorder.go) refuses an empty PlanHash for every outcome, so a recorded non-ALLOW with no steps and no plan hash (A456 Recorder text) is not yet accepted by the adapter; a separate change in this package, not made here (outside the three listed items). (2) The Recorder port doc lives in `trust/intake/ports_t13_t15.go` (not edited); suggested replacement for the line above `type Recorder interface`: "Recorder answers T13 and T15 for every outcome: a non-ALLOW decision is recorded with no steps and no plan hash, an ALLOW requires both, and a REJECT, CONFIRM or UNKNOWN never carries a grant."
- Files to read first: `services/core/orchestrator/portadapters/trust/port.go`.
- Do not touch: `trust/intake` while noema#755 is open.
