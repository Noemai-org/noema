# Status · DEBT-04 (Core Engineer 1) · the tenant of a Record call comes from the login

Updated 2026-11-03. DEBT-04 (Tier 0): `record_lock_head`, `record_insert` and `record_read_chain` trust a caller-supplied tenant. Option A (a login per tenant and a database-checked mapping) is the fix; the design is in the comment on noema#107. Target: merged by Tue 10 Nov; limit Fri 20 Nov (the Architect).

## State: slice 1 of 3 in review (draft; stacked on noema#107, because it needs migration 0003 and numbers 0004)

- **Slice 1 (this branch, `feat/DEBT-04-tenant-bound-record`):** migration `0004_tenant_bound_record.sql` adds `tenant_login` and the tenant-bound functions `record_lock_head_t()`, `record_insert_t(...)`, `record_read_chain_t()`, which find the tenant with `session_user`. The old functions are untouched and still callable (the known gap).
- **Slice 2:** the Go callers (`record/chain` Appender, `record/reconstruct`, the verifier) move to the new functions; `platform/db` gets the per-tenant, per-power pool router; a migration `0005` revokes EXECUTE on the old functions from every role; the DB-level test skipped in noema#107 becomes real and the skip is removed.
- **Slice 3:** the verifier's all-tenant login and function (REC-05), the tenant provisioning step (create the logins and the mapping row), and the deploy secrets.

## Session 2026-11-03 · DEBT-04

### Session summary
- What changed (PR links): the DEBT-04 slice 1 pull request on `feat/DEBT-04-tenant-bound-record`.
- Why: REQ-SEC-004, INV-12; noema#107's Codex review blocks on this.
- Risks, and how they are contained: slice 1 adds functions and a table and removes nothing, so existing behaviour cannot break; it also does not close the gap by itself. The tests connect as real LOGIN roles (SET ROLE would leave `session_user` as the administrator).
- Tests added (REQ-SEC-004, INV-12): `platform/db/tenant_bound_test.go` (5 tests, 10 refusal cases). `record_read_test.go` no longer assumes 0003 is the last migration.
- Regression cases added: none (new). Mutations run and reverted: `current_user` instead of `session_user` (3 tests fail); the mapping table readable by a power (1 fails). One mutation survives by design: removing the read/write kind check inside `record_login_tenant`, because the function grants already refuse the wrong kind of login; the check is defence in depth.
- Debt entries: DEBT-04 remains open until slice 2 revokes the old functions.
- Conflicts raised: Codex (review 5754127112) will not accept noema#107 until the DB binding exists or a committed staging rule does; the Architect's staging order is not committed (docs#89 is open). Escalated in the report.

### Pre-work for the next agent
- Exact next step: slice 2 (see above). Answers to the three open points on noema#107 (a login per power; who provisions the logins; the verifier's all-tenant login) shape it; my defaults are per power, the deploy's tenant-creation step, and yes.
- Files to read first: `services/core/migrations/0004_tenant_bound_record.sql`, `services/core/platform/db/tenant_bound_test.go`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).
