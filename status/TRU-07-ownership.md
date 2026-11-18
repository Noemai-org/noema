# Status · TRU-07 intake Ownership adapter (Core Engineer 2) · A329, A344

Updated 2026-11-19. The Ownership of the Trust intake (T8), `services/core/orchestrator/portadapters/trust/ownership/` (package `ownershipport`), implementing `intake.Ownership`. Ruling A344 (C2; "built against the stand-in client now"). The branch label TRU-07 is as assigned.

## State: draft pull request (red head, then the implementation). Not Done.

# Session 2026-11-19 · TRU-07

## Session summary
- What changed (PR links): not opened yet. The adapter, its README and this file.
- Why: T8 has no Ownership adapter, so the intake cannot be wired (A329, A344).
- Risks, and how they are contained: the adapter writes nothing and calls no model. A failing reader, an expired context, a missing deadline, an answer outside the contract and an answer cut off at the contract's most each fail closed with a fixed-text error and are never "not owned". The one limit is silence 1 below.
- Tests added (REQ-TRU-002, REQ-TRU-003; INV-11, INV-12, INV-19; AGENT-13, AGENT-16; A347): `ownership_test.go`, 22 tests. The reader in them is a real reader of `candidates-request.v1` / `candidates-response.v1` (scopes by tenant and principal, filters by kind and window, orders, caps at 20).
- Regression cases added: none (no defect).
- Debt entries: none new. The reference filter of silence 1 is for the Architect.
- Conflicts raised: none.

## Silences resolved by the safer reading (A332)
1. **The contract cannot ask "does this principal own this target?".** `candidates-request.v1` has no filter by resource reference. Reading taken, with no contract change: the principal-scoped read of the node's kind over the whole history; "returned exactly the target" is owned; a complete answer without it is not owned; an answer of 20 rows (the contract's maxItems) without it is `ErrInconclusive`, an error, never false (INV-19). Limit: a principal with 20 or more resources of the kind, whose target is not among the newest 20, ends T8 in UNKNOWN. A resource-reference filter on the read is the fix (contract change, AGENT-15, Architect).
2. "The whole history" is the earliest and the latest instant `common.v1` `Timestamp` can hold (`0001-01-01T00:00:00Z`, `9999-12-31T23:59:59Z`). A reader that refuses them is `ErrRead`, a failure, not "not owned".
3. Presence in the scoped answer is ownership. The response has no member that proves the principal, so the scope is held by the request (REQ-CTX-006), as in the Candidates adapter.
4. Which nodes need ownership is pack data in `Config.Nodes` (node to resource kind). A node not in it has no ownership slot and is answered `true` with no read (the port's rule). A composition that omits a node would skip the check: it must list every node whose capability has `principal_must_own`.
5. A target that is empty, malformed or of another kind than the node's, for a node that needs ownership, is `(false, nil)` with no read: the same answer as not-owned and absent (REQ-TRU-003).
6. A tenant or principal off its shape, a tenant that is not the adapter's, and an intent that is not the session's are errors, not false: they concern the call, not the target.
7. "Cut off" is exactly 20 rows (the contract's maxItems), not the pack's raw bound, because this port has no pack bound for it.
8. Every row of the answer is checked against the contract (including the digit run and display length) although only the reference is used; one bad row refuses the whole answer.
9. No timeout of its own: the deadline is the caller's context, so no setting is added.
10. The port carries one `TargetRef`; a node with several `principal_must_own` slots is checked for that one target, as `intake.Ownership` says.

## New setting
None.

## Mutants (hand-made, each killed by a named test run, none a compile error)
64 mutants over `ownership.go`, all killed (run by a script that applies one replacement, runs `go test -race`, and restores). Groups: wiring and zero value (zero value answers nil, nodes not copied, kind, name, no nodes, tenant, reader unchecked); deadline (not required, wrong error); session and intent (tenant shape, principal shape, tenant mismatch, intent tenant, principal, ID, version, correlation unchecked, tenant error as intent error); slot and target (unlisted node not owned or read anyway, non-reference an error, kind, id shape unchecked); request (principal, tenant, kind empty, correlation fixed, window narrowed at either end, version changed); read failure (as false, as `intake.ErrNotFound`, cause dropped, cause printed, kind unreachable); answer (not checked, tenant, correlation, version, cap off by one, candidate and reference duplicates, row kind, candidate ID, negative and huge amount, currency, time, display empty, long, newline, digit run and its off-by-one, last4, direction); found (any row, id suffix, id prefix); cut off (check removed, as false, one early, one late, the cap changed); shared mutable state (an answer cache). Two mutants survived the first run (a duplicate candidate ID with a different reference; an id-suffix match); a test case was added for each and both are killed. One more equivalent mutant (kind dropped from the match) is unreachable because the answer check already requires the kind, and was not counted.

## What was not verified here
- No wiring: the composition (`wire_trust.go`, EL) builds the adapter. No real reader exists yet; the candidates reader is connected later (A344: "built against the stand-in client now").

# Pre-work for the next agent
- Exact next step: the composition builds `ownershipport.New(Config{TenantID: <tenant>, Reader: <the same read port as the Candidates adapter>, Nodes: <every intent node whose capability has principal_must_own -> its resource kind>})` per tenant (A320).
- Open questions (owner): a resource-reference filter on the principal-scoped read (Architect, AGENT-15); whether the real reader accepts the earliest and latest `Timestamp` (EL or whoever builds the reader).
- Files to read first: `services/core/orchestrator/portadapters/trust/ownership/README.md`, `services/core/trust/intake/ports_t7_t9.go`, `services/core/trust/intake/gates_t7_t9.go` (t8).
- Do not touch: `trust/intake` ports; `contracts/`.
