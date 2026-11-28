# Session 2026-11-26 · LANE-5 · A723 (C1, interim, relayed): `outcome_bindings` on pack.v1

## Session summary
- What changed: `pack.v1.json` gains the optional `outcome_bindings` (per write capability: outcome, variable sources, retry safety); packcheck `check_outcome_bindings`; SYNTHETIC fintech data for the four non-prohibited writes; the manifest pin; regenerated models and shape-refusals; fixtures.
- Why: A723. The Outcome request builder (slice 4) needs, per capability, the outcome and where each variable comes from, and the core may not hold those names (INV-14).
- Risks: (1) the epoch variable of `complaint_logged` is bound by the fourth source `EPOCH` (A740, the NextEpoch port; added in the second round of this PR); (2) hashed params need the same hash on the reading side. Both are named, neither decided here (AGENT-16).
- Tests added: `OutcomeBindings` (11 tests), 10 pack fixtures. Regression cases: the red at `c9cc76c25`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's slice 4 reader consumes `outcome_bindings` with the receipt's `step_params`.
- Do not touch: the values of `rules.v1.json`, the capability descriptors.

# Session 2026-11-28 · LANE-5 · SR on #1026 (C1)
- Packcheck now enforces what EPOCH means: it binds an Integer variable only, at most once per binding, and only with a SUBJECT in the same binding (red at `6e34d20c2`). The runtime source (`outcome/request.Source`, #1019) does not expose EPOCH yet: that is a PR stacked on #1019 that adds the `EPOCH` kind and the NextEpoch port; nothing reads an EPOCH binding until it lands, and an unbound variable fails closed.
