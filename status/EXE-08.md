# Status · EXE-08 (Core Engineer 1) · W4, database-side verification of receipts

Updated 2026-11-05. Plan task EXE-08: owner C1; REQ-AUD-006, REQ-EXE-004; Needs EXE-07 (slice 1 merged in noema#129) and SEC-12; done when failing-first forgery, replay and downgrade tests pass and noema#94 and noema#90 can be released.

## State: slice A in review (draft until CI is green at the final head and Codex is READY). EXE-08 is not Done.

## Slice A (this session): ownership and privileges, no extension
- Migration `0010_record_tables_to_migrator.sql`: the Record tables belong to `noema_migrator`; `record_append` holds grants, not ownership, and loses `CREATE` on `public` (DEBT-05). D14, D15, D16 and §6.1 of DEC-2026-11-01-receipt-and-record-signing.
- Tests (`platform/db/ownership_test.go`, written first and seen failing): the tables' owner; no runtime role holds `CREATE` on any schema or on the database; no runtime role is a member of `noema_migrator` or has a privileged attribute; `record_append` cannot disable a trigger on, alter, drop, truncate, update, delete from or shadow around the Record tables.
- The test cluster now creates `noema_migrator` and gives `record_append` `CREATE` on `public` before migrating, exactly as `init.sh` does, so the pin can fail.
- `deploy/compose/postgres/test_init.sh` (which CI does not run) was broken by noema#145's two new secrets; fixed, extended to apply every migration as the plain migration login (it works on PostgreSQL 16 locally) and to check the owner and the revoked `CREATE`.

## Not in slice A, and why
- **pgsodium, the custom image, `noema_sig.verify`, `signing_key`, the receipt trigger, `receipt_insert`, `execution_receipt.signed_jcs` (D10 to D13, tests 1 to 6, 8 to 16).** OD-P-72 (adopting pgsodium, AGENT-14) is still `[OPEN]` in `docs/open-decisions.md` and DEC-2026-11-01 still reads `Proposed`, though docs#80 is merged. "Nothing is built from this entry until the Founders approve it." Needs the Architect's or Founders' confirmation that the dependency is approved, and a committed record of it.
- **`cmd/migrate` and the one-shot `migrate` service (D14a, test 18)**: next slice; it touches `deploy/compose` and the core image.

## Pre-work for the next agent
- Exact next step: slice B once OD-P-72 is recorded as approved: the image (digest-pinned base, libsodium by SHA-256 and signature, pgsodium by commit), the bootstrap with the event trigger disabled, the guards in `db.Migrate` and core start-up, then the migration with `signing_key`, `noema_sig.verify`, the receipt trigger and `receipt_insert`.
- Open questions (owner): is OD-P-72 approved (Architect/Founders; the record is still `Proposed`); wire `test_init.sh` into the `compose` job (needs a `.github` change, a founder approval).
- Files to read first: `docs/decisions/DEC-2026-11-01-receipt-and-record-signing.md` §5, §6, §10.
- Do not touch: noema#94, noema#90 (held until W4 is on `main`).

# Session 2026-11-06 16:30 WAT · EXE-08 (A6 slice 1: Record function hardening, noema#234)

## Session summary
- What changed: migration 0014 replaces the five record_append functions with a pinned `pg_catalog, pg_temp` search_path and schema-qualified names. Failing tests first (commit 7b675e0): the catalog test, and an exploit test in which a ten_a writer login with a temporary `tenant_login` table created a chain head for ten_b.
- Why: ruling A6. The exploit is real on main: such a login could write and read another tenant's Record chain.
- Risks: CREATE OR REPLACE needs the migrating role to own or be a member of record_append (as 0003 to 0006 already needed). Number 0014 may need D3 renumbering.
- Tests added: REQ-SEC-004, INV-12.
- Conflicts raised: none.
- Not yet done in A6: RLS + FORCE on grant_spend, idempotency_record, execution_receipt, recovery_request, outbox_event, record_event, record_chain_head (it needs the tenant mapping function of noema#168 and a design for the verifier role, which reads all tenants on purpose); the A20 list of paths for SR.

## Pre-work for the next agent
- Exact next step: RLS slice stacked on noema#168 (uses `decision_login_tenant()`); write the A20 paths list.
- Do not touch: noema#168's migration 0011.
