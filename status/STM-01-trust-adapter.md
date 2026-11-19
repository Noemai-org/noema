# STM-01 · the Trust adapter

State: in review (noema#710). Built by the Engineering Lead (EL). Draft until the red run is read.

# Session 2026-11-18 · STM-01 · the Trust adapter (EL)

## Session summary
- What changed (PR links): noema#710. `orchestrator/portadapters/trust`: `Port.Decide` over `intake.Service.Evaluate`, `Port.Bound` over Trust's decision and grant records, `Dispatches` (the intake's dispatch count) with one limit for every capability.
- Why: noema#572, #661 and #663 merged; the Founder's order of 18 Nov (A306, interim, relayed, not yet committed): the Dispatches adapter must answer a limit of at least 1 for read capabilities.
- Risks, and how they are contained: `Decide` takes the correlation from the stored intent, because `orchestrator.Trust.Decide` carries none; the intake's T1 check of the request's correlation against the intent's is therefore the intent's own. The orchestrator's own port is unchanged. Contained: the intent is read as the session's tenant, and `Bound` checks the correlation of the grant against the turn's. Raised for the Architect on the PR.
- Tests added (REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, AGENT-08): request built from the session and the stored intent; the six outcomes and `OutcomeOf` for all seven; an ALLOW without a decision ID; a read has no grants; each refusal of `Decide`; each mismatch of `Bound`; a store failure is not a refusal; the dispatch limit is at least 1 for a read and a write capability, never invented. 16 hand mutants, all killed.
- Regression cases added: the A306 case (`TestDispatchesAnswerALimitOfAtLeastOneForEveryCapability`).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the pgx implementations of `Intents`, `Records` and `KeyDispatches` (a read of the decision and grant tables as the tenant, and a count of GRANT_SPENT by idempotency key from the Record; both need pgsodium, CI only), then plug the three into `buildComponents` (`cmd/core/compose.go`), with the intake's other ports.
- Open questions (owner): whether `Trust.Decide` should carry the turn's correlation (Architect).
- Files to read first: `orchestrator/portadapters/trust/port.go`, `trust/intake/ports_t13_t15.go`.
- Do not touch: `trust/**` (C1's).
