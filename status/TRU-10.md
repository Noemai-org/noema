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
  - a decision naming no disposition is written whatever its steps (the complaint write, an unknown capability);
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
- **OPEN reading: does a non-escalation ALLOW need a disposition?** PERD 23.1 and REQ-TRU-010 say that the steps of an escalation plan reference their disposition. They do not say that every ALLOW with a write step must. The pack has a write outside the escalation graph (`fin.cap.log_complaint_reference`, `g3_complaint.json`) with its own policy rules (`permit_complaint`, `complaint_confirm`) and a bridge mapping. Requiring a disposition for every write would make it unwritable, because the database allows only an ESCALATE disposition and under it only the escalation plan's capabilities. The narrower reading is implemented: no disposition required, and a decision that names none is unchanged. If the Architect rules that every write must link, the change is one condition in `check` plus new tests, and the complaint write needs a ruling of its own.
- **OPEN: same investigation or intent.** The disposition is "for the investigation intent" (PERD 23.1) and each step has its own decision; the two-node intent model gives them different intents. The recorder does not compare `intent_id` or `correlation_id`. A rule would need to say which field links them.
- **OPEN: which graph is "the escalation plan".** See "Not built". The Steward or Architect to name the designation (a pack field, or a deployment setting).

## Pre-work for the next agent
- Exact next step: the production adapter that builds `recorder.EscalationSet` from the pack once the designation exists; wire it where `recorder.Writer` is built (the `orchestrator.Trust` adapter, status/TRU-05.md). Test: the adapter's set equals the capabilities of the escalation graph's WRITE nodes.
- Open questions (owner): the three above (Architect).
- Files to read first: `services/core/trust/recorder/recorder.go` (`check`, `requireEscalationDisposition`), `escalation_link_test.go`, `services/core/migrations/0012_decision_record.sql`.
- Do not touch: `Write`'s order (Record first on a miss; nothing appended on a hit); migration 0012.

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

# Session 2026-11-26 · TRU-10 · A709 (C1, interim, relayed): migration 0043

## Session summary
- What changed: `0043_decision_early_key_escalate_only.sql` replaces 0040's `decision_record_early_key_shape` with a CHECK in which a decision naming a turn and a principal has `outcome` ESCALATE or REJECT (was: not ALLOW or CONFIRM; widened to REJECT for A680, a T1 to T9 refusal keyed by the turn). The test of "every early outcome may name a turn" is replaced by "an ESCALATE or a REJECT may; CLARIFY, WAIT and UNKNOWN may not".
- Why: A709 (the next migration tightens 0040's CHECK). The early gate writes an ESCALATE only (A663, A574, A577) and the Writer already refuses anything else with a turn.
- Risks: the number 0043 is the next free number on `main` at this head; the migration ledger (A171) governs, so another PR may take it first (then this PR renumbers, content unchanged). The CHECK is validated against existing rows: a database holding a turn on another outcome (none written by this code) fails the migration, by design.
- Tests added: `platform/db/decision_early_key_test.go` (REQ-TRU-010; INV-03, INV-17). Regression cases: the red tests at `e2c322d4b`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: an independent role read of the migration (A685, EL or C2), then both approvals.
- Do not touch: 0040 (never edited after merge).

