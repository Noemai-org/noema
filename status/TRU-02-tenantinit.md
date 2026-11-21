# Status · TRU-02 · `cmd/tenantinit`, the one-shot that creates the tenant rows and registers the core-trust public keys

Updated 2026-11-20. Owner: Core Engineer 1 (helper session). Ruling A484 (interim record `DEC-2026-11-20-architect-rulings-a469-a485`, on `docs` `origin/main`): "C1 builds cmd/tenantinit, which registers the core-trust public key and the tenant rows. IL writes the Compose one-shots against those names." A484 gives no flags and no behaviour beyond that sentence. Everything below that goes beyond it is the smallest safe default, written down so the Architect can confirm or correct it. Nothing is pushed; the work is four local commits on `g1/tinit`.

## Session 2026-11-20 (C1 helper)

### Session summary
- What changed (PR links): none yet (local commits only). New: `services/core/cmd/tenantinit/` (`main.go`, `args.go`, `apply.go`, `README.md`, `helpers_test.go`, `args_test.go`, `tenants_test.go`, `keys_test.go`, `secrets_test.go`) and this file. No migration, contract, dependency (`go.mod` and `go.sum` unchanged), deploy script, workflow or CODEOWNERS line is touched.
- Why: a database with the migrations applied has no tenant row and no registered `core-trust` public key, so no Record event can be appended and no login can be mapped. `create-tenants.sh` makes the tenant rows by hand today; nothing registers a key (`key_register` is called only by tests). A484 asks for one command that does both, which the Compose one-shots can call.
- Risks, and how they are contained: the command writes only through the database's own rules (`INSERT INTO tenant` under row-level security, `key_register` for keys), in one transaction, and the database is the authority: a login that may not do this is refused by the database. It reads private key files only through `execution/signing.LoadRing` and handles only the derived public bytes. No error text is printed; each failure is a fixed sentence (A347).
- Tests added (REQ, INV, US or EC IDs): REQ-SEC-004, REQ-SEC-005, REQ-AUD-006, INV-11, INV-12, AGENT-13, A347. Real PostgreSQL through `internal/testpg`, the real migrations, the real runtime logins (`trust_rw`, `execution_rw`). Tenant created once and twice; a different name refused and unchanged; kill switch and time zone of an existing tenant untouched; key registered once and twice (one ACTIVE row); the stored key verifies a signature of the file's key (standard library); a different public key under a registered version refused with the table unchanged; a retired key refused; a rotation of two keys; a third ACTIVE key refused with everything rolled back; 14 key-file faults refused before any connection (modes, names, link, directory, pipe, content, overlap); empty and missing key directories; a runtime login refused; 47 bad command lines refused before any connection; no secret in any output; an unusable or unreachable DSN reported in fixed words.
- Regression cases added: the whole `cmd/tenantinit` suite is new. The RED commit has the tests against a scaffold that exits 0 and does nothing; every test fails on an assertion, none on a build error.
- Debt entries: none added.
- Conflicts raised: one reading to confirm, not a stop: "the migrator role" against migration 0030 (see question 1). Applied the safer reading.

### Interface `[PROPOSED, AGENT-02]`

```
tenantinit --dsn-file FILE --key-dir DIR --key-max-active N --tenant ten_ID=NAME [--tenant ten_ID=NAME ...]
```

Exit 0 done or already done; 1 refused (nothing changed); 2 usage; 3 could not run. On success one stdout line `{"tenants_created":N,"tenants_existing":N,"keys_registered":N,"keys_existing":N}`; otherwise nothing on stdout and one fixed sentence on stderr. Tenant ID `^ten_[0-9A-Za-z]{2,32}$`, name 1 to 64 printable characters, 1 to 16 tenants, each flag once, none empty, no identity flag. The key identity is the constant `core-trust`. Full table in `services/core/cmd/tenantinit/README.md`. For IL: the one-shot passes a DSN file of the operator's superuser (question 1), a key directory holding `core-trust.ed25519.<version>.key` (0400, owned by the running user), the overlap, and one `--tenant` per entry of `NOEMA_TENANTS` with its name.

