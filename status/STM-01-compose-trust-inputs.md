# STM-01 · lane 2 of the G1 final composition: the trust inputs

State: red pushed (4 commits), greens held locally for EL to push. Built by an agent for the Engineering Lead (EL). Dated 2026-11-23 WAT. All data is SYNTHETIC.

Rulings used (all "interim, relayed, not yet committed"): A537 (the 19 components `/readyz` names are the G1 critical path; four lanes), A469 and A486 (a part whose adapters or settings are not built is a gap, never a quiet default), A475 (the Ownership nodes come only from `Pack.OwnershipKinds()` through `NodesFromKinds`), A330 (no setting has a code default), A332 (a silent ruling takes the safer reading and is listed).

# Session 2026-11-23 WAT · STM-01 (lane 2: Ownership, Writes, Evidence, Reconciler, Policies, Clocks)

## Session summary
- What changed: `cmd/core/compose_trustinputs.go` (`buildTrustInputs`, `trustInputsIn`, `trustInputs.fill`) and its tests. Nothing else changed: `compose.go`, `wire_trust.go`, `wire_trust_adapters.go` and `wire_understanding*.go` are untouched.
- Why: A537. The builder is the one call the final composition makes; it takes the verified pack, the per-tenant Trust pools and the Understanding parts as inputs (a consumer-side seam) and returns the ports it could build and the names of the gaps.
- Built (when every input is given): Ownership (one instance per served tenant behind a router, nodes from the pack only), Evidence (pack trust profiles, `SealedSnapshots`, the Reconciler's own facts), Policies (`LoadPolicyBook`, `StorePublications`, `Admissions`, `Facts` with the snapshot lists).
- Named gaps whatever is given: Writes, Reconciler, Clocks (reasons below).
- Safer readings taken: (1) A part absent is a gap, not an error; a value present and invalid (a pack file that does not decode, a timeout of zero, a served tenant with no login, overlapping list prefixes) is an error with fixed text and no pack content. (2) Policies is a gap while the list entities and their scope prefixes are not given: the intakesources README says "EL must agree" and no ruling gives them, and a Policies adapter with no lists could never evaluate the rules that read the case lists (INV-19). (3) `LoadPolicyBook` is given the current pack only: no older verified pack version exists on main, so an AT_ADMISSION rule admitted under an older policy version is read from the published history only. (4) The builder opens no pool and reads no environment: the pools, the tenants and the bound (`NOEMA_TRUST_DB_TIMEOUT_MS`) come from the caller.
- Why the three gaps: Writes: the pack's rule is `write_when`, a predicate over the intent's slot VALUES (A364), `intake.AdmittedIntent` carries only slot statuses, and the merged `WriteRule` is `MayWrite(node)` (noema#781, written before A364). A rule built over `MayWrite` would answer from the node alone. Reconciler: the pack carries neither the escalation reason a classification maps to nor the capabilities allowed under a freeze (reconciler README, "Pack data"). Clocks: the routing of a clock phase to an escalation reason has no source in the pack, the anchor events have no entity and field mapping, and `limits/trust_limits.json` (the at-risk fraction) is not listed in the manifest ("Not yet listed ... limits"), so no verified source exists.
- Tests added (REQ-STM-001, REQ-TRU-001 to REQ-TRU-007, REQ-CTX-003; INV-11, INV-12, INV-14, INV-16, INV-19; AGENT-13, AGENT-16): `compose_trustinputs_{ownership,evidence,policies,gaps}_test.go`, helpers in `compose_trustinputs_helpers_test.go`. Two DB-backed tests (Evidence and Policies over real logins) run in CI; they passed locally here.
- The existing guard `TestTheCompositionBuildsOwnershipNodesOnlyFromThePack` covers `compose_trustinputs.go` and passes.
- Regression cases added: none. Debt entries: none. Conflicts raised: none (the Writes gap is a mismatch between A364 and the merged `WriteRule`, reported to EL, not a conflict with an invariant).
- Hand mutation check: see the PR body.

## Pre-work for the next agent
- Exact next step: the final composition calls `buildTrustInputs` where the pack view is in hand (lane 1's source) and passes `in.fill(adapters)` into `TrustAdapters`; production leaves `OwnershipReader`, `ListEntities` and the Understanding parts to be filled as they land, so `/readyz` names `trust.Ownership`, `trust.Evidence` and `trust.Policies` until then.
- Open questions (EL): the Writes rule (see above: the port needs the stored intent's slot values); the Reconciler's routing data and the Clocks routes and anchor events (pack changes, AGENT-15); the list entities and their scope prefixes; whether to pin `limits/trust_limits.json` in the manifest.
- Files to read first: `services/core/cmd/core/compose_trustinputs.go`, `status/STM-01-writes-adapter.md`, `orchestrator/portadapters/trust/intakesources/README.md`.
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`, `wire_understanding_compose.go`.
