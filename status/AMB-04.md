# Status · AMB-04 (Core Engineer 2) · the SLOT question template

Updated 2026-11-27. All data is SYNTHETIC.

## State
- Order of the Architect, 27 Nov 2026: "A757: build the AMB-04 SLOT question template PR now (optional question_key in intent-node.v1, template key rule, fin.clarify.when text in en-NG with a native-speaker review flag, packcheck per locale); EL builds the resolver." (relayed; not yet committed as a ruling, so this file and the PR body are its record). Questions are `clarification.v1` mode SLOT (A582, A563, A591, A593, A600): the wording is a pack template named by `question_key`.
- **Built in this PR (noema#1045):** (1) `intent-node.v1.json`: a slot declaration gains the optional `question_key` (pattern of a template id); (2) `packcheck` check `slot_questions` and the template coverage: a slot question has a text in every locale, a candidate_search slot carries none, a key is used once, a key with no slash that no slot names is refused; (3) the pack: the `when` slot names `fin.clarify.when`, `en-NG.json` holds its text, the manifest pin is recomputed; (4) the explanation and en-NG tests loop over summaries only and a new test covers the question keys.
- **The en-NG text is a draft for a native speaker** ("On which day, or at what time, did this happen?"). It is not reviewed (REQ-EXP-002 dependency, EXP-04). `pcm`, `yo`, `ha` and `ig` have no text; none is invented.
- **Not done:** the resolver that builds a SLOT card from the key (EL, A757); texts for the OPTIONS and ATTRIBUTE keys (`fin.clarify.options` and the four `by_*` keys); the four other locales (EXP-04); the answered SLOT value joining the intent as v(N+1) (A743, INP-01).

## Session 2026-11-27 · C2 (A757)

### Session summary
- What changed (PR links): noema#1045.
- Why: a missing optional slot (the time window) had no question wording; the core may not hold wording (INV-14), so the pack carries it.
- Risks, and how they are contained: a new optional contract member (additive, v1, A698). The pack file's SHA-256 changes, so the manifest pin is recomputed and the compile-acceptance fixture `boundary-a604` (the recorder's pin of the pack at its run) is not edited: it records the pack of its own run. A run dispatched after this merges pins the new hash itself.
- Tests added (REQ-AMB-004, REQ-EXP-002, INV-14): `SlotQuestions` (6) and two `TemplateCoverage` cases in `tools/packcheck/test_packcheck.py`; `test_templates_en_ng.py` (question keys equal the slot keys; the forbidden list and plain-language checks cover them); `explanation/questions_test.go`; contract fixtures `valid/intent-node/investigate_slot_question.json` and `invalid/intent-node/slot_question_key_not_a_template_id.json`.
- Regression cases added: the invalid fixture and the packcheck negatives above.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL builds the resolver over `question_key`; EXP-04 writes the four other locales with a native-speaker review and then the OPTIONS and ATTRIBUTE texts.
- Open questions (owner): who reviews the en-NG wording (Founders).
- Files to read first: `contracts/README.md` (the A757 note), `tools/packcheck/checks.py` (`check_slot_questions`), `packs/fintech/templates/README.md`.
- Do not touch: the pack's `fin.clarify.options` and `fin.clarify.by_*` keys (no text yet; not in this change).
