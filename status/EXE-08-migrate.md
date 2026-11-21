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

# Session 2026-11-21 05:30 WAT · EXE-08 (Codex P2 on noema#846: the DSN file read)

## Session summary
- What changed (PR links): noema#846. The DSN file is opened ONCE with `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC` and every check is made on that open descriptor: a regular file, owned by the effective user, no group or world permission bit, at most 8 KiB; the read is bounded on the same descriptor (`io.LimitReader`). `os.Lstat` and `os.ReadFile` are gone from the command. `dsnfile_unix.go` holds the reader; `dsnfile_other.go` refuses off unix (the checks have no portable form).
- Why: Codex P2 at `6ac91051`: a check by path followed by a read by name leaves a gap in which the file can be swapped for a link, and a group- or world-readable file exposes the migration login's password (AGENT-13, INV-29).
- Rule chosen: mode must have no group or world bit (0400 or 0600), owner must be the effective user. The Compose Trust DSN files are already written 0400 owned by the process user (`trust-dsn-init`, A356), so the same convention meets this rule. Question for Codex, SR and IL: the Compose one-shot must write the migration DSN file the same way (0400, owned by the user the one-shot runs as); a Docker secret mounted 0444 and root-owned would be refused by design.
- Risks, and how they are contained: a deployment that mounts the DSN 0444 now fails closed with the fixed "cannot be used" sentence (exit 2), before any connection.
- Tests added (REQ-DATA-001; AGENT-13, INV-29): `cmd/migrate/dsnfile_test.go`: group/world bits refused (eleven modes) and 0400/0600 accepted; a link (to a good file, and a dangling one) never followed; a named pipe refused without blocking; a file owned by another user refused (needs root, skipped where the test cannot chown); a source test that the path is opened once on the descriptor (no `os.Lstat`, no `os.ReadFile`; `O_NOFOLLOW`, `O_NONBLOCK`, `.Stat()`, `Geteuid`, `LimitReader` present).
- Not provable here: the swap race itself (no deterministic test of a swap between check and open exists; the source test and the no-follow tests are the guard), and "the bounded read, not the stat, caps the size" (a file's reported size is its size).
- Regression cases added: the tests above.
- Debt entries: none. Other DSN readers with the same pattern, NOT changed here (outside the allowlist): `services/core/cmd/policypublish/main.go` (Lstat then read). Proposed follow-up: the same descriptor-based reader for it, and a shared helper if a second command needs it.
- Conflicts raised: none. Main is red for another lane's reason (below, in the PR).

## Pre-work for the next agent
- Exact next step: IL writes the Compose one-shot so the migration DSN file is 0400 and owned by the one-shot's user.
- Open questions (Codex, SR, IL): the 0400/0600 rule above; a follow-up for `cmd/policypublish`.
- Files to read first: `services/core/cmd/migrate/dsnfile_unix.go`.
- Do not touch: merged migrations.
