# PCK-09 candidate labels: state

State: PR open (noema#916), red `335f3762e`, green pushed after the red CI was read. All data is SYNTHETIC.

# Session 2026-11-23 WAT · PCK-09

## Session summary
- What changed (PR links): noema#916. `intent-node.v1` `disambiguation` gains optional `options_key`, `display_template`, `attribute_order`; packcheck check `disambiguation`; the fintech pack declares them on the investigate node; manifest re-pinned; models regenerated.
- Why: A555 (interim, relayed, not yet committed): the Candidates Labeller (EL) needs the pack's attribute order, options key and label templates; the pack had no member for them (`portadapters/trust/README-candidates.md`).
- Risks, and how they are contained: additive and optional; a template can show only labelable candidate fields, no digit of its own, no two placeholders touching (REQ-AMB-004, AGENT-13).
- Tests added (REQ-AMB-004, INV-14): `tools/contracts/test_intent_node_candidate_labels.py`, `CandidateLabels` in `tools/packcheck/test_packcheck.py`.
- Regression cases added: the invalid fixtures `options_key_not_a_template_id`, `attribute_order_empty`.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL builds the Labeller from `pack.intent_nodes[].disambiguation` (placeholders `{attr:<candidate field>}`; the Labeller formats the amount from `amount_minor` and `currency`, the time in the customer's local time, and maps nothing to a digit run longer than the pack's `max_unmasked_digit_run`). The wording of each question (`options_key`, each `question_key`) is not in the pack yet.
- Open questions (Founders): the attribute order (occurred_at, counterparty_display, account_last4, amount_minor) is a safe default, not a PERD value; the question wording per question key.
- Files to read first: `contracts/README.md` (the entry above), `tools/packcheck/checks.py` (`check_disambiguation`).
- Do not touch: `trust/intake`, `portadapters/trust` (EL's, C1's).
