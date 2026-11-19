# Session 2026-11-19 12:00 WAT · TRU-12 (confirmations adapter, T13)

State: red pushed; green committed locally, not pushed (A332, A340).

## Session summary
- What changed (PR links): new `services/core/orchestrator/portadapters/trust/confirmations.go` and `confirmations_test.go`: `intake.Confirmations` over `trust/confirm` (Resolve, BindingHash).
- Why: T13 (REQ-TRU-009, REQ-AMB-004; INV-17, INV-25, INV-12), A329.
- Risks, and how they are contained: the store and the profile are consumer-defined interfaces; a store or profile failure is an error, never Pending or Given. No Postgres reader in this lane: the composition fills `ConfirmationReader`.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-009, INV-25, INV-12 (see confirmations_test.go).
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: write the `ConfirmationReader` over the `confirmation_resolution` table and the request/event store, and the `ConfirmationProfile` over the pack; call `NewConfirmations` from the composition.
- Open questions (owner): where the request and event rows (Request, Event, approver roles) are stored; `confirmation_resolution` holds only the accepted binding hash (EL).
- Files to read first: trust/confirm/confirm.go, trust/recorder/confirmation.go, trust/intake/ports_t13_t15.go.
- Do not touch: trust/**, port.go, dispatches.go.

# Session 2026-11-19 15:00 WAT · TRU-12 (the FULL confirmation path, T13)

State: red pushed (d976a228); green committed locally, not pushed (A332, A340).

## Session summary
- What changed (PR links): noema#750. `confirmations.go` is replaced by `ConfirmationPath` (typed `Evaluate`, intake-facing `Status`) over consumer ports (`ConfirmationPolicy`, `intake.Planner`, `FreshBinding`, `ConfirmationStore`, `ConfirmationResolutions`); new package `portadapters/trustpg2` holds the Postgres store of `confirmation_resolution` (insert once, conflict on a differing row; Trust login, forced RLS).
- Why: the founder's decision of 19 Nov (interim, relayed, not yet committed): all seven resolutions of trust/confirm; no second request while one is open; fresh request on Expired, BindingChanged, PolicyChanged.
- Risks, and how they are contained: Given only from confirm.Proceed (INV-25), pinned by a seeded property test; every dependency failure is an error. DB tests ran against the local PostgreSQL here and run in CI.
- Tests added: REQ-TRU-009; INV-12, INV-17, INV-25 (confirmations_test.go, resolutions_test.go).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none. GAPS (see PR body): no store for requests and approver answers; the intake has no Declined status and no RequestID on Pending.

## Pre-work for the next agent
- Exact next step: the Postgres `ConfirmationStore` (requests, answers, expiry) needs a table or a decided Record-event payload; then the route, `confirmation-answer.v1` (C1).
- Open questions (owner): where request rows, approver roles and answers live (EL/C1); a Declined status and a RequestID on `intake.Confirmation` (C1).
- Files to read first: orchestrator/portadapters/trust/confirmations.go, trustpg2/resolutions.go.
- Do not touch: trust/**, migrations.
