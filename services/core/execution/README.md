# execution

**What it does.** Planner, capability registry, execution controller, recovery.

**REQs.** REQ-EXE-002, REQ-EXE-003, REQ-CAP-002.

**Status.** Empty boundary package (PLT-01). PLT-01 creates the boundary; filled by EXE-01 to EXE-06 and PLN-01. The import rules and prohibitions are in `doc.go`.

**Tests.** `go test ./execution/...` from `services/core/` (none yet).
