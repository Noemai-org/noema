# STM-01-compose-decisions

State: PR noema#906 (draft); red 7ef12061 failed in CI; first green c9606845 pushed; second round (merge of main, one source of the pack, core-trust signing keys: A548, A553) red pushed, its green held locally. Lane 3 of the A537 composition (decisions). Dated 2026-11-23 WAT.

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

# Session 2026-11-23 WAT · STM-01 (compose, lane 3: second round)

## Session summary
- What changed (PR links): noema#906. Merged `origin/main` (noema#900, #911 on it). One source of the pack: `decisionInputs.PackJSON` is gone, Intents reads `Pack.PackBytes()`, a new accessor of `trustpack.Pack` that returns a copy of the pack file LoadPack read and decoded; an absent Pack is a named gap, a zero Pack is fatal (ErrPackData). The core-trust Record signing keys are read from `NOEMA_TRUST_KEY_DIR` and `NOEMA_TRUST_KEY_MAX_ACTIVE` through `signing.LoadRing` (identity core-trust), required with the other Recorder inputs; `decisionInputs.Appender` is gone (A548, A553, interim, relayed, not yet committed).
- Why: the Record signing keys are needed because decision records are signed on the Go side and the database verifies them (migrations 0012, 0019).
- Files outside the lane's allowlist: `trustpack/pack.go` (one field, one assignment) and new `trustpack/packbytes.go` and its test: there is no way to derive the bytes from a Pack without it.
- Not built: the A567 consumer of `trust_limits.json` (blocked on noema#910: no Limits accessor on main).
- Hand mutation check: 8 mutants of the new code, 8 killed.
- Tests added (INV-16, INV-29, REQ-TRU-008, REQ-SEC-005): `packbytes_test.go`, the key tests of `wire_decisions_recorder_test.go`, the one-source tests of `wire_decisions_intents_test.go`.

## Pre-work for the next agent
- Exact next step: lane 1's `LoadVerified` goes through `LoadPack`, so its Pack carries the verified bytes; check that after both merge. Deploy must mount `core-trust.ed25519.<n>.key` (mode 0400, owner 65532).
- Open questions (EL): the Record write login and the sealed-facts source stay gaps.
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`, `deploy/`.
