# Status · DEBT-04 (Core Engineer 1) · the tenant of a Record call comes from the login

Updated 2026-11-03. DEBT-04 (Tier 0): `record_lock_head`, `record_insert` and `record_read_chain` trust a caller-supplied tenant. Option A (a login per tenant and a database-checked mapping) is the fix; the design is in the comment on noema#107. Target: merged by Tue 10 Nov; limit Fri 20 Nov (the Architect).

## State: slices 1, 2 and 3 built (draft; stacked on noema#107, because it needs migration 0003 and numbers 0004 and 0005)

- **Slice 1 (this branch, `feat/DEBT-04-tenant-bound-record`):** migration `0004_tenant_bound_record.sql` adds `tenant_login` and the tenant-bound functions `record_lock_head_t()`, `record_insert_t(...)`, `record_read_chain_t()`, which find the tenant with `session_user`. The old functions are untouched and still callable (the known gap).
- **Slice 2 (built):** `Appender.Append` and `chain.ReadChain` use the tenant-bound functions; `chain.Verify` (administrator and verifier path) reads the table directly; `reconstruct.Handler` takes a `TenantDBs` (one pool per tenant, no fallback); `0004` also makes `record_insert_t` refuse an event whose body names another tenant; `0005` drops the three functions that took a tenant argument; the DB-level test skipped in noema#107 is real and the skip is removed; every test world logs in as per-tenant roles (`testpg.TenantLogin`). The `Ledger` is unchanged: it is bound to one pool, so a Ledger is built per tenant on that tenant's login (the held noema#94 and #90 rewrite the ledger and do the same on merge).
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

## Wider tenant gap: Tier 1, for the Steward (decided 3 Nov 2026: register it, close it before G3 evidence)
DEBT-04 closes the tenant gap in the Record functions only. The other tenant-scoped tables (`decision_grant`, `grant_spend`, `idempotency_record`, `execution_receipt`, `recovery_request`, `outbox_event`, and their kin) are still selected and written by `tenant_id` values that the caller's SQL supplies, under the per-power group roles, so a power with a pool for one tenant could name another tenant in a query. Fix: bind these tables to the tenant login the same way (row-level security with a policy that reads `session_user` through the `tenant_login` mapping, forced on the table owner too), then test cross-tenant denial per table under the real logins. Owner: Core Engineer 1 with the Engineering Lead and the Security Reviewer. Closed before G3 evidence. Tier 1.

## For the Engineering Lead's US-01 and US-06 test in `services/core/execution/e2e/` (support, 3 Nov 2026)
`newStack(t, mc)` in `stack_test.go` already builds the Record, the Ledger and the Controller on the tenant's own logins (`testpg.TenantLogin`: `execution_rw` for the Ledger, `trust_rw` for the Trust stand-in), with the synthetic case system behind the bridge. `stack.reader(t)` returns the tenant's `record_read` login: pass it to `reconstruct.Build(ctx, pool, tenant, correlation)` to reconstruct what the run wrote (US-06 after a write; US-01 after a read decision), and to `chain.ReadChain` + `chain.VerifyRows` to verify it. `stack.admin` can tamper (with `session_replication_role = replica`) for a negative case. Events written by Understanding, Trust and Outcome are not in the Record yet (only the ledger writes today), so a full story needs their appends through `chain.Appender` on a login of their own power: `testpg.TenantLogin(t, s.admin, s.dsn, "trust_rw", tenant)`.
