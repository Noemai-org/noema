# STM-01-composition-main

State: red pushed (4 commits), greens held locally for EL. Built by an agent for the Engineering Lead (EL). Dated 2026-11-24 WAT. All data is SYNTHETIC. This is the part of the composition PR that does not need the events route; the rest waits for noema#907 (A643).

# Session 2026-11-24 WAT · STM-01 (composition: everything that does not need the route)

## Session summary
- What changed (PR links): none yet (EL opens the PR). New `services/core/cmd/core/compose_trust_wiring.go` (`wireTrustInputs`, `memoPackSource`, `planContextsFromRequest`), `compose_listentities.go` (`listEntitiesFromDescriptors`), `services/core/orchestrator/portadapters/trustpack/accessors.go` (`ConfirmationRequirements`, `Registry`); small edits to `compose.go` (two `composeDeps` fields, one block in the unbuilt-Trust branch, `productionDeps`), `wire_trust.go` (`buildTrustOver`, the part of `buildTrust` that needs no pool opened) and `wire_decisions.go` (two stale gap texts, two parts that exist now).
- Why: A643, A550, A551, A612, A545 (interim, relayed, not yet committed): compose builds what main already has, from main only.
- Built: the Trust inputs and the decisions are built over the ONE manifest-verified pack (memoised pack source, so it is loaded and verified once) and the ONE set of Trust logins (opened once, shared by the wiring and by `buildTrustOver`); Intents is built in production; Ownership, Evidence, Policies are built when their inputs exist; ListEntities is derived (A545); the plan context of a plan request; two pack accessors.
- Named gaps (fixed text, no value; pinned by tests): Writes (noema#935), Candidates (no candidates read, no labeller adapter), Ownership (no enterprise read), Evidence and Policies in production (no injection screen, correlation source or alert: the injection patterns are not manifest-pinned), Reconciler, Clocks (A612: noema#910), Confirmations (no read by request id, no Record events of the store, expiry not in the pack), Planner (no `plangraphs.Pack`: the pack names no graph of an intent node; no `plangraphs.Intents`), Recorder (Record write login, sealed facts), the confirmation route.
- Risks, and how they are contained: a gap never builds a quiet adapter; a present invalid value stops the process and names the setting only; the pack is read once.
- Tests added (REQ-STM-001, REQ-TRU-009, REQ-INT-001; INV-12, INV-14, INV-16): `compose_trust_wiring_test.go`, `compose_listentities_test.go`, `compose_plancontext_test.go`, `trustpack/accessors_test.go`.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.
- Hand mutation check: 31 mutants, all killed in the end (4 tests were added after it, as separate commits).

## Pre-work for the next agent
- Exact next step: when noema#907 merges, branch the route part off main and add the events route, gateway, recovery and serve calls to `compose.go` and `main.go` (A643). Then: merge #935 (Writes rule) and plug it; the stale comment in `portadapters/trust/writes.go` is untouched because #935 is not on main.
- Open questions (EL): see the PR body, "Stopped".
- Files to read first: `compose_trust_wiring.go`, `compose.go` (the unbuilt-Trust branch).
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`, `deploy/`, the events route and gateway files of #907.