### What was observed
- From `services/core`: `gofmt -l .` empty, `go build ./...`, `go vet ./cmd/... ./execution/...`, `go test -count=1 -race ./cmd/tenantinit/... ./execution/signing/...` all `ok`; from the repo root `python3 tools/lint/run.py` prints `lint OK`. CI run links: none (nothing is pushed).
- Hand mutants, each killed by the tests, source restored with `git checkout` after each: a different public key accepted; the tenant name overwritten on conflict; the kill switch reset on conflict; the cause text printed after the sentence; the key-mode check of `execution/signing` bypassed; the second run erroring (no `ON CONFLICT`); a key re-registered on every run; the transaction committed after a refusal; a name difference ignored; keys registered in descending order; a retired key accepted; control characters allowed in names; a duplicate `--tenant` allowed. Two first ended as build errors (an unused import or variable), which is not a kill; both were redone as mutants that build and are killed. One survived at first (the key-mode bypass): the fault test left no room under `--key-max-active`, so the overlap refused the directory before the fault was reached; the test now gives the overlap room and the mutant is killed. The RED tests were re-run at the corrected RED commit against the scaffold: all 23 top-level tests fail on assertions.

### Open questions for the Architect (none decided here)

1. **Which login (the main one).** The brief said the DSN file is "the migrator role". Migration 0030 and ruling noema#716 (A319, A353) say the opposite for tenant rows: `tenant` forces row-level security with no INSERT policy, so the non-superuser migration login cannot insert a tenant (`TestOnlyASuperuserCreatesATenant`; `create-tenants.sh` refuses a non-superuser). `key_register` is executable by its owner (`noema_migrator`) and the superuser. So the one login that does both steps is the operator's superuser, and that is what the command expects; it does not test the role itself, the database refuses any other. Please confirm: (a) the Compose one-shot holds the superuser DSN file (the same credential `create-tenants.sh` uses by hand), or (b) split the command into a keys step (migrator) and a tenants step (superuser), or (c) a policy for the migrator on `tenant` in a new migration (a change to a ruled decision, not mine to propose as a default).
2. **Who creates the DSN file** of that login, and where it is mounted (IL, with the secret handling of OQ-09). The command only reads a file.
3. **The tenant name source.** `create-tenants.sh` writes the ID as the name. The command takes `ID=NAME` so that IL can pass a name; if every name equals its ID, the interface can drop the `=NAME` part. Is the name a display name, an operator-chosen label, or always the ID in the POC?
4. **The time zone of a tenant.** Not an argument: a new tenant gets the column default `UTC`, and an existing one is never changed. If a tenant needs another zone in the POC, it is a further flag or a separate step.
5. **Other identities.** A484 names only the `core-trust` key. The migration allows eight signing identities (`core-execution`, `core-outcome`, `core-evidence`, `core-workflows`, `core-edge`, `core-record`, `admin`). Should the Compose one-shot register those too (the receipt signature of migration 0013 needs `core-execution`)? As built, adding one means a second key directory and run, or an `--identity` flag with a closed list; I did not add one (AGENT-03).
6. **One transaction for both steps.** The brief allowed one per step. The command uses one for both so that a refusal (for example a changed key) leaves no tenant behind; the cost is that a key problem blocks the tenants. Say if two transactions are wanted.
7. **Exit code of a bad key file.** The loader refuses a file by mode, name, link, owner or content; the command reports it as exit 3 ("could not run"), not 1, because the loader's errors are untyped and the fault is in the environment. Say if 1 is wanted.
8. **`--key-max-active` range.** 1 to 16 is an operating bound against a typo; the database separately refuses a third ACTIVE key (migration 0013). I did not encode the database's "two" in the command.
9. **Output on a refusal.** Stdout is empty and the sentence is on stderr; the counts are printed only for a committed run. Say if the log-line shape of `policypublish` (PERD 44.2 fields) is wanted instead.
10. **CODEOWNERS and the status index.** A new top-level owned path needs `/services/core/cmd/tenantinit/  @OladiranOlatunde @noemai-systems` (AGENTS section 11); not added here (a protected path). `status/README.md` also needs one index line for this file; not edited here (outside the allowlist; the Steward or IL adds it).

