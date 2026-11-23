# cmd/tenantinit

**What it does.** The one-shot that registers the public keys of the `core-trust` signing key, once, before the stack serves, in one transaction. It creates **no tenant row**: ruling A489 (21 Nov 2026, interim record `DEC-2026-11-21-architect-rulings-a489-a492`) amends A484 ("C1 builds cmd/tenantinit, which registers the core-trust public key and the tenant rows") so that tenantinit registers the core-trust public key only, and tenant rows keep one creator, `deploy/compose/postgres/create-tenants.sh` (A353; the guard `deploy/compose/postgres/test_provision_env.sh` fails if any other file creates one). It reads two files and three flags, writes nothing else, and prints one line of counts.

**REQs.** REQ-SEC-005, REQ-AUD-006; INV-12; AGENT-13; A347 (fixed public error texts). Task TRU-02. Written 20 Nov 2026; amended 21 Nov 2026 (A489).

**The interface is `[PROPOSED, AGENT-02]`.** A484 names the command and what it registers; it gives no flags. The smallest safe default below mirrors `cmd/policypublish` (files for secrets, a fixed sentence per refusal). IL codes Compose against it; the Architect confirms or changes it (open questions: `status/TRU-02-tenantinit.md`).

**Usage.**

```
tenantinit --dsn-file /run/secrets/operator_dsn --key-dir /run/secrets/core-trust-keys --key-max-active 1
```

| Flag | Meaning |
|---|---|
| `--dsn-file` | a regular file (not a link, at most 8 KiB, owned by the caller, no group or other permission bit; opened once without following a link, and checked and read on that one descriptor, so replacing the path afterwards changes nothing) holding the DSN of a login that may call `key_register` (see "Which login"). The DSN is never an argument and never printed |
| `--key-dir` | the directory of the `core-trust` signing keys: `core-trust.ed25519.<version>.key`, mode 0400, owned by the user that runs the command. Read with `execution/signing.LoadRing`; only the public half is derived |
| `--key-max-active` | how many key files may be in the directory at once (1 to 16), the rotation overlap. No default. The database separately refuses a third ACTIVE key (migration 0013) |

Each flag is given once and none may be empty. An unknown flag (`--tenant` and an identity flag such as `--published-by` included), a positional argument, `--help` and every bad value are a usage refusal (exit 2) before any file is read or connection opened. The identity of the keys is the constant `core-trust`: no flag sets it.

**Output.** On success, one line on stdout, always these two members in this order, counts only (never a key or a path):

```
{"keys_registered":1,"keys_existing":0}
```

When the exit code is not 0, nothing on stdout and one fixed sentence on stderr (`tenantinit: ...`), with a `(SQLSTATE nnnnn)` suffix only for a database that cannot be reached or an error with no fixed sentence of its own. An error keeps its cause for `errors.Is` and never prints it: not a DSN, a path, a key, a driver's text.

| Exit | Meaning |
|---|---|
| 0 | done, or already done (every key was as wanted: nothing written) |
| 1 | refused, nothing changed: a key version registered with a different public key, or registered but no longer ACTIVE; a key the database refuses (not newer than the registered ones, or a third ACTIVE key); a login the database refuses (not one that may call `key_register`) |
| 2 | usage |
| 3 | could not run: the DSN file cannot be used, the key directory or a key file is refused by the loader (mode, name, link, owner, content, more files than `--key-max-active`, none), the database cannot be reached, any other database error |

**Order and atomicity.** (1) The arguments. (2) The key files, with the loader: a refused file stops the run before any connection. (3) One connection, one transaction: the keys in ascending version order; a refusal rolls the whole transaction back, so a run that refuses changes nothing. (4) Commit.

**Idempotent.** A key version already registered with the same public key and ACTIVE counts as `keys_existing`. `key_register` derives the key ID (`core-trust.ed25519.<version>`); the command never supplies it.

**Which login.** `key_register` is executable by its owner and the superuser only (migration 0013), and no runtime role can call it. The command does not check the role itself: the database refuses any other login, and the command reports it (exit 1, fixed text, nothing changed). A runtime login (`trust_rw__<tenant>`, `execution_rw__<tenant>`) is refused the same way; `cmd/tenantinit` tests prove it. Which of the two logins the Compose one-shot uses is IL's choice with the secret handling of OQ-09.

**What it does not do.** It creates no tenant row (`create-tenants.sh` does, A353, A489) and never reads, changes or removes one: a kill switch and a time zone an operator has set are untouched. It creates no login and no `tenant_login` mapping (`provision-tenants.sh`), applies no migration (`cmd/migrate`, EXE-08, C2), registers no key of another identity, retires or revokes no key. It adds no Record event.

**Tests.** `go test ./cmd/tenantinit/...` from `services/core/`, against a real PostgreSQL (`internal/testpg`). Every key and password in them is SYNTHETIC.
