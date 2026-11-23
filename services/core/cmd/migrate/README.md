# cmd/migrate

**What it does.** Applies the database migrations once and exits: the one-shot that brings a fresh database to the migrated state (A484). It builds its connection in process from a host, a port, a database, a login and a password file (A521), opens one pool, and calls `platform/db.Migrate` on the `.sql` files of a directory (`os.DirFS`). `Migrate` applies the files in file-name order, each in its own transaction with its row in `schema_migration`, under one advisory lock. It is forward-only: a file that changed after it was applied, or an applied file that is gone, is refused and nothing runs after it. A second run applies nothing. There is no shell, no entrypoint script and no image of its own.

**REQs.** REQ-DATA-001 (schema ownership and migrations), REQ-SEC-001; AGENT-13 (no secret in a log or an error), INV-12, INV-29, INV-30. Task EXE-08 (A484, A518, A521, A527, A528). Updated 23 Nov 2026.

**Usage.**

```
migrate --host postgres --port 5432 --database noema --user migrator --password-file /run/secrets/migrator_password --migrations-dir /migrations
```

| Flag | Meaning |
|---|---|
| `--host` | a DNS name or an IP address (a socket path, a space, an at sign or a colon that is not an address is refused). No default |
| `--port` | 1 to 65535. No default |
| `--database`, `--user` | the database and the migration login: letters, digits, `_`, `.`, `-`, at most 63 characters, not starting with `.` or `-` (so no space, quote or equals sign can reach the driver). No default |
| `--password-file` | the file holding the login's password, **read through the shared key-file reader** (`trust/grants/keyfiles.ReadFile`, A518, A528): opened once without following a link and without waiting on a special file, a regular file **owned by the user the command runs as, mode exactly 0400** (0600 is refused), at most 1 KiB, read from the same descriptor. One final line break is dropped; an empty or blank password, and one with a line break or a NUL inside, are refused. The Compose stack writes it 0400, owner 65532 (A509). No default |
| `--sslmode` | `disable`, `prefer` or `require`. Default `prefer`, the driver's own and the one every other login of the stack uses (their DSNs name none). `verify-full` is not offered: it needs a root certificate this command does not take |
| `--migrations-dir` | a directory holding at least one `NNNN_name.sql` file. A path that is not a directory, or a directory with no `.sql` file, is refused, so a wrong mount cannot look like a database that is up to date. No default |

**The password (A527).** It comes only from the checked file. There is no `--password`, `--dsn` or `--dsn-file` flag. The command reads no environment variable, and it removes every `PG*` variable the driver would read from its own process before it builds the configuration, so nothing in the environment can supply a password, name a host or a login, or change a mode (a test sets them all wrong). The configuration is built in process: the non-secret parts are parsed from a keyword string, and the password is set on the configuration directly. No string that holds it is ever built, so it is never escaped, parsed or printed. The one-shot is its only holder.

**Output.** One JSON line on stdout with the PERD 44.2 fields (`ts`, `level`, `service`, `power`, `event`, `correlation_id`, `msg`) and `result` (`migrated`, `up_to_date`, `refused`, `error`), `reason`, `applied_count`, `applied` (the file names applied by this run) and `sqlstate` when there is one. When the exit code is not 0, one fixed sentence on stderr. Never the password, a login, a host, a port, a database, a SQL text or a driver's text.

| Exit | Meaning |
|---|---|
| 0 | migrated, or already up to date |
| 1 | `migration_failed`: a statement the database refused, a changed file, an applied file that is gone. The files before it stay applied and are named in `applied` |
| 2 | could not run: a bad command line (usage on stderr), `password_file_unusable`, `connection_unusable`, `migrations_dir_unusable`, `database_unreachable` |

**Limits.** The whole run has a 5-minute deadline (an operating limit, not a safety threshold). The command creates no role and no database: the login must already be the migrating role (`deploy/compose/postgres/init.sh`). Compose wiring is IL's (A484).

**Tests.** `go test ./cmd/migrate` (needs the PostgreSQL of `internal/testpg`; CI runs it in the core `tests` job). All data is SYNTHETIC.
