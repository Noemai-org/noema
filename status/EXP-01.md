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

### Codex review of #224, round two
A calendar date is one value in check 2 (`2026-05-11` no longer passes against `2026-11-05`), and a template-bound claim must be the template the input facts select (`<locale>/<outcome_id>/<classification>`), so a SUCCESS_CONFIRMED template cannot pass against INSUFFICIENT_EVIDENCE facts. Red commit first, then the fix; three mutants killed.

### Codex review of #224, round three
A claim is supported only by KNOWN facts (INV-13, EXP-02): citing an INFERRED fact, alone or with a KNOWN one, is blocked, because a claim carries no hedge marker. Reversible: a later `Hedged` flag on a claim could allow an INFERRED fact in hedged wording; that is a contract change and not made here. Red commit first; one mutant killed.

### Codex review of #224, round four
Name detection no longer depends on capitalisation: every word of a rendered text must occur in the facts (value, label, as_of, kind), in the pack's reviewed templates for the locale, or be an ordinary function word, so `we spoke to bello` is blocked. A consequence to know about: a model text that uses words the templates do not (for instance "money is safe") is blocked and falls back to the template; widening the vocabulary is a pack-data change for EXP-03/EXP-04, not code. Duplicate fact IDs are refused. Red commit first; three mutants killed (one, forcing the duplicate's status to KNOWN, is equivalent because the duplicate error already blocks).

# Session 2026-11-08 · EXP-01 slice 3 · the forbidden-claim lists as pack data (Architect decision A3)

## Session summary
- What changed: `packs/fintech/templates/en-NG.forbidden.json` (nine patterns, from PERD 31.2 and the earlier Python list), `explanation.LoadForbidden`, README lines in `packs/fintech/templates/` and `services/core/explanation/`. The Python template test now reads the pack file instead of its own literal list, so one source feeds both checks.
- Tests added: `forbidden_test.go` (load; every pack template passes its own list; the seven PERD claims are blocked by check 3; a missing, empty, blank, duplicate, invalid or non-array file fails closed) and `ForbiddenClaimsFile` in `packs/tests` (shape, coverage, packcheck does not take the sibling for a locale). Red first (commit 89d723b).
- Directory check (the Architect asked): the templates live in `packs/fintech/templates/<locale>.json` (R3 (c)); `LoadTemplates` and packcheck read only that exact name per declared locale, so `<locale>.forbidden.json` beside them is not read as a locale.
- Debt entries: none. Conflicts raised: none.

# Session 2026-11-08 (later) · EXP-01 slice 3 · EL's blocking finding on noema#224

## Session summary
- What changed: the figure, digit-run and word patterns of `explanation.go` read any script (`\p{Nd}`, `\p{L}`, `\p{M}`), and the digit-run length counts runes, not bytes. README: the non-semantic binding, the caller must take outcome and classification from the verdict, `ErrNoTemplate` is never shown, why `Validate` is long.
- Why: EL's statement at 8caf0dd was NOT READY: fullwidth and Arabic-Indic digits and a Cyrillic name passed checks 2 and 4. I had posted "READY FOR CODEX" at 140fe67 over that open finding; that was premature and is corrected here.
- Tests added (PERD 31.2 checks 2 and 4, AGENT-13): `unicode_test.go` (nine inputs must be blocked, long digit runs in other scripts must be blocked by check 4, every pack template still passes). Red first (commit e18f3286).
- Debt entries: none. Conflicts raised: none. The `forbidden == nil` case of `Validate` is still allowed (tests pass nil on purpose); the production caller loads the list with `LoadForbidden`.

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

## Session 2026-11-12 04:00 UTC · EXP-01 · the template version is checked (follow-up to noema#224)

## Session summary
- What changed: `Validate` refuses a template-bound render whose `TemplateVersion` is not the loaded version of its template (new `Templates.version`, four lines in `Validate`), with `TestATemplateBoundRenderMustCarryTheLoadedTemplateVersion` (empty, another digest, the loaded digest upcased, the loaded digest plus text, not a digest) and a README paragraph. Nothing else in the package changed.
- Why: found by C2 while answering the review of noema#224: text and template id were right but the version string was not compared with the loaded digest, so a wrong value would have been stored as provenance (A18). It cannot change what a customer sees. Not part of the four findings on noema#224; it waited until that PR merged (A131).
- Risks, and how they are contained: a render that carries no template version (fact-bound claims only) is not checked; a caller that builds a template-bound render by hand must copy `Fallback`'s version. Both are stated in the README.
- Tests added (REQ-EXP-002, A18): the one above. Failing first: seen failing locally on the five variants before the fix (no separate red CI run). Mutants killed: check disabled; case-insensitive comparison.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: slice 4 (the `orchestrator.Explainer` constructor) waits on the two decisions on noema#461.
- Do not touch: the fallback templates (EXP-04's content).


## Session 2026-11-12 (C2) · slice 4: fact slots, FactsSource, the Explainer (ruling A155)
- **What changed:** `{fact:<kind>}` slots (`slots.go`, `Fallback`, `LoadTemplates`, `Validate`); `FactsSource`, `Recorder`, `Subject`, `Event`, `Explanation` and `NewExplainer`/`Explain` (`explainer.go`); tests `slots_test.go` and `explainer_test.go`; README.
- **Failing first:** tests-only commit `4b199633` did not compile.
- **Mutations by hand, each red:** slot ambiguity, INFERRED fact, brace in a value, empty value, no fact, stray brace in a template, `Validate` brace check, `Validate` span check (first survived: the figure check caught my altered text; added a reordered-words case), fact IDs dropped, the render record error ignored, the block record error ignored, `Blocked` flag, subject unchecked, decision unchecked, no timeout (red in 2 s after the hang-based test was replaced), forbidden list not applied, empty forbidden list accepted, wrong rule code, correlation id unchecked (survived until a test existed), nil recorder accepted, text in the blocked event.
- **Debt for the Steward to register:** rule codes come from prefixes of `Validate`'s message strings (`ruleOf`); a typed problem list would remove the string matching. The rule code names are mine (PERD 28.2 says `EXPLANATION_BLOCKED{rule}` without a list); REC-04 may rename them.
- **Not done, stated:** the Record-backed `Recorder` (payload shapes are REC-04's), the real `FactsSource` (EL, A155), the compose adapter (EL), the pack templates with slots and `packcheck` (needs one Founder approval, A155).
- **Conflicts raised:** none.

### Pre-work for the next agent
- Exact next step: C2 builds the `RecordRecorder` (as `core-edge`, `EXPLANATION_RENDERED` claims in the shape `record/reconstruct/project.go` reads, `EXPLANATION_BLOCKED{rule}`), then the pack edit under Founder approval.
- Open questions (owner): the payload shape of `EXPLANATION_RENDERED` for template-bound claims (REC-04).
- Files to read first: `services/core/explanation/README.md`, `explainer.go`.
- Do not touch: the pack templates (Founder approval).
