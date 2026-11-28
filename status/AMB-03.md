
# Session 2026-11-28 · AMB-03 · SLOT answer path (A563, A743; C1, interim, relayed)

## Session summary
- What changed: `understanding.SupersedeWithSlot` builds the raw document of v(N+1) of an admitted intent that carries one answered slot: version one more, `supersedes` = `<intent id>@<N>`, `created_at` = now, the slot set, and the model markers for the principal type, the assertion and the signature (so the gate admits it and the core overwrites them with the session's values). An answer never overwrites a slot the intent holds (`ErrSlotAlreadySet`); the gate still judges the slot's name and type (UNKNOWN_SLOT, SLOT_TYPE) and re-runs over the document (A743). The model is not asked; the answer is data (AGENT-09).
- Why: A563 (a missing time window makes Trust ask the customer when, CLARIFY) and A743 (an answered SLOT value joins the intent as v(N+1) with supersedes, and the gate re-runs).
- Not built (named): the route that receives the answer (`POST` of a clarification answer, the edge) and the caller that saves the admitted v(N+1) with `intentstore.Save` and re-runs the intake gate; both need the clarification store's reader of the asked slot (the request-by-id read for clarifications) which `main` does not have. This PR is the construction and its tests.
- Tests added (REQ-AMB-001; INV-02, INV-12): `slotanswer_test.go`. Regression cases: the red at `f77a29bf8`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the clarification answer route reads the open SLOT clarification (its `slot`), calls `SupersedeWithSlot` with the stored latest intent, `Gate.Admit`, `intentstore.Save` and the intake again.
- Do not touch: `Gate.Admit`'s checks.
