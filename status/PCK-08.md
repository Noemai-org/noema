# Session 2026-11-23 12:30 WAT · PCK-08 (A544: pin the limits files; fill the Reconciler and Clocks routing)

## Session summary
- What changed (PR links): noema#910. (1) The manifest gains the additive kind `limits` (version 3): every file of `fintech/limits/` is pinned, `packload.Load` verifies them and returns `Pack.Limits` by pinned path, packcheck lists the kind. (2) `trust-limits.v1` gains four optional members (`escalation_reasons`, `allowed_under_freeze`, `phase_routes`, `anchor_entities`), additive (AGENT-15 note in `contracts/README.md`), models regenerated; `platform/trustlimits.Load` checks them (patterns, lengths, repeats) and exposes none. (3) `packs/fintech/limits/trust_limits.json` carries the values the PERD and the SoT state; `packcheck` has a new check `trust_routing` that the routing names only what the pack declares.
- Why: ruling A544 (interim): the Reconciler and Clocks routing is pack data owned by C2, pinned in the manifest; a value the documents do not state is an OPEN entry with the safest default.
- Risks, and how they are contained: no consumer reads the new members yet (EL's adapters), so nothing changes at run time. A file added to the limits folder without a pin now refuses the pack (by design). The manifest pins all four limits files, not only `trust_limits.json`, because the loader's rule is that every file of a listed kind's folder is listed.
- Tests added (REQ-RES-002, REQ-TRU-004, REQ-TRU-006, INV-14, AGENT-16): `tools/packcheck/test_manifest.py` (class `Limits`), `tools/packcheck/test_packcheck.py` (class `TrustRouting`), `services/core/platform/packload/packload_limits_test.go`, `services/core/platform/trustlimits/routing_test.go`, 14 invalid and 1 valid contract fixtures.
- Regression cases added: the above.
- Debt entries: none.
- Conflicts raised: none. One reading to confirm (not a conflict): REQ-RES-002 says the freeze allow-list is the capabilities "whose descriptor declares `allowed_under_freeze: true`", and `reconciler.Pack.AllowedUnderFreeze()` is documented as "the descriptor's allowed_under_freeze"; the pack's capability descriptors carry no such member today and A544 puts the list in `trust_limits.json`. If descriptors later declare it, one of the two must go.

## Values: stated by the documents, and OPEN
Stated, and in the file:
- `allowed_under_freeze` = `fin.cap.create_escalation_case`, `fin.cap.record_investigation_evidence`, `fin.cap.route_to_internal_owner`: PERD v1.23 REQ-RES-002 [ADOPTED DEC-2026-10-17]; SoT INV-20.
- `escalation_reasons`: CONTRADICTORY_STATE to `fin.reason.contradictory_state` and EXECUTION_OUTCOME_UNKNOWN to `fin.reason.execution_outcome_unknown`: PERD v1.23 §23.3 T10 ("ESCALATE on contradiction or unknown outcome, per pack mapping") and §23.1 (the example codes), which are the catalogue's two STATE_MAPPING codes.
- `anchor_entities`: `complaint_logged` to `complaint_ref.effective_at`: A438 (interim: "The clock anchor is complaint_ref.effective_at"); PERD v1.23 §23.5 (the anchor event is declared in clock data, for example the complaint logged time) and the pack clock's `anchor_event`.

Not stated, so absent from the file (absent means no route, which is UNKNOWN, and nothing allowed under a freeze):
- OPEN-A544-1, the escalation reason of any other classification (DEBIT_POSTED_CREDIT_UNCONFIRMED, SETTLEMENT_PENDING, REVERSAL_PENDING, POSSIBLE_DUPLICATE, PARTIAL, EVENT_ORDER_UNCERTAIN, INSUFFICIENT_EVIDENCE). PERD §22.3 says EVENT_ORDER_UNCERTAIN is escalated "when the decision depends on it" and gives no code. Safest default: no route (UNKNOWN).
- OPEN-A544-2, the phase routing: which of NOT_STARTED, RUNNING, AT_RISK and BREACHED escalate, and with which code. PERD §23.5 says priority derives from the phase "through pack routing rules" and §23.3 T12 says "may escalate per policy"; no phase is named. The catalogue holds `fin.reason.sla_at_risk` and `fin.reason.sla_breached` (CLOCK_MAPPING), so the likely proposal is AT_RISK to the first and BREACHED to the second, to be decided. Safest default: no phase is routed (`phase_routes` absent).
- OPEN-A544-3, other freeze-allowed capabilities: none beyond the three. Safest default: nothing else allowed.

Ready-to-paste block for `docs/open-decisions.md` (the Steward registers it; this PR edits no `docs` file):

> **OD-A544-1 [OPEN]** Escalation reason per classification beyond CONTRADICTORY_STATE and EXECUTION_OUTCOME_UNKNOWN. Needed by: the Reconciler (T10). PERD v1.23 §22.3 and §23.3 do not give a pack reason code. Safest default: no route, so UNKNOWN. Invalidated by: a ruling naming a code per classification.
> **OD-A544-2** [OPEN] Clock phase routing (which phase escalates, with which code). Needed by: Clocks (T12). PERD v1.23 §23.5 does not name one. Proposal: AT_RISK to `fin.reason.sla_at_risk`, BREACHED to `fin.reason.sla_breached`. Safest default: none routed. Invalidated by: a ruling.

## Pre-work for the next agent
- Exact next step: EL's adapters read `Pack.Limits[<trust_limits path>]` through `trustlimits.Load` and expose the routing members (this PR adds none): `reconciler.Pack.EscalateReason` and `AllowedUnderFreeze`, `ClocksConfig.Routes`, `NewAnchors`'s map.
- Open questions (owner): OD-A544-1 and OD-A544-2 (Architect); the descriptor-versus-file question above (Architect).
- Files to read first: `contracts/schemas/trust-limits.v1.json`, `services/core/platform/trustlimits/routing.go`, `packs/fintech/limits/trust_limits.json`, `tools/packcheck/checks.py` (`check_trust_routing`).
- Do not touch: the other three limits files' values (not part of this task).
