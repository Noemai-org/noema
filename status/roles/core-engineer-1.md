# Role status · Core Engineer 1 (Execution, Record, Recovery, Temporal)

Only Core Engineer 1 writes this file. Dated entries are appended; history is never rewritten.

## 2026-11-01 15:45 WAT

**Completed.** None. Nothing is Done until a founder approves and it merges.

**In review (draft PR).** REC-03, the reconstruction endpoint (`status/REC-03.md`).
- Local checks pass on PostgreSQL 17.11 (the image Compose pins) and on 16: `go test -race ./...`, `gofmt`, `go vet`, the architecture lint and the contract suite.
- CI links follow at the final head.

**Blocked.**
- CON-03, since 1 Nov, on the founders. They have not answered which generator (A or B), whether CON-03 may change `.github/workflows/safeguards.yml` and the Makefile, or whether migrating existing hand-written types belongs in CON-03.

**Decisions I took** (recorded in the REC-03 PR):
- ACTION items come from `RECEIPT` events; `DISPATCH_ATTEMPTED` appears in the timeline only;
- the chain head is the correlation's last event;
- a break anywhere in the tenant's chain refuses every correlation of that tenant;
- only the audit role is served (the narrower reading);
- the envelope `schema_version` is "1.0", as in the approved fixtures.

**Findings.**
1. **CI does not test on the Compose Postgres major.** `core.yml` runs `go test ./...` on `ubuntu-24.04` with no database service, so `internal/testpg` uses the runner image's own PostgreSQL. That is 16, not 17.11. CI also runs without `-race`.
2. **The ledger's `RECEIPT` payloads break their contract.** They carry `logical_action: "ESCALATE_CASE"`, which is not a pack ID as `execution-receipt.v1` requires. EXE-02 is in my area; the fix needs its own task card. A regression case is in REC-03.
3. **REC-01 is not Done.** Its property test does not exist yet; `rapid` is now approved for it.

**Escalation: CI Postgres major.**
- *Problem:* Record tests in CI run on 16 while the stack runs 17.11 (`deploy/compose/docker-compose.yml:58`, `.github/workflows/core.yml:33`).
- *Option A (recommended):* give the `core` job a `postgres:17.11-alpine` service container, pinned by digest, and set `NOEMA_TEST_DATABASE_URL`. Add `-race`. Small effort; one CI file; matches Compose exactly.
- *Option B:* install `postgresql-17` from apt on the runner. A different build from Compose's Alpine image, and slower.
- *Option C:* do nothing. Version-specific behaviour (privileges, `jsonb`) stays untested in CI.
- *Question:* do you approve option A, and may I change `.github/workflows/core.yml`? It is a protected path, owned by both founders.

**Next.** On approval, REC-01's property test with `rapid`. I'll pin v1.2.0 (February 2025) rather than v1.3.0, checking the licence and recording it under AGENT-14 in `docs`. Then REC-04 and REC-05. CON-03 resumes as soon as the founders answer.
