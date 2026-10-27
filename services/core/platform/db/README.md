# platform/db

**What it does.** Postgres access with per-power roles: `Open`, `OpenAsRole` (every connection runs `SET ROLE <power>`, so the database decides what a power may write) and `Migrate` (forward-only, checksummed, one advisory lock).

**REQs.** REQ-DATA-001, REQ-DATA-002.

**Status.** Built for the first end-to-end case (task PLT-06, slice). The tables are in `services/core/migrations/README.md`.

**Tests.** `go test ./platform/db/...` from `services/core/`: migration mechanics (applies once, refuses an edited, removed or misnamed file, rolls a failed file back), the role-permission matrix, and the immutability triggers. They need PostgreSQL server binaries or `NOEMA_TEST_DATABASE_URL`.
