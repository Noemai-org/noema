# orchestrator

**What it does.** Investigation projection, transition table and commands.

**REQs.** REQ-STM-001.

**Status.** G1 slice (11 Nov 2026): `ports.go` and `conversation.go`, the read-only runner of US-01 (`Runner.RunRead`: RECEIVED, INTERPRETING, EVALUATING, EXPLAINING, COMPLETED; every move applied by the Machine and recorded before the run goes on). STM-01 slice 1 built (7 Nov 2026): the table reader (`table.go`) and the pure state machine (`machine.go`). Not built: the `investigation` projection table, the optimistic-lock store, the `INVESTIGATION_STATE_CHANGED` Record event, the guards that need Trust, Execution or Outcome inputs. The import rules and prohibitions are in `doc.go`.

**How it works.** `LoadTable()` reads the embedded `transitions.yaml`; `Machine.Apply(inv, cmd)` refuses a command made against another version (`ErrStaleVersion`), then an arrow that is not in the table (`*IllegalTransitionError`), then a refusing guard (`ErrGuardRefused`), and otherwise returns the investigation at `Version + 1` and the `Change` for the Record. A guard can refuse a listed transition and can never allow an unlisted one.

**Tests.** `go test ./orchestrator/...` from `services/core/`: the table equals the file, each unlisted pair is refused, a random-command property, malformed tables fail closed.

`transitions.yaml` is the state machine of PERD §28.2 as data (REQ-STM-001); `perd-28-2-state-machine.md` is the vendored PERD text it is tested against (`test_transitions.py`, run by `make contracts-validate`).
