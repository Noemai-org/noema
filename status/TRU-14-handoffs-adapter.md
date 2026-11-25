# Session 2026-11-25 · TRU-14 (C1, A684: the Handoffs adapter over the pack's handoff routes)

## Task card
- Task: TRU-14 (A531: a first evaluation with nothing runnable is ESCALATE `NO_RUNNABLE_CAPABILITY` when the pack routes that reason to a handoff). A684 lists `Service.Handoffs` ("an intake.Handoffs adapter over the pack's handoff routes") among the named gaps of the G1 critical path.
- PERD REQ: REQ-TRU-004, REQ-AG-001. SoT: INV-14, INV-16, AGENT-16; A551 (the read-only pack accessor), A531, A684 (interim, relayed; recorded in `DEC-2026-11-25-architect-rulings-a673-a684`).
- Allowlist: `services/core/orchestrator/portadapters/trustpack/` (the adapter and the accessor), this note. No migration, no contract change, no new dependency.
- Plan: tests first (a stub that answers false), then `Pack.HandoffRoutes()` (a copy of the pack's routes, read at load) and `trustpack.Handoffs` implementing `intake.Handoffs`. The adapter lives in `trustpack`, not in `portadapters/trust`, because `trustpack` already holds the pack-backed intake adapters (Scope, Authority, Ownership) and `portadapters/trust` imports it, not the other way.
- Faults: no tenant, no node, no reason, a node the pack does not have, a cancelled context, the zero adapter, a nil pack, a pack that routes one reason twice to the same node, a route that names a node the pack lacks.
- Record events: none (an adapter that reads pack data).

## Session summary
- What changed (PR links): red pushed alone; the PR link is in the PR body.
- Why: the composition has no `Handoffs`, so T12's `noRunnable` ends UNKNOWN `errNoHandoff` for every first evaluation of a write that finds nothing runnable, and `/readyz` names `Service.Handoffs`.
- Tests added (REQ-TRU-004; INV-14, INV-16): `trustpack/handoffs_test.go`.

## Pre-work for the next agent
- Exact next step: EL sets `Service.Handoffs` in the composition from `trustpack.NewHandoffs(pack)` (the gap line `Service.Handoffs` in `cmd/core/wire_decisions.go` then flips to wired).
- Files to read first: `trust/intake/steps_t10_t12.go` (`noRunnable`), `trust/intake/ports_continuation.go` (`Handoffs`), `orchestrator/portadapters/trustpack/handoffs.go`.
- Do not touch: `packs/fintech_v1.json` (the route data is C2's).
