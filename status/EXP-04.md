
# Session 2026-11-14 18:10 WAT · EXP-04 · the real pack's fact slots (noema#595)

## Session summary
- What changed (PR noema#595): `packs/fintech/templates/en-NG.json` (the 35 templates gain the `{fact:<kind>}` slots of the inventory: `payment.amount`, `account`, `payment.time` for `status_explained`; `case.ref` for the four case outcomes), the template README, `packs/tests/test_templates_en_ng.py`, `explanation/explanation_test.go` (the shared facts helper gains the other kinds), `explanation/realpack_slots_test.go` (new), `explanation/slots_test.go` (one test that pinned "the pack has no slot yet" now pins one slotted template filled).
- Why: the Founder's order of 14 Nov 2026 (the A209 pack edit; the A155 approval of the slot syntax in the pack).
- Risks, and how they are contained: a missing, INFERRED or repeated fact blocks the explanation (`ErrSlot`), tested for every template that needs it; the classification sentence of each template is unchanged; only kinds the inventory marks **stated** are in the pack. No slot-free sentence is added beside a slotted one (a degraded answer would be a loader change): stated in the PR.
- Tests added (REQ-EXP-001, REQ-EXP-002; INV-13, AGENT-17): the Python slot tests and two Go tests over the real pack, red first at `36d9130a`. One existing Go test was updated, not weakened: it asserted the pack had no slot yet, which the edit changes on purpose.
- Debt entries: none. Conflicts raised: none. Open: a native-speaker review of every changed sentence; the other four locales' files do not exist (`packcheck --templates-required` still fails on `yo`, `ha` and `ig` exactly as on `main`).

## Pre-work for the next agent
- Exact next step: EL's real facts source (STM-01) must produce KNOWN facts of the four kinds, masked per `record/payloadguard`; the [REC] kinds wait for a ruling on their names and sources.
- Files to read first: `services/core/explanation/facts-inventory.md` (noema#586), `packs/fintech/templates/README.md`.
- Do not touch: the classification sentences; the slots' order.
