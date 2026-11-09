# Status · STM-01 (Core Engineer 1) · investigation state machine

Updated 2026-11-07. Plan task STM-01: owner C1+AG; REQ-STM-001; needs REC-01; done when "property test (random command sequences) finds no illegal state; each prohibited transition has a negative test".

## State: slice 1 of 2 in review (draft until CI is green at the final head and the reviewer line is given). STM-01 is not Done.

## Session 2026-11-07 17:00 WAT · STM-01

### Session summary
- What changed (PR links): the STM-01 pull request on `feat/STM-01-machine`. `orchestrator/table.go` reads the embedded `transitions.yaml` (restricted YAML, standard library only, fails closed on any line it does not understand); `orchestrator/machine.go` is the pure `Apply(investigation, command)`: version first (optimistic lock), then the table, then the guard; it returns the new investigation and the `Change` that `INVESTIGATION_STATE_CHANGED` will carry.
- Why: REQ-STM-001 (every transition explicit, SoT 45.1); the property and negative tests are its acceptance criteria.
- Risks, and how they are contained: a guard can refuse a listed transition and can never allow an unlisted one (the guard is not consulted for an unlisted arrow); an unknown state is never a source or a target; a table the reader does not fully understand is an error.
- Tests added (REQ-STM-001, SoT 45.1): `orchestrator/machine_test.go` (the Go table equals the file by an independent reader; every listed arrow applies with version + 1; every one of the unlisted pairs, 21 x 21 less the arrows, is refused with the typed error and changes nothing; stale versions; guards; a rapid property over random command sequences with right and wrong versions and junk states), `orchestrator/table_internal_test.go` (13 malformed tables).
- Regression cases added: none (no defect).
- Debt entries: none.
- Conflicts raised: none. `EXPLAINING_BLOCKED` has no exit in the table (OD-P-60); it is a listed target and, as the file says, the machine does not invent an exit.

## Pre-work for the next agent
- Exact next step (slice 2): the `investigation` projection table (migration, `tenant_id`, `state_version`, RLS and FORCE as in 0015), the store that does the compare-and-swap on `(investigation_id, state_version)` in the same transaction as the `INVESTIGATION_STATE_CHANGED` Record event (appended through `chain.Appender`, emitter `core-workflows`), and the alert plus S1 triage on `IllegalTransitionError`. Check PERD section 38 for the table before writing it; take the migration number from the noema#183 ledger (A34).
- Open questions (owner): the counted guards ("at most 2 times" on REJECTED_AT_GATE and BLOCKED_UNKNOWN) are policy numbers and come from `platform/config` when it has them, not from literals (Architect); OD-P-60 (EXPLAINING_BLOCKED exit).
- Files to read first: `services/core/orchestrator/README.md`, `transitions.yaml`, PERD v1.20 section 28.
- Do not touch: `transitions.yaml` without the PERD text and `test_transitions.py` (the vendored PERD text is the check).
