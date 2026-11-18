# Status · TRU adapters · the production `Policies` (T11) and `Clocks` (T12) ports of the Trust intake (Core Engineer 1, prepared locally)

Updated 2026-11-18. The user's order of 18 Nov 2026 (rulings A329 to A340 relayed and not committed; A306 and A319 committed in `docs`). Follows noema#572 (A306: the intake asks the `Policies` port once for each capability of the authorized set) and the Dispatches adapter of noema#710. Tier 1 (Trust is a P0 path).

## State: five local commits on `prep/adapters-policies-clocks`; not pushed, no PR

Package: `services/core/trust/intakeadapters` (a sibling under `trust/`; its README says how it works). One small addition to `trust/policystore` (`Published`). No edit to `cmd/core` (`wire_trust.go` is EL's lane, A313).

# Session 2026-11-18 · TRU adapters · Policies and Clocks

## Session summary
- What changed (PR links): none yet. `trust/policystore.Published` (every version a tenant published, oldest first, with the database's publication time in whole Unix seconds); `trust/intakeadapters`: `LoadPolicyBook`, `StorePublications`, `NewPolicies` (the `intake.Policies` adapter), `LoadClockBook`, `NewClocks` (the `intake.Clocks` adapter), and the consumer-defined ports `FactSource`, `Admissions` and `Anchors`.
- Why: T11 and T12 had a port and no production adapter; `cmd/core` cannot wire Trust without them.
- Risks, and how they are contained: it adds no allow path. Every failure (a rule error, a published version with no document, a history that cannot be ordered, a missing or mistyped fact, an unusable admission time, a WAIT with no usable delay, an anchor the source cannot give, an unusable configuration) is an error and no answer, which the intake turns into UNKNOWN `INTERNAL_ERROR`. The adapters hold no number and no industry term (lint: `threshold-literal`, `forbidden-term` clean). Two fail-closed choices are Architect questions below (Q3, Q6).
- Tests added (REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007, REQ-SEC-004; A306, A205 Q1, A225; INV-03, INV-09, INV-14, INV-16, INV-19, INV-21): `policystore/published_test.go` (6), `intakeadapters/book_test.go` (5), `policies_test.go` (19), `clocks_test.go` (9). Real pack (`packs/fintech_v1.json`), real engine, real `clocks.Evaluate`, real PostgreSQL read as each tenant's own Trust login (two tenants). Red first: `a1eed755` (4 of 5 failed, the fifth is a guard), `a0025165` (all 22 failed: the adapter is not built), `a7509b82` (all 9 failed), each seen failing before its green commit.
- Regression cases added: the same tests. Mutation checks (mutate, run, restore, `git diff` empty before each commit): `Published` 8 mutants (7 killed, 1 survivor killed by a test added in the green commit); Policies 54 mutants, 51 killed, 3 equivalent (the kind and presence checks of `wait_seconds` are redundant with the at-least-one check; a pack with no `policy` key is refused by `policy.Parse` with the same error); Clocks 37 mutants, 33 killed, 4 equivalent (the label check and the three key-presence checks of the loader are redundant with the contract's own refusal and the JSON decoder's, with the same `ErrPack`). Survivors of first runs were closed by tests, not by weakening any.
- Review triggers (AGENTS section 7): `policies_test.go` is 492 lines (a table-driven suite over one adapter; one file for one behaviour); `LoadClockBook` is 61 lines (one decode-and-check pass over three pack keys). No other file over 400 lines or function over 60.
- Debt entries: none opened. DEBT-23 (the policy document store) is what Q4 depends on.
- Conflicts raised: none. Questions below are for the Architect; the safer reading is applied.

## What is built, and what is not (and why)

| Part | State |
|---|---|
| `policystore.Published` | Built. |
| `PolicyBook` (pack policy as data, vocabulary from the pack, `FactNeed` per target) | Built. One policy per book. |
| `Policies` over the real engine and the real publication store | Built: version in force at the reading, D8 for AT_ADMISSION, rules and versions recorded, default deny, rule error is an error, `ReevaluateAt` from `wait_seconds`, per-capability query, tenant isolation. |
| `StorePublications` (production `Publications`) | Built; the composition root supplies the per-tenant Trust logins (`For(tenantID)`). |
| `ClockBook`, `Clocks.Applicable`, `Clocks.Route` | Built as adapters over configuration and a port. |
| **`FactSource` production implementation** | **NOT built: stop condition.** See S1. |
| **`Admissions` production implementation** (`admitted_at`) | **NOT built.** See S2. |
| **`Anchors` production implementation** | **NOT built.** See S3. |
| **The at-risk fraction and the clock routing as pack data** | **NOT in the pack; the adapter takes them as configuration.** See S4. |
| Several policies in one book; older versions without a pack document | Not built. See Q4 and Q5. |

## Stop points: exactly what is missing

**S1. The facts the shipped policy reads have no source** (the instruction: if conditions need facts no source provides, stop on that point and report). `packs/fintech_v1.json`'s policy `fin.pol@3.1` reads, per target (the adapter derives this as `FactNeed`; `TestTheShippedPackLoadsAsAPolicyBookAndNamesTheFactsEachCapabilityReads` pins it):
- `fin.cap.create_escalation_case`: variables `subject_resource` (Reference), `subject_resource.status` (Enum RESOLVED, UNRESOLVED, AMBIGUOUS, ABSENT), `logical_resolution_epoch` (Integer); entity `case` (fields `id`, `subject`, `status`, `created_by_intent`, `owner_queue`, `epoch`), the rows of the case system, to evaluate `exists(case where case.subject == subject_resource and case.epoch == logical_resolution_epoch and case.status in ['OPEN','ROUTED'])`.
- `fin.cap.record_investigation_evidence`: `subject_resource.status`.
- `fin.cap.route_to_internal_owner`: `case_id` (Reference).
- every other capability of the pack (reads, the complaint, the prohibited ones): nothing.

What exists in the repository and what does not: the target (`AdmittedIntent.TargetRef`) and the slot statuses (`IntentView.RequiredSlots`) come from `intake.Intents.Get`, so `subject_resource` and `subject_resource.status` can be built from the stored intent (no new store, but a consumer must be written and the mapping slot to status is not stated by the pack). **`logical_resolution_epoch` has no reader**: it is a key field the planner gives a step (`execution/controller` `KeyFields`), PERD 27.2 says it changes only when the prior case is verified closed and a new intent is admitted, and no table or port holds it for Trust at T11. **`case_id` has no meaning at T11**: the plan (and so the case it routes to) does not exist until T14, and for a first write there is no case; the pack's `permit_routing` (`case_id != null`) cannot be evaluated by a source that exists. **The `case` entity has no reader**: `packs/fintech/evidence_mapping/mapping.v1.json` maps sources to the reconciliation rule set's rows and has no `case` mapping; nothing builds the policy's `case` rows from the case-system evidence in the sealed snapshot. Until the pack owner and the Architect say where each comes from, `FactSource` has no production implementation and the `Policies` adapter cannot be wired for a write capability whose rules read facts (a read capability needs none). Absence is never treated as "no case" (INV-19): the adapter refuses a fact the rules read that the source did not give.

**S2. `admitted_at`.** The intent contract (`intent-object.v1` to `v3`) has no `admitted_at`; the nearest stored value is `admitted_intent.created_at` (database clock, `trust_rw` may SELECT it under row security) and the `INTENT_ADMITTED` Record event time. Which of them is `admitted_at`, and whether it is the evaluated version's or the first version's, is undefined; PERD 23.4 says "the version in force at `intent.admitted_at`". The port asks for the evaluated version's (`Admissions.AdmittedAt(tenant, intent, version)`); the shipped pack has no AT_ADMISSION rule, so nothing depends on it today.

**S3. Clock anchors.** The pack's clock names an `anchor_event` (`complaint_logged`) and nothing maps an event to a source, an entity and a field. The snapshot's case-system evidence (`complaint_ref`: `subject`, `epoch`; the item's `effective_at`) and the Record are candidates; the choice is not mine. `Anchors.AnchorTime(tenant, snapshot, event) (at, happened, err)`: `happened=false` is NOT_STARTED; an error is never NOT_STARTED.

**S4. Pack data the order assumed that does not exist.** (a) The at-risk fraction: `clock.v1.json` has no field for it and nothing else in the pack holds it (ruling A205 Q1 gives it as provisional pack data). `NewClocks` takes `AtRisk` and refuses a zero denominator, a negative numerator and a fraction above one; it does not default. (b) The routing of clock statuses: the pack has `routing_allowlist` (four queue names) and `reason_catalogue` codes produced by `CLOCK_MAPPING` (`fin.reason.sla_at_risk`, `fin.reason.sla_breached`), but no rule that says which phase escalates. Reading the two codes as AT_RISK and BREACHED would be a guess, so `NewClocks` takes `Routes map[clocks.Phase]string` (phase to pack reason code, validated with `decide.Pack`) and with none routes nothing: a breached clock is then escalated by the intake itself (`CLOCK_BREACHED_UNROUTED`, A225), which is the safer direction. **Until the pack gives (a), `cmd/core` cannot construct the `Clocks` adapter** (`ErrConfig`), so T12 stays unwired.

## Questions for the Architect (the safer reading is applied)
- Q1 **The version in force at the reading** is the newest published at or before it (`published_at`, the database clock, as `effective_from`; the store has no other date). `policystore.Active` (what Execution's binder reads at E4) is the newest published regardless of time. They agree unless the intake's clock reading is earlier than a publication (a SYNTHETIC clock that runs behind the database): then no version is in force and every query is REJECT `NO_PERMITTING_RULE` (fail closed), and an older version could apply to a decision made before a newer one was published. Confirm that `published_at` is the policy's effective time, and that the intake's clock and the database's are meant to be one clock.
- Q2 **Rules recorded**: every rule that matched (strongest first), with its policy and version, not only the winner. The port says "the rules that decided"; `decide.PolicyRuleVersion` says "took part in the decision"; a PERMIT that matched under a winning REJECT, and the CONFIRM and the PERMIT of a CONFIRM, are on the Record. Confirm.
- Q3 **Intent-node targets.** The engine takes one target. A capability query evaluates the capability; a query with no capability evaluates the intent node. A rule whose `applies_to` names an intent node is **refused when the pack loads** (the vocabulary's targets are the pack's capabilities only), because evaluating it for a capability query would silently skip it. EL's wiring note says "rules whose `applies_to` names that capability or the intent node"; supporting both needs an engine change (not mine: `trust/policy` is not on the allowlist) or a ruling that node rules combine like capabilities (most restrictive, A319).
- Q4 **No document store (DEBT-23).** The documents are the pack's `policy`. A tenant that published a version the running pack has no document for is `ErrNoDocument` for every query (the newest, and the one in force at admission, must have a document). The composition root should load every supported pack version into one book (`LoadPolicyBook(current, older...)`).
- Q5 One policy per book (the pack has one). Several policies, combined most restrictive wins (A319), are not built.
- Q6 **Two versions published in the same second, or a newer version with an earlier time** (the guard orders by version, `published_at` is the transaction start): the history cannot be placed, `ErrPublications`, for that policy until a later version. Rare, and loud. Alternative: a distinct `effective_from` column (a migration).
- Q7 A tenant with no publication is REJECT `NO_PERMITTING_RULE` with no rule and no version (default deny), not an error.
- Q8 `ReevaluateAt` = the reading plus the winning WAIT rule's `wait_seconds` (a whole number of at least 1); not measured from the admission.

## Wiring for EL's `cmd/core` (what `main.go` must pass)
- `intakeadapters.LoadPolicyBook(currentPackBytes, olderPackBytes...)`: the pack file(s) read by `main`.
- `intakeadapters.StorePublications{DBs}`: `DBs.For(tenantID)` returns that tenant's own `trust_rw` pool (the same per-tenant login files as the Record reader and the Recorder; never another tenant's, never a default).
- `intakeadapters.NewPolicies(PoliciesConfig{Book, Publications, Admissions, Facts})`: **`Admissions` and `Facts` have no production implementation (S1, S2).**
- `intakeadapters.LoadClockBook(packBytes)` and `intakeadapters.NewClocks(ClocksConfig{Book, Anchors, AtRisk, Routes})`: **`Anchors`, `AtRisk` and `Routes` have no source yet (S3, S4).**
- Every call needs a context with a deadline (`policystore` refuses one without).

## Pre-work for the next agent
- Exact next step: the Architect answers S1 to S4 and Q1 to Q8; the pack owner adds the at-risk fraction, the clock routing and the anchor mapping to the pack (and the policy's fact sources); then build the three ports' production implementations and push this branch (task: TRU adapters; REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007; Tier 1: Codex, SR and one independent role review; the PR body needs the plan, the SoT 52 questions and the checklist).
- Open questions (owner): S1 to S4 (Architect, pack owner); Q1 to Q8 (Architect); the wiring (EL).
- Files to read first: `trust/intakeadapters/README.md`, `policies.go`, `clocks.go`, `facts.go`, this file.
- Do not touch: `trust/policy`, `trust/clocks`, `trust/intake` (the ports), `cmd/core/wire_trust.go`.

## Messages
- 2026-11-18 · the user (relayed order) → Core Engineer 1 (this session): build the production `Policies` and `Clocks` adapters; stop and report on a missing fact source. Receiver used it for: this branch. Evidence: the five commits of `prep/adapters-policies-clocks`. Rulings A329 to A340 are relayed and not committed; nothing here cites one as authority.
