# PCK-05 convergence windows: state

State: red and green committed locally, not pushed. One open item blocks a green CI: the corpus metamorphic property fails after the reversal pair is removed (see the CONFLICT block in the PR body). All data is SYNTHETIC.

# Session 2026-11-26 WAT · PCK-05

## Session summary
- What changed (PR links): not yet opened. Rule set `fin.recon.rules` is version 2 (A659): each matrix entry has one `convergence_window_seconds` (300, SYNTHETIC); `reversal_conflict` is out of the active list (A674). `ruleset.go` requires the window above zero from version 2 and still refuses it in version 1. packcheck checks the window against the smaller `freshness_sla_seconds` of the pair's two sources, linked through the evidence mapping; the rule file must be labelled SYNTHETIC. References to `fin.recon.rules@1` moved to `@2`; manifest re-pinned.
- Why: A674 (interim, relayed, not yet committed; `DEC-2026-11-25-architect-rulings-a673-a684`) and A659.
- Risks, and how they are contained: removing the reversal pair makes the `unless reversal POSTED` clause of `credit_without_debit` non-monotonic over the real rule set (open item below). Version 1 parsing is unchanged.
- Tests added (REQ-RES-001, REQ-AG-001, INV-14): `trust/reconcile/ruleset_window_test.go`, `ConvergenceWindows` in `tools/packcheck/test_manifest.py`; the corpus test follows A674.
- Regression cases added: none beyond the above.
- Debt entries: none.
- Conflicts raised: one (PR body), the metamorphic property against A674.

## Pre-work for the next agent
- Exact next step: EL decides the open item (drop the `unless` of `credit_without_debit` until a reversal source exists, or another reading), then pushes.
- Open questions (Architect): see the CONFLICT block.
- Files to read first: `services/core/internal/corpus/reconcile_test.go` (`TestMetamorphicProperties`), `packs/fintech/recon_rules/rules.v1.json`.
- Do not touch: the evidence mapping's rows; a future-dated `effective_at` belongs to #949 (A677).
