# Status · EXP-01 (Core Engineer 2) · template-only explanations

Updated 2026-11-06. Plan task EXP-01 (owner C2; REQ-EXP-002; Needs OUT-01, merged). Rulings: A8 (templates in the pack as data, en-NG; code in `services/core/explanation/`) and A18 (templates keyed by (outcome class, locale), the template ID is an output; one contract bump carries the explanation-facts shape and a nullable `voice.audio_ref`; no tag, A12). The card (docs#113) stays a draft; its Q1 is answered by A8 and A18 and the PCK-02 format already in `packs/fintech/templates/README.md` (one JSON file per locale, keyed `<outcome_id>/<classification>`).

## Slice 1 · the contract bump (this PR)
- `explanation-facts.v2.json`: v1 with the input list `template_ids` replaced by `outcome_id` and `classification`, and a new `voice` object whose `audio_ref` is a nullable reference (null when no audio exists). C2 defines it under A18; the shape is a reading of "nullable voice.audio_ref" and is easy to change before anything serves it. Fixtures: the v1 set re-expressed in v2 plus two valid (audio_ref null and set) and seven invalid. INVENTORY row with the AGENT-15 migration note. Generated Go and Pydantic types regenerated, not edited. No tag.
- Order of work: fixtures and schema were written together, and I pushed before running the contracts unit tests: `tools/codegen/shape_refusals.py` needed regenerating for the new invalid fixtures (fixed in the next commit); `tools/contracts/validate.py` is the check (it fails on a schema without a valid and an invalid fixture and an INVENTORY row). I did not push a tests-only commit for this slice.

## Next slices (not started)
2. `packs/fintech/templates/en-NG.json` (the five existing slice outcomes times their classifications), packcheck coverage for en-NG.
3. `services/core/explanation/`: `Render(facts) -> {text, claims, template_id, template_version}` from the pack data, with the §31.2 checks that need no model.

# Session 2026-11-08 11:50 WAT · EXP-01

## Session summary
- What changed: `voice.audio_ref` in `explanation-facts.v2` is now `^aud_[A-Za-z0-9]{2,32}$` (was `^[a-z]+_[A-Za-z0-9_]{2,64}$`); Go and Pydantic types regenerated; INVENTORY row updated. Noema#211.
- Why: the Voice Engineer's F1 at `8c5f23a`: the old pattern accepted any lower-case prefix and embedded underscores up to 64 characters, values outside both named reference types (`aud_` spoken answer, `raw_` utterance input, docs#137). The format `aud_` plus a bounded alphanumeric suffix is the Voice Engineer's exact fix and matches the valid fixture. The Security Reviewer's confirmation of the prefix is open in noema#371; if it differs, the pattern moves once, on this PR.
- Risks, and how they are contained: still a breaking v2 that nothing serves yet (EXP-01), so no producer breaks. Tightening only; no value that validated before and is a real reference is lost (`aud_01HZYXWVUTSRQ` still validates).
- Tests added (REQ-EXP-001, AGENT-13): four invalid fixtures (other prefix, embedded underscore, oversized suffix, base64 audio in the field). The first three failed before the schema change (`tools/contracts/validate.py`, commit 4138060); the base64 one already failed on the old pattern and stays as a regression case.
- Regression cases added: the four fixtures above.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: merge main into #217 and #224 after #211 lands (the stack is built on this schema).
- Open questions (owner): SR to confirm the `aud_` prefix (noema#371).
- Files to read first: `contracts/schemas/explanation-facts.v2.json`, `contracts/fixtures/invalid/explanation-facts.v2/`.
- Do not touch: generated `*_gen.go` and `explanation_facts_v2.py` by hand.
