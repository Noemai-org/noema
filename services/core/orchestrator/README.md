# orchestrator

**What it does.** Investigation projection, transition table and commands.

**REQs.** REQ-STM-001.

**Status.** Empty boundary package (PLT-01). PLT-01 creates the boundary; filled by REC-03 and STM-01. The import rules and prohibitions are in `doc.go`.

**Tests.** `go test ./orchestrator/...` from `services/core/` (none yet).

`transitions.yaml` is the state machine of PERD §28.2 as data (REQ-STM-001); `perd-28-2-state-machine.md` is the vendored PERD text it is tested against (`test_transitions.py`, run by `make contracts-validate`).
