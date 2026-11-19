# Status · TRU-02 · `cmd/policypublish`, the policy publication command for bench seeding

Updated 2026-11-19. Owner: Core Engineer 1 (helper session). Item 3 of the A341 to A357 batch. The rulings A341 to A357 were relayed by the Founder in the session and are NOT committed (AGENTS.md section 2.3). The only text the session had for this item was "the policy publication command for bench seeding". Everything below that goes beyond that sentence is the safer reading, written down so the Architect can confirm or correct it. Nothing is pushed; the work is two local commits on `prep/r3-policypublish`.

## Session 2026-11-19 (C1 helper)

### Session summary
- What changed (PR links): none yet (local commits only). New: `services/core/cmd/policypublish/` (`main.go`, `pack.go`, `publish.go`, `README.md`, `helpers_test.go`, `publish_test.go`) and this file. No other path is touched; no migration, contract, dependency or deploy script is changed.
- Why: policies reach the Trust intake through `trust/policystore` (a tenant's published versions), and nothing outside tests publishes a version for a tenant. A bench run has no policy in force, so every request ends REJECT `NO_PERMITTING_RULE`.
- Risks, and how they are contained: see "Doubts" below. The command writes only through `policystore.Publish`, as the tenant's own Trust login, so the database guard (migration 0015) is the authority; the command adds no event, no SQL write and no version comparison of its own.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-005, REQ-TRU-004, REQ-AUD-001, REQ-SEC-004, REQ-SEC-005, INV-09, INV-12, AGENT-13, PERD 32.4 (the Record comes first). Real PostgreSQL through `internal/testpg`, the real migrations, the tenant's real `trust_rw` login and the real Record signer.
- Regression cases added: the whole `cmd/policypublish` suite is new; the RED commit `e8bb24bf` has the tests with a stub that exits 2.
- Debt entries: none added. DEBT-23 (the policy document store) is what blocks "a different document under the same version is an error" (below).
- Conflicts raised: none. One piece STOPPED (below).

### Usage

```
policypublish --tenant ten_aa --pack-dir packs --pack-file fintech_v1.json \
  --dsn-file FILE --key-dir DIR --key-max-active N --published-by adm_ops01
```

Exit 0 published or already published; 1 refused (`login_not_for_tenant`, `not_newer`, `invalid_policy`, `invalid_request`); 2 could not run. One JSON log line on stdout; one fixed sentence on stderr when the exit is not 0. Full table in `services/core/cmd/policypublish/README.md`.

### What a bench seed script must pass (and where it would call this)

Not edited here; named only. The call belongs in the bench stack's seed step, after the three things that already exist have run: `deploy/compose/postgres/init.sh` (creates the `trust_rw__<tenant>` login with the tenant-login secret), the migrations, and `deploy/compose/postgres/provision-tenants.sh` (maps that login to the tenant in `tenant_login`; the tenant row itself is made by the tenant-creation step that script names). It would sit with `deploy/compose/bench_tokens.sh` (which already takes `BENCH_TENANT`) in the bench seeding order, once per tenant. For each tenant it must pass:

1. `--tenant`: the same tenant ID `BENCH_TENANT` carries.
2. `--pack-dir` and `--pack-file`: the pack the core mounts (`packs/` and `fintech_v1.json` today), so that the published version is the version of the policy the core loads.
3. `--dsn-file`: a 0600 file the script writes from the secrets directory for the login `trust_rw__<tenant>` (host `postgres`, database `noema`). The DSN is built inside the script from the secret file and never appears in `argv`, `env` or a log. It must be that login: the administrator's, the migration login's and `noema_core`'s are all refused (see doubt 2).
4. `--key-dir`: a directory holding `core-trust.ed25519.<version>.key` (0400, owned by the user running the command). **Nothing in `deploy/` makes or registers a `core-trust` Record signing key today** (a grep of `deploy/` finds no `key_register` call and no Ed25519 key but the spike's), so without it the append fails and the command exits 2 with nothing written. See doubt 3.
5. `--key-max-active`: the deployment's rotation overlap (REQ-SEC-005). There is no default; the command does not choose it.
6. `--published-by`: the principal ID the Founders give the seeding administrator (the form `adm_<name>`). There is no default.

The script should treat exit 0 as success (both "published" and "already published"), and exit 1 or 2 as a stop. It runs once per tenant.

### What was observed

- `cd services/core && go build ./... && go vet ./cmd/... ./trust/policystore/... && gofmt -l .` clean; `go test -count=1 -p 2 ./trust/policystore/... ./cmd/...` all `ok` (policypublish 16 s); `python3 tools/lint/run.py` prints `lint OK`. CI run links: none yet (nothing is pushed).
- RED commit `e8bb24bf` (stub exits 2; the publish, idempotency, downgrade, unknown-tenant, wrong-login and race tests fail on assertions); GREEN commit `4f082836`; a third commit adds the tests that killed three surviving mutants, the README and this file.
- Hand mutants, each killed, source restored after: no re-read after a refused insert (idempotent re-run and race tests); any active version counted as "already published" (older-after-newer test); no login check; login check without the tenant compare; without the role check; a commit that never commits; pack read without the root (parent path and link out); DSN file followed through a link; empty DSN allowed (ambient PG* fallback); key overlap fixed at 1; version fixed by a constant; connection error text kept in the error value; default failure sentence printing the error text; refusal exiting 2; invalid policy exiting 2; tenant check dropped from usage; `--key-max-active` not required. Three first survived (error value text, default sentence, usage check on max-active); tests were added for each and all three are now killed. One pre-check ("is the version already active") was removed because its mutant was equivalent.

### Doubts, for the Architect (none of them decided here)

1. **STOPPED: "a different document under the same version is an error".** The publication record is `(tenant_id, policy_id, version, version_key, published_by, published_at)`. It holds no document and no document hash, and `policystore` has no document store (DEBT-23, open). The command cannot tell two documents of one version apart, so a changed rule set under an unchanged version is reported `already_published`. I did not add a hash column (a migration and a contract change are not this item), a hash in the `POLICY_PUBLISHED` payload (the brief says add no events of my own, and the payload is policystore's), or a file next to the pack (a second store of authority, against INV-30's spirit). The safe use is: change the version in the pack when the rules change. What would settle it: a decision that `POLICY_PUBLISHED` carries a document hash (a policystore change) and/or that the publication row does (a migration), after which this command compares it.
2. **Which login publishes.** The brief allowed "the tenant's `trust_rw` login or the operator login". Reading migration 0015: the guard on `policy_publication` accepts only a login mapped in `tenant_login` with `power = 'trust'` for that tenant (`session_user`), and only `trust_rw` holds INSERT. The operator (the migration login, a superuser) is not mapped and is refused by the database. So the one login that can publish is `trust_rw__<tenant>`. No privilege is missing: no stop. The command refuses the other logins itself, with the same answer for a tenant that does not exist (it does not tell a login which tenants exist).
3. **The `core-trust` Record signing key.** `policystore` appends as `core-trust`, and the database verifies the Ed25519 signature of every Record event (migration 0019). The brief named a DSN file and "key material from a FILE"; I read that as the `core-trust` key directory, loaded with the core's own loader (`execution/signing.LoadRing`). A bench stack that does not yet provision and register that key cannot use the command; whoever owns the seed step (IL) has to make the key, `key_register` its public half, and mount it. This is not a gap of the command; it is a gap in the seed order I could not fill from `deploy/`.
4. **Vocabulary the pack's policy is checked against.** `policy.Parse` needs a vocabulary and nothing in production builds one from a pack (only a test helper does). The command builds it from the pack's own `evidence_descriptors`, `predicate_variables` and the `capability_id`s of `capabilities`, as the shipped-policy test does. It does not add intent node IDs to the targets (the loader's text allows "a capability or an intent node"), so a rule that applies to an intent node would be refused here. That is the stricter reading; the shipped policy applies to capabilities only. The scope builder is a small copy of the one in `outcome/predicate` and the test helper, because `trust` must not import `outcome`; a shared builder is a refactor for its own PR.
5. **Version and publisher patterns are policystore's.** The command does not restate them: a version such as `3.1.0` (the store accepts only one spelling) or a `--published-by` outside the pattern comes back as exit 1 `invalid_request`, not as a usage error.
6. **`status/README.md` is not updated.** Section 2 of the status rules asks for the folder README to be updated in the same PR; the index there is edited by many PRs and a line from this local session would collide. The Steward or IL adds: "`TRU-02-policypublish.md` (19 Nov 2026): the policy publication command `cmd/policypublish`, its seed-script contract and the document-hash limit."
7. **File size.** `publish_test.go` is about 500 lines (the shared world is in `helpers_test.go`); one line of justification for the PR: one file per behaviour group, each test names its REQ or INV.
8. **No attribution of the rulings.** The commit messages cite TRU-02 only; no A-number is cited as authority because none is committed.

## Pre-work for the next agent
- Exact next step: open the PR from `prep/r3-policypublish` (when the Founder says so) with the plan, the SoT section 52 questions, the plan 13.1 checklist and the CI run links; ask the Architect the two questions in doubts 1 and 3.
- Open questions (owner): where the document hash lives (Architect); who makes and registers the `core-trust` signing key in the bench stack (IL).
- Files to read first: `services/core/cmd/policypublish/README.md`, `services/core/trust/policystore/store.go`, `services/core/migrations/0015_policy_publication.sql`.
- Do not touch: `services/core/trust/policystore/` (the command only calls it), the migrations, `deploy/compose/` (the seed script is IL's).
