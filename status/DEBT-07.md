# Status · DEBT-07 (Core Engineer 1) · writer identity for emitters that are not a power

Updated 2026-11-05. DEBT-07 (Tier 0 until closed with EDG-01; docs#106 R16, OD-P-73). Architect's decision on Q-DEBT04-1: option (b), a login per such emitter, `core-edge` first.

## State: slice 1 in review (stacked on noema#178 because it extends `test_init.sh`). DEBT-07 is **not** closed.

## Session 2026-11-05 · DEBT-07
### Session summary
- What changed (PR links): the PR on `feat/EDG-01-emitter-logins`: migration `0011_provision_emitter_login.sql`; `init.sh` creates `record_emitter__<tenant>__<emitter>` logins (`NOEMA_EMITTERS`, default `core-edge`); `provision-tenants.sh` maps them; `platform/db/emitter_provision_test.go`; `test_init.sh` proves the whole path (init, every migration as the plain migration login, provisioning, the `core-edge` login locks its head and is refused on the Record tables, grants and receipts).
- Why: noema#145 bound the five power identities to their logins. The rest (`core-edge`, `core-record`, `admin`, `model-service`, the adapters) were refused to power logins (0004) but no login could write them. This gives a writer an identity the database can check, and the edge a login for `SESSION_VALIDATED`.
- Risks, and how they are contained: a login per emitter is more credentials in one process (the core), so the boundary is the database mapping, not the secret (the same stance as the tenant logins). An emitter login is a member of `record_emitter` only (append functions, no table, no power); the function refuses a login that is also in a power group.
- Tests added (REQ-AUD-006, REQ-SEC-004, INV-12; DEBT-07): the provisioned login writes as its emitter in its tenant and as nothing else, and not for another tenant; 13 kinds of wrong input refused (no role, NOLOGIN, a power group only, an emitter login that is also a power, the five power identities, empty, upper-case and spaced emitters, an unknown tenant, the same login twice); no runtime role, tenant login or emitter login can call the function. Seen failing first (the function did not exist).
- Regression cases added: none. Debt entries: DEBT-07 stays open (below). Conflicts raised: none.

## What closes DEBT-07 (and what does not yet)
- **Not closed:** (1) the edge does not write `SESSION_VALIDATED` yet, so no code uses the `core-edge` login; (2) `core-record` and `admin` have no login (refused until their writers exist); (3) `model-service` and the seven adapter identities have no login by OD-P-73's default; (4) the cross-tenant tests have not been validated by the Security Reviewer (the Architect's condition).
- Pre-work for the next agent: wire the edge's Record writer to the `core-edge` pool (EDG-01 wiring, blocked on the claims decision, below); ask the Security Reviewer to validate `emitter_provision_test.go` and the `test_init.sh` path; decide OD-P-73.
- Do not touch: noema#94, noema#90 (held until W4 is on `main`).

## Session 2026-11-06 · A4: each emitter login has its own file-mounted secret
### Session summary
- What changed (PR links): the EDG-01 emitter-login pull request (noema#181), main merged in after noema#178: `deploy/compose/postgres/init.sh` (a pre-flight before any role is created), `secrets_local.sh`, `docker-compose.yml` (a read-only mount of `secrets/emitter_logins/` into postgres only), `test_init.sh`, the compose README.
- Why: the Security Reviewer's NOT READY on `83fb4fb` (a leaked shared password let one emitter authenticate as another emitter or a power) and ruling A4 (docs#117): each emitter login gets its own file-mounted secret; DEBT-13 (Tier 1) extends the same to the tenant and power logins before G3 evidence.
- Risks, and how they are contained: `init.sh` refuses, before it creates anything, a login whose own file is missing, empty, or equal to any other secret (the core's, Temporal's, the migrator's, the verifier's, the shared tenant-login password, the superuser's, or another emitter's); a refusal names the file and never prints a value. The tenant and power logins still share one password until DEBT-13.
- Tests added (REQ-SEC-005, INV-12, INV-29): `test_init.sh` now runs with password authentication on (the superuser trusted on the socket, every other login must present its password): five refusals before anything is created (missing, empty, equal to the shared password, equal to the core's, two logins with one secret), the emitter logs in with its own secret, and the shared tenant-login password does not open it. Mutations run and reverted (both caught): the emitter login given the shared password; the duplicate check removed. `test_init.sh` is not run by CI (it needs a `.github` change, a founder approval); I ran it locally on PostgreSQL 16.
- Regression cases added: the refusal cases above. Debt entries: DEBT-13 (filed by docs#117). Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: DEBT-13, the same per-login secret for the tenant and power logins (`init.sh` loop over `trust_rw` and the other groups); A6 for the Execution and Record tables.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-10 · DEBT-07 / EDG-01 slice 2: the upgrade-path test (ruling A113)
### Session summary
- What changed (PR links): noema#181: merged `main` (the README conflict only) and added `services/core/platform/db/migration_order_test.go`.
- Why: the Architect's ruling A113 (noema#393, 10 Nov): the numbers stay as assigned (the gap is accepted) and the files merge in strict ascending order (no out-of-order merge); 0017 (#265) is already on `main`, so a database that holds 0017 is later given 0011 to 0016, and it must end with the same catalog as a fresh one. This PR is the first of the lower-numbered files to merge, so it carries the test.
- Tests added (REQ-DATA-001, REQ-AUD-006): `TestALowerNumberedMigrationAppliedAfterAHigherOneGivesTheSameCatalog`: for every file numbered 0011 to 0016, migrate a database without it, then with it, and compare with a fresh database: function bodies (`pg_get_functiondef`), owners, ACLs and `search_path` settings, triggers, relations and column ACLs, constraints, indexes, policies. It passes on this branch (a guard; there is no red run). A temporary pair of files, `0012` creating a function and `0016` replacing it, makes it fail on the function body (checked and removed).
- Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- The test finds the lower files by glob, so the next lower-numbered PR to merge (0012, 0013, 0014, 0015, 0016) is covered with no change to it. Merge order is strictly ascending (A113): #181, #168, #229, #233, #259, #234, #261, #263, #268, #272.

## Session 2026-11-10 · EDG-01 / DEBT-07: C2's P2 on noema#181 (search_path of provision_emitter_login)
- What changed (PR links): noema#181: `0011_provision_emitter_login.sql` pins `provision_emitter_login` to `search_path = pg_catalog, pg_temp` and writes `public.tenant_login` by schema; two tests in `platform/db/emitter_provision_test.go`.
- Why: C2's second human review found that the function (owned by `record_append`, SECURITY DEFINER) was pinned to `pg_catalog, public`, so noema#234's catalog test (`TestEveryRecordFunctionPinsItsSearchPathToCatalogAndTemp`, every function `record_append` owns) fails once #181 merges first, and an unqualified `tenant_login` resolved to a caller's temporary table first. 0011 is not merged, so it changes in place; this replaces my earlier reply to the Security Reviewer (note 1, "a forward migration with DEBT-21").
- Tests: failing first at `1ab3944f` ([run](https://github.com/Noemai-org/noema/actions/runs/42803171829/job/129785589155), `core` red): the catalog pin test, and a temporary `tenant_login` table must not capture the mapping (before the fix the call resolved the temporary table and failed with 42501).
- Debt entries: none new; the provision_emitter_login line I meant to add to DEBT-21 is not needed.
- Conflicts raised: none.
