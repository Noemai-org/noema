# Session 2026-11-28 · LANE-5 · A723, A740 (C1, interim, relayed): SUBJECT and EPOCH as sources of a predicate variable

## Session summary
- What changed: `outcome/request` gains two source kinds, `FromSubject` (the intent's subject, `Facts.Subject` from the reader) and `FromEpoch` (the subject's current logical resolution epoch as a whole number), the `Epochs` port (`Current(ctx, tenant, subject)`) and an optional `GrantRequests.Epochs`. The port is asked once, for the tenant's subject, and only for an Outcome that binds EPOCH. A binding the inputs cannot supply (no subject, no epoch, a negative one, no port) is `ErrBinding`; a failing port gives no request. `Build` stays pure.
- Why: A723 and A740. The pack's `outcome_bindings` (#1026) bind `SUBJECT` and `EPOCH`, and the request builder had no source for either.
- Risks: nothing yet supplies `Facts.Subject` (the store reader) or an `Epochs` (an adapter over `evidence/epochport`), so a pack that binds them fails closed with `ErrBinding` until they are composed. Named, not guessed.
- Tests added (REQ-VER-001; INV-04, INV-12, INV-14): `epoch_test.go` (five). Regression cases: the red at `50ced20b8`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: (1) `outcome/store` reads the subject of the decision's intent (the pack's `subject_slot`) into `Facts.Subject`; (2) an `Epochs` adapter over `evidence/epochport`; (3) the composition maps the pack's `outcome_bindings` (`PLAN_PARAM:<p>`, `INTENT_ID`, `SUBJECT`, `EPOCH`) to `request.Source` and gives `GrantRequests.Epochs`.
- Do not touch: `outcome/verifier`.
