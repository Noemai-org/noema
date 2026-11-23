# STM-01-compose-decisions

State: red pushed (branch `feat/STM-01-compose-decisions`, no PR opened yet); green committed locally, not pushed. Lane 3 of the A537 composition (decisions). Dated 2026-11-23 WAT.

# Session 2026-11-23 WAT · STM-01 (compose, lane 3: decisions)

## Session summary
- What changed (PR links): new `services/core/cmd/core/wire_decisions.go` (`decisionInputs`, `buildDecisions`, `decisionParts.fill`, `decisionRecorderConfig`) and its tests; no existing file touched. No PR yet (EL opens it).
- Why: A537 (interim, relayed, recorded in docs#229): `/readyz` names 19 components that are not built; lane 3 is Confirmations, the Recorder, Intents and the Planner.
- Built: Intents (`trustport.NewStoredIntents` over the verified pack's bytes, the tenant's own trust login, `NOEMA_TRUST_DB_TIMEOUT_MS`); the Recorder (`trustport.NewRecorderAdapter` over `loadRecorderSettings` with grant <= record, the pack's escalation set from `NOEMA_TRUST_ESCALATION_GRAPH`, the grant keys and the seams below).
- Named gaps (fixed text, no value; every one pinned by a test): Confirmations (no read of a request by its id: `trustpg2.Requests` has `Latest` only; no Record events of the confirmation store: `RequestEvents` has no implementation and no confirm-request or confirm-event contract; no pack accessor for the confirmation terms); Planner (no `plangraphs.Pack` over the pack, no `plangraphs.Intents`, no `plangraphs.PlanContexts`, no registry accessor on `trustpack.Pack`; noema#896 merged and changes none of these); `Service.Runnable` and `Service.Handoffs` (no adapter for either intake port, so the first evaluation plans the graph's first steps as before); the confirmation route (`Components.Confirmations`, which needs the same store).
- Recorder seams no adapter on main fills, each a named gap while absent: the Record write login of the core-trust identity (`TxBeginner`), the sealed-facts source (`SealedFacts` needs a snapshot reader, a `StepFactSource` that has no implementation, and the pack's policy id, A512), and the core-trust Record signing keys (no setting names them and `deploy/` mounts none).
- Not done: the `DeclinedRequestID` mapping (noema#892 is open; none of its files is touched); the call into `compose.go` (the inputs come from lane 1, and `trustAdaptersSource` is shared with lanes 1 and 2: EL merges `decisionParts.fill` there).
- Risks, and how they are contained: a gap never builds a quiet adapter (typed nil counts as absent); a present invalid setting or a pack the adapter cannot trust is fatal and never echoes a value.
- Tests added (REQ-TRU-003, REQ-TRU-008, REQ-TRU-009, REQ-TRU-010, REQ-SEC-004; INV-03, INV-12, INV-16, AGENT-16): `wire_decisions_intents_test.go`, `wire_decisions_recorder_test.go`, `wire_decisions_gaps_test.go`, `wire_decisions_config_test.go` (written after the mutation check).
- Hand mutation check: 23 mutants of `wire_decisions.go`, 22 killed; the survivor replaces `absent(in.Keys)` by `in.Keys == nil` and is equivalent (`Keys` is a pointer, not an interface).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.
- Safer readings (A332): an absent setting or input is a gap and an invalid present one is fatal (A469, A486); entropy defaults to crypto/rand (not a setting); a missing clock with every other input present is an error.
- DB tests of `cmd/core` need pgsodium and run in CI only; 11 of them fail locally for that reason and none is this lane's.

## Pre-work for the next agent
- Exact next step: EL merges `decisionParts.fill` into `trustAdaptersSource` (lane 1 gives the pack, the pack bytes and the policy id; lane 2 gives Evidence for the sealed facts). The Confirmations route and path wait on the gaps above.
- Open questions (EL): the core-trust Record signing keys and write login settings (names, `deploy/`); who writes the confirmation Record events and its contracts; who writes `plangraphs.Pack`, `Intents` and `PlanContexts`.
- Files to read first: `services/core/cmd/core/wire_decisions.go`, `wire_trust_adapters.go`.
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`, `deploy/`, `wire_understanding_compose.go`.
