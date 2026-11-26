# Session 2026-11-25 · LANE-5 slice 4, option (b): `step_params` on the signed execution receipt (A692, OD-P-119)

## Session summary
- What changed (PR links): see the PR for branch `feat/CON-receipt-step-params`. RED: fixtures and tests only; the schema is unchanged.
- Why: A692. The Outcome request builder binds a predicate variable from `PLAN_PARAM` (one param of the plan step of the executed capability). Slice 4's reader can only learn those params from the receipt, so Execution (which produces the receipt) signs them into it. Additive, optional.
- Shape stated here (OD-P-119: C1's to state): `step_params` is an object, never empty, whose keys are slot names (`common.v1#/$defs/SlotName`, as the keys of a plan step's `params`) and whose values are what a plan param may hold (string, integer, boolean or null). Absent when the outcome definition has no `PLAN_PARAM` binding. Which params appear is the outcome definition's, not the schema's: only the params its `PLAN_PARAM` bindings name. The schema cannot check that (it needs the pack); the Controller keeps to it, and that is the follow-up.
- Risks, and how they are contained: a signed member the verifier ignores is harmless; one the Controller fills with a param no binding names would widen what the receipt discloses (AGENT-13). The follow-up PR fills it from the bindings only and a test pins that.
- Tests added (REQ-EXE-006, REQ-AUD-006; INV-17): `tools/contracts/test_receipt_step_params.py`, `internal/contracts/receipt_step_params_test.go`, `outcome/receiptsig/step_params_test.go` (the signature covers the member; an old receipt still verifies; removing, changing or adding it after signing is INVALID. These last guards pass on `main` already: the signature is over the whole canonical document), fixtures `{valid,invalid}/execution-receipt`.
- Regression cases added: the fixtures above. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Controller fills `step_params` from the plan step and the outcome definition's `PLAN_PARAM` bindings (a separate PR, Execution); EL builds the slice 4 reader after both land.
- Open questions (Architect): none for this PR.
- Files to read first: `contracts/schemas/execution-receipt.v1.json`, `services/core/outcome/request/request.go` (`FromPlanParam`).
- Do not touch: the receipt's signing tag or key handling.
