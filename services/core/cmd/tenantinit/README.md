# cmd/tenantinit

**What it does.** The one-shot that makes a fresh database ready for the core: it creates the `tenant` rows and registers the public keys of the `core-trust` signing key, in one transaction. Ruling A484 (20 Nov 2026, interim record `DEC-2026-11-20-architect-rulings-a469-a485`): "C1 builds cmd/tenantinit, which registers the core-trust public key and the tenant rows. IL writes the Compose one-shots against those names." It reads two files and a few flags, writes nothing else, and prints one line of counts.

**REQs.** REQ-SEC-004, REQ-SEC-005, REQ-AUD-006; INV-11, INV-12; AGENT-13; A347 (fixed public error texts). Task TRU-02. Written 20 Nov 2026.

**The interface is `[PROPOSED, AGENT-02]`.** A484 names the command and what it registers; it gives no flags. The smallest safe default below mirrors `cmd/policypublish` (files for secrets, a fixed sentence per refusal). IL codes Compose against it; the Architect confirms or changes it (open questions: `status/TRU-02-tenantinit.md`).

**Usage.**

```
tenantinit --dsn-file /run/secrets/operator_dsn --key-dir /run/secrets/core-trust-keys --key-max-active 1 \
  --tenant ten_aa="Synthetic Bank A" --tenant ten_bb="Synthetic Bank B"
```

| Flag | Meaning |
|---|---|
| `--dsn-file` | a regular file (not a link, at most 8 KiB) holding the DSN of the **operator's superuser** login (see "Which login"). The DSN is never an argument and never printed |
| `--key-dir` | the directory of the `core-trust` signing keys: `core-trust.ed25519.<version>.key`, mode 0400, owned by the user that runs the command. Read with `execution/signing.LoadRing`; only the public half is derived |
| `--key-max-active` | how many key files may be in the directory at once (1 to 16), the rotation overlap. No default. The database separately refuses a third ACTIVE key (migration 0013) |
| `--tenant` | `ten_ID=NAME`, repeated, at least 1 and at most 16. The ID is `^ten_[0-9A-Za-z]{2,32}$` (`chain.ValidTenantID`); it ends at the first `=`. The name is 1 to 64 printable characters (valid UTF-8, no control character or line break, no space at either end). An ID is given once |

Each flag is given once and none may be empty. An unknown flag (an identity flag such as `--published-by` included), a positional argument, `--help` and every bad value are a usage refusal (exit 2) before any file is read or connection opened. The identity of the keys is the constant `core-trust`: no flag sets it.

**Output.** On success, one line on stdout, always these four members in this order, counts only (never a name, a key or a path):

```
{"tenants_created":2,"tenants_existing":0,"keys_registered":1,"keys_existing":0}
```

When the exit code is not 0, nothing on stdout and one fixed sentence on stderr (`tenantinit: ...`), with a `(SQLSTATE nnnnn)` suffix only for a database that cannot be reached or an error with no fixed sentence of its own. An error keeps its cause for `errors.Is` and never prints it: not a DSN, a path, a name, a key, a driver's text.

| Exit | Meaning |
|---|---|
| 0 | done, or already done (every row was as wanted: nothing written) |
| 1 | refused, nothing changed: a tenant ID that exists under another name; a key version registered with a different public key, or registered but no longer ACTIVE; a key the database refuses (not newer than the registered ones, or a third ACTIVE key); a login the database refuses (not the operator's superuser) |
| 2 | usage |
| 3 | could not run: the DSN file cannot be used, the key directory or a key file is refused by the loader (mode, name, link, owner, content, more files than `--key-max-active`, none), the database cannot be reached, any other database error |

**Order and atomicity.** (1) The arguments. (2) The key files, with the loader: a refused file stops the run before any connection. (3) One connection, one transaction: every tenant, then the keys in ascending version order; a refusal rolls the whole transaction back, so a run that refuses changes nothing (not even the tenants before the refused one). (4) Commit.

**Idempotent.** A tenant row that exists with the same name counts as `tenants_existing`; it is never rewritten, so a kill switch an operator has set and a time zone are untouched (the insert is `ON CONFLICT DO NOTHING`; the table's trigger of migration 0017 also stops a clear by anyone but a superuser). A key version already registered with the same public key and ACTIVE counts as `keys_existing`. `key_register` derives the key ID (`core-trust.ed25519.<version>`); the command never supplies it. A new tenant starts with its kill switch off and time zone `UTC` (the column defaults).

**Which login.** The task text said "the migrator role". The database says otherwise for tenants: `tenant` forces row-level security with no INSERT policy (migration 0030, ruling on noema#716, `create-tenants.sh`), so the non-superuser migration login cannot insert a tenant row (`TestOnlyASuperuserCreatesATenant`); `key_register` is executable by its owner and the superuser only. The one login that can do both steps is the operator's superuser. The command does not check the role itself: the database refuses any other login, and the command reports it (exit 1, fixed text, nothing changed). A runtime login (`trust_rw__<tenant>`, `execution_rw__<tenant>`) is refused the same way; `cmd/tenantinit` tests prove it.

**What it does not do.** It creates no login and no `tenant_login` mapping (`provision-tenants.sh`), applies no migration (`cmd/migrate`, EXE-08, C2), registers no key of another identity, retires or revokes no key, and never changes a tenant's name, time zone or kill switch. It adds no Record event.

**Tests.** `go test ./cmd/tenantinit/...` from `services/core/`, against a real PostgreSQL (`internal/testpg`). Every tenant, key and password in them is SYNTHETIC.
