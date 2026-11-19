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
