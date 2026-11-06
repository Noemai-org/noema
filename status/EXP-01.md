# Status · EXP-01 (Core Engineer 2) · template-only explanations

Updated 2026-11-06. Plan task EXP-01 (owner C2; REQ-EXP-002; Needs OUT-01, merged). Rulings: A8 (templates in the pack as data, en-NG; code in `services/core/explanation/`) and A18 (templates keyed by (outcome class, locale), the template ID is an output; one contract bump carries the explanation-facts shape and a nullable `voice.audio_ref`; no tag, A12). The card (docs#113) stays a draft; its Q1 is answered by A8 and A18 and the PCK-02 format already in `packs/fintech/templates/README.md` (one JSON file per locale, keyed `<outcome_id>/<classification>`).

## Slice 1 · the contract bump (this PR)
- `explanation-facts.v2.json`: v1 with the input list `template_ids` replaced by `outcome_id` and `classification`, and a new `voice` object whose `audio_ref` is a nullable reference (null when no audio exists). C2 defines it under A18; the shape is a reading of "nullable voice.audio_ref" and is easy to change before anything serves it. Fixtures: the v1 set re-expressed in v2 plus two valid (audio_ref null and set) and seven invalid. INVENTORY row with the AGENT-15 migration note. Generated Go and Pydantic types regenerated, not edited. No tag.
- Order of work: fixtures and schema were written together, and I pushed before running the contracts unit tests: `tools/codegen/shape_refusals.py` needed regenerating for the new invalid fixtures (fixed in the next commit); `tools/contracts/validate.py` is the check (it fails on a schema without a valid and an invalid fixture and an INVENTORY row). I did not push a tests-only commit for this slice.

## Slice 2 · en-NG templates (noema#PR stacked on this one)
- `packs/fintech/templates/en-NG.json`: 5 outcomes x 7 classifications = 35 static texts, no digit, no placeholder (every figure/time/name comes from facts, REQ-EXP-001). Tests first: `packs/tests/test_templates_en_ng.py` (red commit, then green). Other locales are EXP-04's; the real pack still fails `--templates-required` until EXP-04.

## Next slices
3. `services/core/explanation/`: `Render(facts) -> {text, claims, template_id, template_version}` from the pack data, with the §31.2 checks that need no model.

## Slice 3 · the renderer and the validator (stacked on slice 2)
- `services/core/explanation/`: `LoadTemplates`, `Fallback` (template text, template ID `<locale>/<outcome_id>/<classification>`, version = digest of the locale file), `Validate` (PERD 31.2 checks 1 to 4 plus forbidden patterns). Failing-first: tests-only commit "EXP-01: failing-first tests for the deterministic fallback renderer and the claim validator (red on purpose)". Mutation check: eight mutants (each check, unknown fact, template span, template id, forbidden pattern, fact claims) all killed after two added cases.
- Gap, stated: the forbidden-claim pattern lists are "pack data" in PERD 31.2 but no such file exists in the pack yet; `Validate` takes the patterns as an argument. A pack file for them is a pack-owner (PCK) change.

### Codex review of #224 (three findings, fixed test-first)
Check 2 now compares whole figures (thousands separators dropped, so 50 is not 50,000; a time also matches its hour:minute), counts each fact's `as_of` and `source_label` as fact data, and blocks any capitalised word that is neither in the facts, nor in the rendered template, nor an ordinary English function word (`openers`, language syntax not domain data, reversible if too strict: a false block falls back to the template). Red commit af1bb63, then the fix; six mutants killed.
