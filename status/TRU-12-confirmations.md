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

# Session 2026-11-19 18:00 WAT · TRU-12 (lane 1a: conform to the merged port of noema#757)

State: red pushed (b7a89138); green committed locally, not pushed (A332, A340).

## Session summary
- What changed (PR links): noema#750. `ConfirmationPath.Status(ctx, a, f intake.FreshBinding)` is PURE and answers NotRequired, Pending (RequestID of the open request, or NewRequest{Requirement, TTL}), Given (EventID, PlanHash, RequestID; only confirm.Proceed against the stored six values plus f; an error when f is empty on an accept) or Declined (RequestID). Expired, BindingChanged and PolicyChanged answer Pending with a NewRequest. Deleted: `Evaluate`, the typed result and states, `ErrDeclined`, the Planner, FreshBinding, Resolutions and Open/Expire ports, and the package `trustpg2` (its resolution insert is superseded by `trust/recorder.RecordConfirmation`). `ConfirmationStore` is read-only (`Latest`). New: `NewConfirmationRequest` and `NewRequestFor` (the hand-over).
- Why: the founder order of 19 Nov (A341, interim, relayed, not yet committed), item 1.
- Tests (REQ-TRU-009, REQ-AMB-004; INV-12, INV-17, INV-25): all status tests rewritten to the pure contract; the 600-iteration property test now varies f (including empty) against confirm.Resolve. Hand mutation: 14 of 14 killed.
- Debt entries: none. Conflicts raised: none.

## Hand-over for the Recorder (lane 5)
- Pending with NewRequest != nil: persist, in the transaction of the CONFIRM Decision Record, a request built with `trustport.NewRequestFor(a, spec)` (tenant, correlation, intent id/version/hash, RequesterID = admitted PrincipalID, Requirement, TTL; expiry = Recorder clock + TTL). Supersede the intent's latest request (the adapter cannot name it: `intake.Confirmation` has no field for it; look it up by tenant, principal, intent, version). Append CONFIRMATION_REQUESTED. The six binding values (capability, target, params, plan, evidence-set hash, policy version) are not in the spec: the Recorder supplies them.
- Given: whoever spends it inserts the immutable `confirmation_resolution` row with `recorder.RecordConfirmation` (EventID, RequestID from the Confirmation; BindingHash, AcceptedAt, ExpiresAt from the stored request and answer, which the Recorder reads).
- Not done here: expiry events (CONFIRMATION_EXPIRED) are no longer appended by the adapter; the Recorder or the store must own them. Plan/params/target drift is no longer detected in Status (it has no plan); the writer's check of ConfirmationPlanHash against the ALLOW steps is the guard.

## Pre-work for the next agent
- Exact next step: the Postgres `ConfirmationStore.Latest` reader and the `ConfirmationPolicy` over the pack; the Recorder side above.
- Do not touch: trust/**, migrations.

# Session 2026-11-19 WAT · TRU-12 (lane 3c)

## Session summary
- What changed (PR links): noema#750. The adapter takes an `intake.Planner` and asks it for the plan hash (A369) on every non-NotRequired path; the request is derived from the AdmittedIntent as T14 does (WriteRequired true). The hash compared with the stored request's is the Planner's, so a drifted plan supersedes (Pending with a fresh NewRequest). `NewRequestFor` is now a method that asks the Planner and carries `PlanHash`. A Planner error (ErrPlanInvalid included) is returned wrapped, never Given or Pending. An open unanswered request is also superseded when plan, evidence hash or policy differ from the fresh values.
- A365: `snapshot.SetHash` hashes (evidence_id, payload_hash) pairs and payload_hash covers the canonical payload, so the set hash is a content hash; no gap found. Test added at the adapter boundary.
- Tests added (REQ-TRU-009, REQ-AMB-004; INV-12, INV-25): planner asked with the T14 request, drift, planner error, empty hash, content-hash supersession.
- Debt entries: none. Conflicts raised: none.
- Safer readings taken: an empty Planner hash is ErrBadAnswer; a nil Planner is ErrConfig; the open-request drift check ignores empty fresh values (they only block a Given).

## Pre-work for the next agent
- Exact next step: the Recorder must persist `NewConfirmationRequest.PlanHash` in the request's binding; the composition passes the PlannerAdapter.
- Do not touch: trust/**, execution/**.
