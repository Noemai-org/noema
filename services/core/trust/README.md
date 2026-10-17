# trust

**What it does.** The Trust Engine: reconciliation at decision time, authority, policy, clocks, decisions, grants, confirmations.

**REQs.** REQ-TRU-001, REQ-TRU-008, REQ-TRU-009.

**Status.** Empty boundary package (PLT-01). PLT-01 creates the boundary; filled by TRU-01 to TRU-12. The import rules and prohibitions are in `doc.go`.

**Tests.** `go test ./trust/...` from `services/core/` (none yet).
