# Status · PCK-02

## Session 2026-11-03 · template-coverage mechanism (Architect's decision 3, final)
## Session summary
- What changed: `tools/packcheck/checks.py` gains `required_template_keys`, `template_locales`, `check_templates` and `run(..., templates="enforce")`; `__main__.py` gains `--templates-required`; `TemplateCoverage` tests; the format is written in `packs/fintech/templates/README.md`.
- Format (decided): one JSON file per locale at `packs/fintech/templates/<locale>.json`, keyed `<outcome_id>/<classification>`, locales en-NG, pcm, yo, ha, ig (read from the pack's `understanding.locales`). The classification set is defined: PERD v1.19 §22.3 [REC], carried as pack data in `packs/fintech/recon_rules/rules.v1.json` (each rule's classification plus the fallback's), not listed in code.
- Enforcement on the real pack moves to EXP-04, which cannot close until `--templates-required` passes on it. Today it reports one failure per missing locale file. PCK-04 now needs the mechanism only (the Steward records this decision).
- Tests, seen failing first (6 errors, no `check_templates`): REQ-EXP-002: the key set; a complete set passes; a missing key fails and names the locale and key (the negative fixture); a missing locale file; an empty text, an unknown key and broken JSON; the real pack is green by default and red with enforcement.
- Debt entries: none. Conflicts raised: none.
- Risks: the `--templates-required` default is off; it must be turned on in CI by EXP-04 (named in the README and here).

## Pre-work for the next agent
- Exact next step: EXP-04 (C2) writes the five locale files and flips the flag in CI.
- Do not touch: `noema#94`, `noema#90`.
