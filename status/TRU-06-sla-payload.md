# Session 2026-11-25 · TRU-06 (C1: the SLA_EVALUATED payload schema, A681)

## Session summary
- What changed (PR links): noema#984. One additive contract file, `contracts/schemas/sla-evaluated-payload.v1.json`, with fixtures (4 valid, 20 invalid), an INVENTORY row, a migration note in `contracts/README.md`, the regenerated Go and Pydantic models, and `tools/contracts/test_sla_evaluated_payload.py`.
- Why: A681 approves the `SLA_EVALUATED` payload schema as one additive contract file (OD-P-116: the ruling states no members, so they are proposed here from `Evaluation.EventPayload` in noema#977). The event type existed in `record-event` v1 to v3; its payload was pinned by a Go test only.
- Risks, and how they are contained: a member name drifts between the evaluation and the schema. The generated type lets noema#977's test read the member names from the contract instead of repeating them. No existing schema changed, so no reader changed meaning.
- Tests added (REQ-TRU-006, REQ-AUD-001; INV-21, AGENT-15): the Python test file and the 24 fixtures. Each invalid fixture names the failing keyword in `expectations.json`, so a rule that stops firing is caught.
- Regression cases added: the fixtures above. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after both Founders approve and this merges, change noema#977's payload test to read `contracts.SlaEvaluatedPayloadV1` (no change to the payload code is needed).
- Open questions (Architect): the member list and the two conditional rules (OD-P-116).
- Files to read first: `contracts/schemas/sla-evaluated-payload.v1.json`, `services/core/trust/clocks/routing.go` (noema#977).
- Do not touch: `record-event.v1` to `v3` and `enums.v1.json`.
