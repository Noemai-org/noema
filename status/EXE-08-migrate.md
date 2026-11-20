# EXE-08 · cmd/migrate

State: PR noema#846 (draft until its CI is green). Tier 1-adjacent: Codex, SR, one Founder.

# Session 2026-11-20 14:30 WAT · EXE-08 (cmd/migrate, A484)

## Session summary
- What changed (PR links): noema#846. New `services/core/cmd/migrate`: `--dsn-file` and `--migrations-dir` (both required, no default), one pool, `db.Migrate(ctx, pool, os.DirFS(dir))`, one JSON line, exit 0/1/2.
- Why: A484 (Architect, 20 Nov 2026): C2 builds cmd/migrate under EXE-08; IL writes the Compose one-shots against those names.
- Risks, and how they are contained: the command holds the migration login's DSN. It comes only from a mounted file (a regular file, not a link, at most 8 KiB); no error or line carries the DSN, a login, a host or a driver's text (tested with a refused login). A wrong mount is refused (no `.sql` file), so it cannot look like an up-to-date database.
- Tests added (REQ, INV, US or EC IDs): `cmd/migrate/migrate_test.go` (REQ-DATA-001; AGENT-13; INV-30).
- Regression cases added: none (new behaviour).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL wires the Compose one-shot (`migrate --dsn-file ... --migrations-dir ...`) and C1's `cmd/tenantinit` runs after it.
- Open questions (IL): which secret file holds the migrating role's DSN in Compose.
- Files to read first: `services/core/cmd/migrate/main.go`, `services/core/platform/db/db.go` (Migrate).
- Do not touch: merged migrations.
