
## Session 2026-11-20 · EXE-06 (C1, merge of main into noema#797)

- Main added `CONFIRMATION_REFUSED` to the same lists (noema#784). The merge keeps all values in one order: the seven earlier kinds, `CONFIRMATION_REFUSED`, `KILL_SWITCH_CLEARED`, `KILL_SWITCH_CLEAR_REFUSED`. Generated files were taken from main and regenerated with `tools/codegen/generate.py --write` and `tools/record_chain_ref_v3.py --write` (both `--check` pass); both fixtures sets are kept.
- Two exact pins of #784 changed to the new exact list, nothing deleted or skipped: `tools/contracts/test_record_event_v3.py` (`test_security_kinds_are_those_of_v2_and_the_three_added_in_place`) and `record/chain/types_test.go` (`TestTheSecurityKindsAreTheSevenConfirmationRefusedAndTheKillSwitchPair`).
- Verification: `python3 -m unittest tools.contracts.test_record_event_v3 tools.contracts.test_security_kinds_v3` ok (42 tests); `go test ./record/chain/` ok (real PostgreSQL, 38 s); `python3 tools/lint/run.py`: lint OK.
