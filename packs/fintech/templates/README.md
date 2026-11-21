# packs/fintech/templates

**Implements:** REQ-CAP-001, REQ-AG-001, REQ-AG-002, REQ-NFR-005.
**Owning task:** PCK-01 (the rewrite of the fintech pack) and, for `graph/`, T2 of DEC-2026-10-20.

Will hold the fintech pack's explanation and clarification templates. Packs are data, loaded and never imported as code (SoT §51). Empty: the existing pack is `packs/fintech_v1.json` until PCK-01 replaces it.

Created by T1 of DEC-2026-10-20.

## Format and coverage (PCK-02, decision of 3 Nov 2026)

One JSON file per locale, `packs/fintech/templates/<locale>.json`, for each locale the pack declares in `understanding.locales` (`en-NG`, `pcm`, `yo`, `ha`, `ig`). Each file is an object keyed `<outcome_id>/<classification>` with non-empty text as the value. The keys are every outcome in `outcome_definitions` times every classification the pack's reconciliation rules can produce (`recon_rules/rules.v1.json`: each rule plus the fallback). `python3 tools/packcheck validate --templates-required packs/fintech_v1.json` checks 100% coverage and names each missing locale, key or empty text; mechanism and negative fixtures: `tools/packcheck/test_packcheck.py` (`TemplateCoverage`).

The content is EXP-04's (Core Engineer 2, with native-speaker review). The flag is off in the default run only until EXP-04 delivers; **EXP-04 cannot close until the real pack passes with `--templates-required`**, and it turns the flag on in CI.

## Forbidden-claim lists (Architect decision A3, 8 Nov 2026)

`packs/fintech/templates/<locale>.forbidden.json` is a JSON array of regular expressions (Go RE2 syntax; prefix `(?i)` for case-insensitive), the locale's forbidden claims for PERD 31.2 check 3. It sits next to the templates and is loaded as data (`explanation.LoadForbidden`); a locale with no usable file fails closed. Only `en-NG` exists now; the other four are EXP-04's with native-speaker review. packcheck reads only `<locale>.json`, so these files are not mistaken for locales. A pack's own templates must pass its list (`TestEveryPackTemplatePassesItsOwnForbiddenList`).

## Fact slots (ruling A155; the slot inventory is noema#586; the Founder's approval of the pack edit: 14 Nov 2026, noema#595)

A template may hold `{fact:<kind>}` slots. The renderer fills each from the `value_masked` of the **one KNOWN fact of that kind**; no fact, an INFERRED fact, two facts of one kind, an empty value or a value with a brace blocks the explanation (`ErrSlot`, INV-13): it is never guessed and never retried looser. The en-NG templates hold the kinds the inventory marks **stated** (a PERD or user-story sentence names them), by outcome and in sentence order:

| Outcome | Slots |
|---|---|
| `fin.outcome.exception.status_explained` | `payment.amount`, `account`, `payment.time` |
| `fin.outcome.exception.case_opened`, `case_routed`, `complaint_logged`, `evidence_recorded` | `case.ref` |

The kinds the inventory marks [REC] (`payment.counterparty`, `case.team`, `case.clock`) are not in the pack. A `case.ref` fact exists only with an ACHIEVED verdict, so a `case_*` explanation exists only for a verified case. There is no slot-free sentence beside a slotted one: a missing fact blocks the explanation (a degraded answer would be a change to the loader). The text outside the slots states no digit and no name; the filled text passes the forbidden list and the digit guard (`packs/tests/test_templates_en_ng.py`, `explanation/realpack_slots_test.go`). The sentences are not yet reviewed by a native speaker (REQ-EXP-002 dependency, EXP-04).
