# record

**What it does.** The append-only hash-chained Record, reconstruction, operational memory, chain verification.

**REQs.** REQ-AUD-001, REQ-AUD-003, REQ-AUD-005.

**Status.** `chain/` (REC-01: append and verify) and `reconstruct/` (REC-03: the reconstruction endpoint, not yet wired into `cmd/core`) are built; `memory/` and `verify/` are empty boundaries (REC-04 to REC-06). The import rules and prohibitions are in each `doc.go`.

**Tests.** `go test ./record/...` from `services/core/`. They need PostgreSQL server binaries or `NOEMA_TEST_DATABASE_URL`.
