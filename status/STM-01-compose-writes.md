# Status · STM-01-compose-writes · composition gap: Writes (A684 step 1)

Updated 2026-11-26 WAT. Owner: Engineering Lead (built by a subagent; unreviewed work). REQ-STM-001, REQ-TRU-004, REQ-NFR-002; INV-01, INV-03, INV-12, INV-14, INV-16; AGENT-13, AGENT-16. Rulings (all "interim, relayed, not yet committed"): A684 (one PR, one composition gap), A543, A364, A551 (the pack's `write_when` rule and its accessor), A537, A469. All data is SYNTHETIC.

## State
- Red committed locally (`0d0cdb86`), not pushed. Green committed locally on top, not pushed. EL pushes after reading the red job.
- DB-backed tests of `cmd/core` run in CI only (pgsodium). Locally, `TestComposedWritesReadTheTenantsOwnKillSwitch` ran (it needs only the tenant table) and passed; the pgsodium Record tests of the package fail locally and are unrelated.

# Session 2026-11-26 WAT · STM-01 (composition gap: Writes)

## Session summary
- What changed (PR links): none yet. New `cmd/core/compose_writes.go` (`buildWritesInput`) and `compose_writes_test.go`; `compose_trustinputs.go` gains the `Writes` field, its `fill` and one call; the gap test `compose_trustinputs_gaps_test.go` is pinned to the new state (gaps are Reconciler and Clocks); the stale comment of `trustport.NewWrites` is corrected. `compose.go` is not touched: it does not call `buildTrustInputs` on main, the final composition does, so the one call lives in `buildTrustInputs`.
- Why: noema#935 put the production `WriteRule` (`NewPackWriteRule` over `Pack.WriteWhens()`) and `intake.NewWrites` on main, so Writes is no longer a named gap whatever is given.
- Risks, and how they are contained: fail closed. No pack, an unusable pack (or one with no intent node), no served tenant or no login is a gap (nil port, named by /readyz as `trust.Writes`, no value). A `write_when` that does not compile and a bound that is not positive are errors with fixed text; no error carries a predicate, a slot, a tenant or a path.
- Tests added (REQ-STM-001, REQ-TRU-004, REQ-NFR-002; INV-01, INV-03, INV-12, INV-14, INV-16; AGENT-13): `compose_writes_test.go` (9 tests).
- Regression cases added: none (no bug).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the final composition calls `buildTrustInputs` and passes `in.fill(adapters)` into `TrustAdapters`; Writes then needs only the pack, the tenants, their logins and `NOEMA_TRUST_DB_TIMEOUT_MS`.
- Open questions (Architect): none. A693 settled it: a pack with no `write_when` on any node answers "no write" (A543) and is built, not a gap.
- Files to read first: `cmd/core/compose_writes.go`, `orchestrator/portadapters/trust/writerule.go`, `status/TRU-02-write-rule.md`.
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`.

# Session 2026-11-26 WAT · STM-01 (A693: a pack with no write_when is built)

## Session summary
- What changed (PR links): none yet (#990 is the PR). `buildWritesInput` no longer treats a pack with no `write_when` on any node as a gap: it builds the rule, which answers "no write" for every node the pack holds. Nil, zero or unusable pack, no served tenant and no logins stay gaps; a `write_when` that does not compile stays an error. The test that pinned the gap now pins built-and-no-write.
- Why: A693 (interim, relayed, not yet committed): a pack with no `write_when` answers "no write" (A543); it is not a gap.
- Risks, and how they are contained: the rule still refuses a node it does not hold and an intent with a missing required slot (false with an error). SR's condition for A693 (packcheck refusing a write-capable node with no `write_when`, #963 on main first; #990 tests the shipped pack) is outside this change; the shipped pack's cases are unchanged.
- Tests added (REQ-TRU-004; INV-03, INV-14): `TestAPackWithNoWriteWhenIsBuiltAndAnswersNoWrite`.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL pushes the red, reads the job, then the green.
- Open questions (owner): none.
- Files to read first: `cmd/core/compose_writes.go`.
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`.
