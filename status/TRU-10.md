# Status · TRU-10 (Disposition and action decision linkage)

State: built locally on `prep/tru10-linkage` (two commits, not pushed, no PR). Owner C1 in tasks.csv; prepared by Core Engineer 1. Needs TRU-05: Done. Done-when: "Property test: no action decision without a linked disposition".

# Session 2026-11-18 · TRU-10

## Session summary
- What changed: `trust/recorder/recorder.go` (`EscalationSet`, `Writer.Escalation`, `ErrDispositionNotValid`, `ErrStepOutsideEscalation`, two checks in `Write`), `doc.go`, `trust/README.md`, new `trust/recorder/escalation_link_test.go`. No migration, no contract, no pack change, no threshold.
- Why: REQ-TRU-010, INV-25; PERD 23.1; ruling A297 (DEC-2026-11-17-architect-rulings-a296-a305, interim): "PERD 23.1 (adopted) defines it. An action decision is the per-write-step decision. An ESCALATE disposition authorises only the escalation plan's write steps (create case, record evidence, route). The set comes from the pack's escalation plan."
- What it does:
  1. A decision that names a disposition (`Request.DispositionDecisionID`) must name an existing ESCALATE decision of the same tenant. Read in the caller's transaction before anything is written. Missing, another tenant's and another outcome are one answer, `ErrDispositionNotValid`. The foreign key of migration 0012 already refused these; its failure (23503, `decision_record_disposition_fkey`) now maps to the same typed error as a backstop.
  2. Every step of such a decision must carry a capability in `Writer.Escalation`, else `ErrStepOutsideEscalation`. The set is injected by the composition root, as `NewFreezeGate` takes its list; the recorder holds no capability ID (INV-14, SoT 51). The zero set allows no step (fail closed).
  3. Refusals happen before the lock, the DECISION_MADE append and every insert: no row and no Record event. A retry of a stored input (A186) still returns the stored decision before the disposition check.
