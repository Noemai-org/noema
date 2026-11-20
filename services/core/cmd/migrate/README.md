# cmd/migrate

**What it does.** Applies the database migrations once and exits: the one-shot that brings a fresh database to the migrated state (A484). It reads the DSN of the migration login from a file, opens one pool, and calls `platform/db.Migrate` on the `.sql` files of a directory (`os.DirFS`). `Migrate` applies the files in file-name order, each in its own transaction with its row in `schema_migration`, under one advisory lock. It is forward-only: a file that changed after it was applied, or an applied file that is gone, is refused and nothing runs after it. A second run applies nothing.

**REQs.** REQ-DATA-001 (schema ownership and migrations); AGENT-13 (no secret in a log or an error), INV-30. Task EXE-08 (A484). Updated 20 Nov 2026.

**Usage.**

```
migrate --dsn-file /run/secrets/migrator_dsn --migrations-dir /migrations
```

| Flag | Meaning |
|---|---|
| `--dsn-file` | a regular file (not a link, not a directory), not empty, at most 8 KiB, holding the DSN of the migration login. The DSN is never an argument, never read from the environment and never printed. No default |
| `--migrations-dir` | a directory holding at least one `NNNN_name.sql` file. A path that is not a directory, or a directory with no `.sql` file, is refused, so a wrong mount cannot look like a database that is up to date. No default |

**Output.** One JSON line on stdout with the PERD 44.2 fields (`ts`, `level`, `service`, `power`, `event`, `correlation_id`, `msg`) and `result` (`migrated`, `up_to_date`, `refused`, `error`), `reason`, `applied_count`, `applied` (the file names applied by this run) and `sqlstate` when there is one. When the exit code is not 0, one fixed sentence on stderr. Never a DSN, a login, a host, a SQL text or a driver's text.

| Exit | Meaning |
|---|---|
| 0 | migrated, or already up to date |
| 1 | `migration_failed`: a statement the database refused, a changed file, an applied file that is gone. The files before it stay applied and are named in `applied` |
| 2 | could not run: a bad command line (usage on stderr), `dsn_file_unusable`, `dsn_unusable`, `migrations_dir_unusable`, `database_unreachable` |

**Limits.** The whole run has a 5-minute deadline (an operating limit, not a safety threshold). The command creates no role and no database: the login in the DSN must already be the migrating role (`deploy/compose/postgres/init.sh`). Compose wiring is IL's (A484).

**Tests.** `go test ./cmd/migrate` (needs the PostgreSQL of `internal/testpg`; CI runs it in the core `tests` job). All data is SYNTHETIC.