## Pre-work for the next agent
- Exact next step: when the Founder says so, open the PR from `g1/tinit` with the plan, the SoT section 52 questions, the plan 13.1 checklist and the CI run links, and add the CODEOWNERS line in the same PR (a protected path: it needs the Founder's approval). Ask the Architect question 1 first: it decides the credential IL mounts.
- Open questions (owner): the ten above (Architect; IL for 2).
- Files to read first: `services/core/cmd/tenantinit/README.md`, `services/core/migrations/0013_receipt_signature.sql` (`key_register`), `services/core/migrations/0030_tenant_row_security.sql`, `deploy/compose/postgres/create-tenants.sh`.
- Do not touch: `services/core/migrations/`, `execution/signing` (the command only calls `LoadRing`), `deploy/compose/` (the Compose one-shots are IL's).

# Session 2026-11-20 17:50 WAT · TRU-02 (tenantinit, the Security Reviewer's DSN note)

## Session summary
- What changed (PR links): noema#852: the DSN file is read only if it is a regular file (not a link), owned by the caller (`connectAs(ctx, path, euid)`), with no group or other permission bit; any other file is the fixed refusal "the DSN file cannot be used" (exit 3, nothing written). `owner_unix.go` / `owner_other.go` supply the owner (a platform without one refuses, fail closed).
- Why: the Security Reviewer's note on noema#852 asked for the same checks as the other one-shots' secret files; the first version checked only "regular file and size".
- Tests added (REQ-SEC-005; INV-12, AGENT-13, A347): `dsn_file_test.go`: eight modes with a group or other bit are refused with nothing written; a file of another owner is refused and one of the caller (mode 0600) is used. Seen failing in CI (RED `ba30dab8`, shard `tests (2)`: exactly these two tests).
- Regression cases added: none. Debt entries: none.
- Conflicts raised: the tenant-row CONFLICT block on noema#852 is open (A353 guard versus A484); this change is independent of it.

## Pre-work for the next agent
- Exact next step: the Architect's answer on the tenant rows and the login (see the PR comments); then either drop the tenant half or amend the guard.
- Do not touch: `deploy/compose/postgres/test_provision_env.sh` (the guard) without that decision.

# Session 2026-11-21 15:10 WAT · TRU-02 (tenantinit, ruling A489: the core-trust public key only)

## Session summary
- What changed (PR links): noema#852: `tenantinit` registers the core-trust public key only and creates no tenant row (A489, interim record `DEC-2026-11-21-architect-rulings-a489-a492`, which amends A484). The `--tenant` flag, the tenant insert, the name check and the two tenant counts are removed; the output is `{"keys_registered":N,"keys_existing":N}`. A run neither creates, changes nor removes a tenant row.
- Why: A353 gives tenant rows one creator, `create-tenants.sh`, and its guard (`deploy/compose/postgres/test_provision_env.sh`, job `compose-scripts`) fails on a second one. A489 chose this option (my option 1 in the CONFLICT block on the PR).
- Risks, and how they are contained: the Compose one-shot IL writes against the old flags and output must drop `--tenant` (a usage refusal now, exit 2, before any file is read). The command no longer needs the superuser: any login that may call `key_register` works; the database decides.
- Tests added (REQ-SEC-004, REQ-SEC-005; INV-11, INV-12; AGENT-13, A347): `tenants_test.go` (no tenant row after two runs; an existing tenant row with its kill switch and time zone is untouched; a runtime login is refused for keys); `args_test.go` (a `--tenant` flag in either form is a usage refusal with nothing opened); the key, DSN-file and secret tests carry the two-count output. RED was read failing in CI before GREEN (A302).
- Regression cases added: the `--tenant` refusal and the no-tenant-row test. Debt entries: none.
- Conflicts raised: the tenant-row CONFLICT block on noema#852 is resolved by A489.

## Pre-work for the next agent
- Exact next step: IL wires the Compose one-shot without `--tenant`; the open questions 1, 3, 4 and 6 above are closed by A489 (the tenant half is gone).
- Open questions (owner): the DSN login of the one-shot, owner of `key_register` or the superuser (IL, OQ-09); other signing identities (Architect, question 5 above).
- Files to read first: `services/core/cmd/tenantinit/README.md`, `deploy/compose/postgres/create-tenants.sh`, `deploy/compose/postgres/test_provision_env.sh`.
- Do not touch: `services/core/migrations/`, `deploy/compose/` (IL's).