- Not built (stated): the production adapter that derives the set from the pack's escalation plan. In this repository the escalation plan is `packs/fintech/graph/g2_escalate.json` (graph `fin.graph.escalate`, three WRITE nodes: create case, record evidence, route), but no pack field says "this graph is the escalation plan" (no purpose or kind field in `action-graph.v2` or `pack.v1`). The adapter needs that designation, which is a pack or contract decision (AGENT-15) and is not made here. The tests read g2 as data to build the set, which is the only place a graph is named.
- Risks, and how they are contained: a Writer built without `Escalation` refuses every step that names a disposition (fail closed), so the production wiring must set it. No production caller of `Writer.Write` exists yet (status/TRU-05.md), so nothing in the repository is affected.
- Tests added (REQ-TRU-010, INV-25, REQ-SEC-004), all on real PostgreSQL, in `escalation_link_test.go`:
  - (SUPERSEDED on 18 Nov by A317-A328, see the last section) a decision naming no disposition was written whatever its steps;
  - each step of the escalation plan follows an ESCALATE disposition; the record, the column and the Record show the link; a REJECT of a step links too;
  - a step outside the set under a disposition (alone, mixed, first) is refused and the rows are unchanged;
  - the writer follows the injected set, and the empty set refuses every step;
  - an unknown, another tenant's, a same-tenant ALLOW and a same-tenant REJECT disposition are each refused, for an ALLOW with a step and for a decision with none, rows unchanged;
  - the same with the foreign key off (the recorder's own check), and the tenant check with a connection that sees every tenant;
  - the property `TestPropertyNoActionDecisionWithoutALinkedDisposition` (rapid, 100 checks): over generated dispositions (none, valid, unknown, another tenant's, own ALLOW, own REJECT), outcomes and up to three steps from the pack's three escalation capabilities plus two outside: a decision naming a disposition is written only if it is an existing ESCALATE decision of the tenant, no grant is issued under a disposition for a capability outside the set, a refusal changes no row or event, a decision naming none is written; and, over all rows written, every grant of a linked decision is for a set capability under an ESCALATE disposition of the same tenant.
  - Seen failing first at commit cdf766ad (refusals wrote rows; the foreign key's raw error came back).
- Mutants (7, each run against the new tests only; all caught): M1 drop the disposition existence check (caught by the foreign-key-off test and the all-tenants-connection test); M2 accept another tenant's disposition, by dropping the tenant predicate (caught only by the all-tenants-connection test: under the tenant login row security already hides the other tenant's row, so without that test the mutant is equivalent; the kill is that the mutant gets past the check and then fails at the append instead of returning `ErrDispositionNotValid`); M3 skip the ESCALATE step restriction (3 tests); M4 hard-coded three-capability list instead of the injected set (the injected-set test); M5 check after the write, at the end of `Write` (the two foreign-key-off and all-tenants tests, which see the rows written); M6 drop the outcome check, accepting a non-ESCALATE disposition (the foreign-key-off test); M7 empty set allows every step (the fail-closed test). The foreign-key backstop mapping (23503 to `ErrDispositionNotValid`) is not reachable while the recorder's own check runs first, so it has no test of its own.
- Regression cases added: the tests above. Debt entries: none. Conflicts raised: none (the readings below are open points, not conflicts).

## Readings settled, and the open one for the Architect
- **Action decision** (settled, PERD 23.1): "For each write step in the escalation plan ... an action decision evaluated separately under the capability's trust profile. Action decisions reference `disposition_decision_id`." REQ-TRU-010: "Each step is evaluated as its own action decision, and the action decisions reference the disposition." In the recorder an action decision is a decision that names a disposition. Its steps are the escalation plan's writes.
- **ESCALATE authorises only the escalation plan** (settled, ruling A297 and INV-25).
- **(CLOSED on 18 Nov by A317-A328, see the last section) Does a non-escalation ALLOW need a disposition?** PERD 23.1 and REQ-TRU-010 say that the steps of an escalation plan reference their disposition. They do not say that every ALLOW with a write step must. The pack has a write outside the escalation graph (`fin.cap.log_complaint_reference`, `g3_complaint.json`) with its own policy rules (`permit_complaint`, `complaint_confirm`) and a bridge mapping. Requiring a disposition for every write would make it unwritable, because the database allows only an ESCALATE disposition and under it only the escalation plan's capabilities. The narrower reading is implemented: no disposition required, and a decision that names none is unchanged. If the Architect rules that every write must link, the change is one condition in `check` plus new tests, and the complaint write needs a ruling of its own.
- **OPEN: same investigation or intent.** The disposition is "for the investigation intent" (PERD 23.1) and each step has its own decision; the two-node intent model gives them different intents. The recorder does not compare `intent_id` or `correlation_id`. A rule would need to say which field links them.
- **OPEN: which graph is "the escalation plan".** See "Not built". The Steward or Architect to name the designation (a pack field, or a deployment setting).

## Pre-work for the next agent
- Exact next step: the production adapter that builds `recorder.EscalationSet` from the pack once the designation exists; wire it where `recorder.Writer` is built (the `orchestrator.Trust` adapter, status/TRU-05.md). Test: the adapter's set equals the capabilities of the escalation graph's WRITE nodes.
- Open questions (owner): the three above (Architect).
- Files to read first: `services/core/trust/recorder/recorder.go` (`check`, `requireEscalationDisposition`), `escalation_link_test.go`, `services/core/migrations/0012_decision_record.sql`.
- Do not touch: `Write`'s order (Record first on a miss; nothing appended on a hit); migration 0012.

# Session 2026-11-18 · TRU-10 · every grant-issuing action decision links a disposition (A317-A328, relayed, interim)

## Session summary
- What changed (noema#713): `Writer.Write` now refuses a decision that carries steps (it issues grants) and names no disposition: `ErrDispositionRequired`, checked in `check()` before anything is written. A decision with no step still needs none. The disposition must still be an existing same-tenant ESCALATE decision (`ErrDispositionNotValid`) and every step must be in the injected `Writer.Escalation` set (`ErrStepOutsideEscalation`). This replaces the narrow reading of the first round.
- Tests (REQ-TRU-010; INV-25): red first at `8514de9c` (only the two new-behaviour tests failed: `TestADecisionWithStepsAndNoDispositionIsRefusedAndNothingIsWritten` and the rapid property, which now also requires that no grant belongs to a decision naming no disposition); `TestADecisionWithNoStepsNeedsNoDisposition` covers an ALLOW of a disclosure, a REJECT and a WAIT. The existing recorder tests link a seeded SYNTHETIC ESCALATE decision (`dec_SEEDDISP`, inserted directly, no Record event, no grant) when they give steps; their decision-record counts exclude it (`recordsQ`); no expected number changed.
- Mutants killed (against the new tests): N1 drop the new check; N3 check after the write; N4 require only for more than one step; N5 require for any ALLOW; M1 drop the existence check; M2 another tenant's disposition; M3 skip the ESCALATE restriction; M4 hard-coded capability list; M5 disposition check after the write; M6 drop the outcome check (killed by `TestTheRecorderRefusesAnInvalidDispositionOnItsOwnWithTheForeignKeyOff`); M7 an empty set allows every step (killed by `TestTheWriterUsesTheEscalationSetItIsGivenAndFailsClosedWithNone`). M6 and M7 were re-run by C1 after the agent's run was cut short.
- Verified: `go test -count=1 ./trust/...` ok; `gofmt`, `go vet ./trust/...`, `python3 tools/lint/run.py` clean.

## Consequence for the Architect
A write outside the escalation plan can no longer be recorded with grants through `Writer.Write`: `fin.cap.log_complaint_reference` (`g3_complaint.json`) is the pack's example. It would need an ESCALATE disposition and a step in the escalation set; it is in neither, and the database allows only an ESCALATE disposition, so there is no valid way to link it. No production caller of `Writer.Write` exists yet, so nothing is broken today; the complaint write needs its own ruling before one does.

## Still open
- The pack names no escalation graph, so the production adapter that builds the set is not built (a pack field is a contract change, AGENT-15, or a deployment setting).
- The disposition's `intent_id` and `correlation_id` are not compared with the action decision's.

## Pre-work for the next agent
- Exact next step: the Architect rules on the complaint write and the escalation-plan designation; then the production adapter and the composition-root wiring (EL).
- Do not touch: migration 0012.

# Session 2026-11-21 · TRU-10 (merge of main; a conflict with the production adapter)

## Session summary
- What changed (PR links): noema#733: main merged into the branch. One textual conflict (`trust/recorder/doc.go`): main's new confirmation paragraphs are kept, and this PR's three-rule disposition paragraph replaces main's older two-rule one. Tests that main added since this branch (`confirmation_state_test.go`, `confirmrequest_test.go`) counted every `decision_record` row and so counted the seeded disposition: they now use `recordsQ`, and the tenant-B ALLOW in `TestTheConfirmationStateKeyIsPerTenant` names tenant B's own ESCALATE disposition; the "ALLOW with a request" case names the seeded one so the request, not the missing disposition, is what is refused. Test-only; no production code changed by the merge. `go test ./trust/recorder/` passes.
- Why: the PR was dirty against main.
- Risks, and how they are contained: **a conflict with the production Recorder adapter (CONFLICT block on the PR).** When this PR was written no production caller of `Writer.Write` existed. `orchestrator/portadapters/trust.RecorderAdapter` (merged since) records the intake's ALLOW with steps and names no disposition, so the rule now refuses the adapter's main path: seven tests of that package fail with "a decision that issues grants must name its escalation disposition" (`TestRecordWritesTheDecisionTheGrantsAndTheirEventsTogether` and six more). I did not give those tests a disposition: that would pass them while the production path stays refused.
- Tests added / regression cases / debt: none new this session.
- Conflicts raised: yes (above). Also not from this PR: `cmd/core` tests do not build on main (`fatalConn` is declared in `wire_worker_ports_test.go` and `worker_run_fatal_test.go`).

## Pre-work for the next agent
- Exact next step: the Architect rules whether the intake's ALLOW with steps needs a disposition (A319's wording is for the per-step "action decision" of an escalation), or the rule is limited to the escalation's action decisions; then the adapter tests and this rule agree.
- Do not touch: `orchestrator/portadapters/trust/recorder_test.go` to name a disposition before that ruling.

# Session 2026-11-21 17:05 WAT · TRU-10 (A503: the Writer rule stays; DispositionDecisionID on the intake's RecordRequest)

## Session summary
- What changed (PR links): noema#733. Ruling A503 (interim, `DEC-2026-11-21-architect-rulings-a502-a506`) settles the CONFLICT block: PERD 23.1 and REQ-TRU-010 make an ESCALATE disposition its own Trust decision and each escalation write step its own action decision that references it, so the Writer's rule stays (a decision with escalation-set steps must link a disposition). The production path changes instead: `intake.RecordRequest` gains an optional `DispositionDecisionID`, which EL's escalation path fills after recording the ESCALATE disposition (no step, no grant) through the Recorder (A470). The intake itself records no escalation action decision, so it leaves the field empty.
- Contract check (A503, "C1 confirms"): `disposition_decision_id` is already in `decision-record.v1.json` and `v2` (an optional `common.v1` DecisionId), in the generated types and in migration 0012 (the column, the form check and the same-tenant ESCALATE foreign key). No minor bump and no migration note are needed.
- The real-writer e2e tests (`adapters/outcome_e2e/real_grant_test.go`) are main's again: they no longer seed a disposition (A503: not until the production path records one). **Two of them fail on this PR for that reason** (`TestAGrantTheRealWriterIssuedIsLoadedVerifiedAndExecutedByExecution`, `TestARetryOfTheSameInputReturnsTheStoredGrantStillSigned`: "a decision that issues grants must name its escalation disposition"). That is the rule working, not a defect; this PR cannot be green or merge until the escalation path records a disposition and those tests use it.
- Tests added (REQ-TRU-010; INV-25): `trust/intake/disposition_decision_id_test.go` (the field exists, is a string, is empty by default, and an ALLOW, a CONFIRM and a REJECT the intake records leave it empty). RED was pushed alone (`628e0c65`).
- Regression cases added: the same. Debt entries: none. Conflicts raised: the CONFLICT block is settled by A503.

## Pre-work for the next agent
- Exact next step: EL's escalation path records the ESCALATE disposition through the Recorder first, then passes its ID in `DispositionDecisionID` on each escalation action decision; then the e2e tests get their disposition from that path (never an INSERT) and this PR goes green. BO's expected Record events for US-06 list the disposition decision first (A503).
- Open questions (owner): the disposition's intent is not compared with the action decision's (still open, Architect); how the write outside the escalation plan (`fin.cap.log_complaint_reference`, `g3_complaint.json`) is recorded (Architect).
- Files to read first: `trust/recorder/recorder.go` (`ErrDispositionRequired`), `trust/intake/ports_t13_t15.go`, `adapters/outcome_e2e/real_grant_test.go`.
- Do not touch: `services/core/migrations/` (0012 already has the column).
# Session 2026-11-22 03:35 WAT · TRU-10 (C1, the small PR: DispositionDecisionID on the intake's RecordRequest and the adapter pass-through; A503, A507, A508)

## Session summary
- What changed (PR links): the optional `DispositionDecisionID` on `intake.RecordRequest` and its pass-through in `orchestrator/portadapters/trust.RecorderAdapter` to `recorder.Request` (A503: an ESCALATE disposition is its own Trust decision, recorded first through the Recorder; each escalation action decision links it). It is the small PR from main that A503 and the Founder asked for; noema#733 keeps the Writer rule and flips it afterwards, and EL adds the escalation path on #733's branch (A508).
- Why: until the adapter hands the Writer the ID, no production path can name a disposition, so the Writer's rule (#733) could not be satisfied by any caller.
- Risks, and how they are contained: none in behaviour for any caller that names no disposition (the field is empty and the Writer's rules are unchanged on main). The Writer still checks that a disposition it is given is an existing same-tenant ESCALATE decision.
- Tests added (REQ-TRU-010; INV-25): `trust/intake/disposition_decision_id_test.go` (the field exists, is a string, is empty by default, and an ALLOW, a CONFIRM and a REJECT the intake records leave it empty; it failed in CI on the earlier head of noema#733 before the field existed); `recorder_disposition_test.go` (the Writer gets the ID, or none; a decision with no steps passes it too; **the real path**: the disposition is recorded first through the adapter and the real Writer, an action decision that names it is stored with the link, and the chain verifies; no INSERT). `newRecWorld` in `recorder_test.go` is now `newRecWorldWith(t, escalation)` (the escalation plan the composition root injects, A335); `newRecWorld` keeps its behaviour.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: noema#733 flips the Writer rule (the escalation path of EL passes the ID) and refuses, before any transaction opens, a decision with steps and no disposition ID at the adapter; its tests record their disposition through a real `Writer.Write`. A decline's `DeclinedRequestID` is not passed by the adapter yet (the follow-up of A507 does it, with the ownership check).
- Files to read first: `orchestrator/portadapters/trust/recorder.go`, `trust/recorder/recorder.go` (`ErrDispositionRequired`, `ErrDispositionNotValid`).
- Do not touch: `services/core/migrations/` (0012 has the column).

# Session 2026-11-22 05:10 WAT · TRU-10 (C1, US-06: disposition_decision_id in the DECISION facts of reconstruction.v3; A503)

## Session summary
- What changed (PR links): the Writer names the disposition in the DECISION_MADE payload (`disposition_decision_id`) when the decision names one; the reconstruction projector serves it as a DECISION fact (`record/reconstruct/project.go`: the allowed fact and its `dec_` shape); `reconstruction.v3.json` types it as a `common.v1` DecisionId in the DECISION facts (optional; v1 and v2 are not edited). Additive minor change in place of v3 (AGENT-15), migration note in `contracts/README.md`; the generated TypeScript type is regenerated; the v3 fixtures are generated (`tools/record_chain_ref_v3.py`), so the two new cases are in that script, not hand-written.
- Why: BO's US-06 expectations list the disposition decision first and then the action decision that links it; before this the link was only a column of the decision table, not in the Record events or the reconstruction.
- Risks, and how they are contained: a decision that names no disposition has no new member (tested); `disposition_decision_id` of another shape is refused by the schema fixture and masked by the projector like any `_id` of another shape; no migration.
- Tests added (REQ-TRU-010, REQ-AUD-003; INV-25, US-06): `trust/recorder/decision_made_disposition_test.go`; `record/reconstruct` `TestTheDecisionFactsNameTheDispositionTheDecisionLinks` (disposition first, then the action decision linking it); `tools/contracts/test_reconstruction_disposition.py`; the valid fixture `decision_with_its_disposition` and the invalid `disposition_decision_id_not_a_decision_id` (`pattern`).
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: BO's US-06 expected events can list `disposition_decision_id` on the action decision's DECISION item; EL's escalation path (noema#733's branch) records the disposition first.
- Files to read first: `trust/recorder/recorder.go` (DECISION_MADE payload), `record/reconstruct/project.go`, `contracts/README.md` (the A503 note).
- Do not touch: `reconstruction.v1.json` and `reconstruction.v2.json` (pinned by a test).

# Session 2026-11-23 05:15 WAT · TRU-10 (C1, the Writer rule flips: steps if and only if a disposition; A503, the Founder's order of 22 Nov 2026)

## Session summary
- What changed (PR links): noema#733. `trust/recorder.Writer.Write` refuses a decision with no step that names a disposition (`ErrDispositionWithoutSteps`, new), next to the existing refusal of a decision with steps and no disposition (`ErrDispositionRequired`); `orchestrator/portadapters/trust.RecorderAdapter` refuses either mismatch before it opens a transaction (`ErrRecordRequest`). The tests that put a disposition on a decision with no step (a read ALLOW, a REJECT, a WAIT) now expect the refusal; every test that records a decision with steps records its ESCALATE disposition first through a real `Writer.Write` (no INSERT): `trust/recorder`, `orchestrator/portadapters/trust`, `orchestrator/portadapters/trustpg`, `adapters/outcome_e2e`.
- Why: the Founder's order after noema#883 merged: a decision names its escalation disposition if and only if it carries steps, so the Record never holds an action without its disposition or a disposition on a decision that does nothing.
- Risks, and how they are contained: a caller that passes a disposition on a read or a refusal is refused (no caller does: the escalation path of EL records the disposition as its own decision and names it on the steps decision only). A REJECT cannot carry steps ("grants are issued only for an ALLOW"), so a decision that names a disposition is always an ALLOW with steps. The adapter refuses before opening a transaction, the Writer refuses before the first write: nothing is written by either refusal (counted in the tests).
- Tests added (REQ-TRU-010; INV-25, EC-E16): `trust/recorder/disposition_without_steps_test.go` (read ALLOW, REJECT, WAIT with a disposition: refused, rows unchanged; the disposition decision itself is written); `escalation_link_test.go` (the property now draws the no-step cases; a REJECT naming a disposition is refused); `orchestrator/portadapters/trust/recorder_disposition_test.go` `TestAStepsAndDispositionThatDoNotGoTogetherAreRefusedBeforeAnyTransaction`.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL adds the escalation path as commits on `feat/TRU-10-disposition-required` (A508): record the ESCALATE disposition as its own decision first, then name it on the steps decision. A511 (`intake.PlanRequest` filled from Trust's own steps) is a separate PR from main.
- Open questions (owner): none new.
- Files to read first: `trust/recorder/recorder.go` (`ErrDispositionWithoutSteps`), `orchestrator/portadapters/trust/recorder.go` (`outcomeShape`).
- Do not touch: `services/core/migrations/` (0012 has the column).

# Session 2026-11-23 12:35 WAT · TRU-10 (C1, A542 and A534: the disposition belongs to the same intent; the rule applies only to the escalation plan's steps)

## Session summary
- What changed (PR links): noema#733, tests first (RED) in this commit; GREEN follows after the red CI run is read. (1) A542 (interim, relayed): a disposition must belong to the same intent (the intent ID and the intent hash) as the action decision that names it, else `ErrDispositionNotValid` and nothing is written. (2) A534 (interim, relayed): the rule applies only to a decision whose steps include a capability of the injected escalation set; a `log_complaint_reference` ALLOW with a grant and no disposition is accepted; the adapter's pre-transaction rule follows the same scope (it holds the set). Merge after #903 (merged) and EL's path.
- Test changes stated: `TestADecisionWithStepsAndNoDispositionIsRefusedAndNothingIsWritten` now loops over the escalation plan's capabilities only (the complaint write and an unknown write moved to the new accept test); the rapid property requires the disposition only when a step is in the set, and its closing check is about grants of the plan; the helper `world.action` takes the disposition's intent ID and hash and its own sealed snapshot (another input, A186), `escalateDisposition`, `world.disposition` (trustpg) and `realGrantWorld` do the same, so every test that records an action decision after a disposition records the same intent.
- Tests added (REQ-TRU-010; INV-25): `trust/recorder/{escalation_rescope_test.go,intent_binding_test.go}`; `orchestrator/portadapters/trust/recorder_disposition_test.go` (`TestTheRuleBeforeTheTransactionAppliesOnlyToStepsOfTheEscalationPlan`, `TestAWriteOutsideThePlanNeedsNoDispositionThroughTheAdapter`, `TestADispositionOfAnotherIntentIsRefusedThroughTheAdapter`).

## Pre-work for the next agent
- Exact next step: GREEN in `trust/recorder/recorder.go` (require the disposition only when a step is in the set; `requireEscalationDisposition` compares the intent ID and the intent hash) and `orchestrator/portadapters/trust/recorder.go` (`outcomeShape` with the escalation set, saved from `RecorderConfig`); EL's escalation path on this branch (A508).
- Do not touch: `services/core/migrations/`.
