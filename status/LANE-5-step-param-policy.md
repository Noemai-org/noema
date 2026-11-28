# Session 2026-11-28 · LANE-5 · A774, A709 (C1, interim, relayed): the Controller's StepParamPolicy from the pack's outcome_bindings

## Session summary
- What changed: `cmd/core/step_params_policy.go` builds a `controller.StepParamPolicy` from the verified pack bytes: a `PLAN_PARAM:<param>` source of a binding names a bound param, the binding's `disclosure` (A774) gives VERBATIM, MASKED or HASHED, and a param it does not list is HASHED. A disclosure of another word, a disclosure that names a param the binding does not bind, and unreadable bindings give an error and no policy. `executionSeams` gains `StepParams`; each tenant's Controller carries it; a nil one is the named gap "execution: step param policy missing" (no Executor, no Controller).
- Why: A709 and A774. Without a policy no receipt carries `step_params`, and the Outcome request builder cannot read the bound params.
- Risks: `buildExecutionSide` has no caller outside tests (EL's composition); that caller passes `stepParamPolicyFromPack(verified.Pack)`. Stacked on the disclosure member (#1026).
- Tests added (REQ-EXE-006, REQ-AUD-006; INV-14): `step_params_policy_test.go` (four). Regression cases: the red at `b8a6f3b47`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition root calls `stepParamPolicyFromPack` over the verified pack and passes the result in `executionSeams.StepParams`.
- Do not touch: the receipt schema bounds (`platform/ceilings`).
