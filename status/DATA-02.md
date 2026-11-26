# Status · DATA-02 (Core Engineer 2) · money and time representation: property tests

Updated 2026-11-26. Plan task DATA-02 (week 7, workstream SEC): "Money and time representation tests: minor units plus currency, UTC instants with tenant zone, no floats anywhere (OQ-06 answered)". Owner C2 by A650 (plan v1.28, `CR-2026-11-24-data-02-owner`); REQ-DATA-002; Needs PLT-03 (Done) and UND-05; Done when "Property tests pass; lint green". DATA-02 is not Done until merged evidence (the Steward writes status).

## State: pull request to follow (draft); tests pushed red first, one defect found and fixed

- Needs: A729 waives UND-05 as a Need of DATA-02 (on A634 and A650); plan v1.31 makes DATA-02's Needs PLT-03 only.
- No task card exists for DATA-02; the allowlist used: `services/model/tests/test_money_time_properties.py`, `services/core/platform/clock/clock_property_test.go`, `services/core/internal/contracts/money_time_property_test.go` (all new, tests only), `services/model/noema_model/understanding/compiler/lexicon.py` (one fix, below), two READMEs and this file. No contract, pack, migration, workflow or dependency is touched (`hypothesis` and `pgregory.net/rapid` are already direct dependencies).

## Session 2026-11-26 · DATA-02

### Session summary
- What changed: property tests (Hypothesis in the model service, rapid in the core) of REQ-DATA-002, against oracles that do not read the code under test (an exact `Fraction`; the standard library's zone database; a numeral-word writer in the test file). Money: the lexicon's conversion is exact or unread for digits, comma groups, multiplier words, decimals and numeral words; the ceiling 2^53-1 is exact; a sign, an exponent or a stray character is unread; the contract `Money` accepts exactly the integer literals in `0..2^53-1` and no float, quoted number or container; a currency outside `contracts/currencies.json` is refused. Time: every instant is UTC (the clock, the contract `Timestamp` with its `Z`); the lexicon's fixed offset equals the pack's `tenant_zone` (`Africa/Lagos`) over 1990 to 2100; "yesterday" is the previous local day and "this morning" is `[05:00, 12:00)` local, both half-open and the same for one instant written in any zone; a reference instant with no zone is refused. Go: the simulated clock is UTC, adds exactly and never runs backwards; a `Money` literal with a point, exponent, quote or bracket is never decoded; `crypto.CanonicalJSON` writes an in-range amount as digits and refuses one above 2^53-1 and any float64, integral or not.
- Why: REQ-DATA-002 "Property tests on conversion SHALL pass" and A634 and A650 (time half).
- Defect found and fixed (AGENT-23: reproduction first, red CI read, then the fix): `Normaliser._amount` multiplied a `Decimal` under the default 28-digit context, so `1.0000000000000000000000000001 naira` (and any amount with more than 28 significant digits) was rounded to `100` minor units. M5 says excess precision is rejected, never rounded. The conversion now uses exact `Fraction` arithmetic (no `Decimal` is left in `lexicon.py`); an amount that is not a whole number of minor units is unread. Only an input the old code wrongly accepted changes.
- Risks, and how they are contained: the fix is in untrusted-output code; it only narrows (a value that was rounded is now unread, which the pipeline treats as a missing slot, never a guess). The existing 203 lexicon and numeral tests pass unchanged.
- Hand mutants: see the PR body (killed and survived, with the argument for each survivor).
- Debt entries: none. Conflicts raised: none.

### Answers of A729 (the four open questions of this task; DEC-2026-11-26-architect-rulings-a721-a737, interim)
1. **UND-05 Need:** waived on A634 and A650.
2. **Tenant offset:** the pack's `tenant_zone` is the AUTHORITY (OD-P-129); the lexicon's `utc_offset_minutes` must equal it. The test `test_the_lexicons_offset_equals_the_packs_tenant_zone_which_is_the_authority_over_a_century` pins it; a change of zone (GOV-08, the Founder's) changes the pack and the lexicon must follow.
3. **Money arithmetic (M6):** no code exists and nothing is invented here; it stays OPEN until after G1 (OD-P-130). The M10 rows "addition crossing the ceiling" and "NGN plus another currency" have nothing to test until then.
4. **Decoder bounds:** the generated Go decoder is shape-only by design (`tools/codegen/README.md`); every route validates request bodies against the JSON Schema (A655). **C1's confirmation is owed** that the conversation POST does; it is not DATA-02's.

## Pre-work for the next agent
- Exact next step: SR and Codex read the lexicon fix; C1 confirms the conversation POST validates against the JSON Schema (A729 item 4).
- Files to read first: `services/model/tests/test_money_time_properties.py`, `services/model/noema_model/understanding/compiler/lexicon.py` (`_amount`, `_digits`, `_spoken`).
- Do not touch: the generated contracts; `contracts/currencies.json`; the pack's numbers.
