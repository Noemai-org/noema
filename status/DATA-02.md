# Status · DATA-02 (Core Engineer 2) · money and time representation: property tests

Updated 2026-11-26. Plan task DATA-02 (week 7, workstream SEC): "Money and time representation tests: minor units plus currency, UTC instants with tenant zone, no floats anywhere (OQ-06 answered)". Owner C2 by A650 (plan v1.28, `CR-2026-11-24-data-02-owner`); REQ-DATA-002; Needs PLT-03 (Done) and UND-05; Done when "Property tests pass; lint green". DATA-02 is not Done until merged evidence (the Steward writes status).

## State: pull request open (noema#1014, ready for review); tests pushed red first, two defects found and fixed (excess precision, A758 digit runs)

- Needs: A729 waives UND-05 as a Need of DATA-02 (on A634 and A650); plan v1.31 makes DATA-02's Needs PLT-03 only.
- The task card for DATA-02 is the one below (written here from the plan row, because no owner-written card was found; a Founder or the Steward confirms it). The allowlist used: `services/model/tests/test_money_time_properties.py`, `services/core/platform/clock/clock_property_test.go`, `services/core/internal/contracts/money_time_property_test.go` (all new, tests only), `services/model/noema_model/understanding/compiler/lexicon.py` (one fix, below), two READMEs and this file. No contract, pack, migration, workflow or dependency is touched (`hypothesis` and `pgregory.net/rapid` are already direct dependencies).

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

## Session 2026-11-27 · C2 (A758)

### Session summary
- Order (A758, relayed; not yet committed as a ruling, so this section and the PR body are its record): "digit runs over 4300 characters make the amount unread, regression case."
- What changed (this PR, noema#1014): `Normaliser._digits` returns `None` (the amount is unread, never an error) when the number has more than `MAX_DIGIT_RUN` = 4300 digits. CPython refuses to convert a longer digit string and `Fraction` raised `ValueError`; the public seam's 64-character quote limit (`MAX_SURFACE`) hid it, so the defect was latent in `_digits` (INV-02, INV-21). The count is of digits, not of the commas that group them.
- Red first, read whole: `3d81a92f4`, `model` job 140663558962: 5 failed (`{d}`, `{d}.5`, `1.{d}`, `{d}k`, `{d}{d}`), 3804 passed, every one `ValueError: Exceeds the limit (4300 digits)`. Green `7c361c6b6`: 26 runs, all passed after one re-run of `tests (2)` (the first run failed `execution/ledger` `TestTheContextDeadlineStopsTheAttempts`, a timing test in a package this diff does not touch; the re-run passed: `core` 140692619011, `tests (0..2)` 140689996369 / 140689997072 / 140689995136, `static` 140690051872, `model` 140687154756, `contracts` 140687154745, `checks` 140687155306, `arch-lint` 140687154417, `regression-link` 140687155183).
- Tests added (INV-02, INV-21): `test_a_digit_run_over_4300_characters_is_unread_and_never_an_error` (six shapes), `test_the_digit_reader_still_reads_a_run_at_the_limit`, `test_a_quoted_amount_with_a_run_over_4300_characters_is_unread_at_the_seam`, `test_the_limit_counts_the_digits_and_not_the_commas_that_group_them`. The "1,{d}" shape and the seam test pass before the fix (characterisation, said here).
- Hand mutants: limit off (no guard), limit one digit high, guard after the conversion, count with commas: all killed.
- Debt entries: none. Conflicts raised: none.

## Task card (PERD §61.4)

```
DATA-02  Money and time representation tests: minor units plus currency, UTC instants with tenant zone, no floats anywhere (OQ-06 answered)
Priority: P1 (REQ-DATA-002)
Requirement IDs: REQ-DATA-002; INV-02, INV-21 (SoT §44, §45), DEC-2026-10-16 M5, A729
Rationale: money is an integer in minor units with a currency and time is a UTC instant with the tenant's zone, so no amount or time is ever a float, rounded, or read from a guess.
Modules touched (allowlist): services/model/tests/test_money_time_properties.py, services/core/platform/clock/clock_property_test.go, services/core/internal/contracts/money_time_property_test.go (tests only); services/model/noema_model/understanding/compiler/lexicon.py (the defects the tests found: excess precision, A758 digit runs); the READMEs of clock and compiler; status/DATA-02.md. Nothing else.
Contracts: none; bump required? N
Inputs/outputs: customer text (untrusted) in; a Money (integer minor units plus currency) or an unread value out.
Acceptance criteria: plan row DATA-02 "Property tests pass; lint green"; an amount with excess precision or a digit run over 4300 characters is unread, never rounded or an error (M5, A758).
Tests to write FIRST (AGENT-11): the property tests in the three files (red at 3d81a92f4 for A758).
Faults to exercise (AGENT-12): none (pure functions); the hostile inputs are generated (Hypothesis, rapid).
Dependencies: PLT-03; UND-05 waived (A634, A650, A729).
Out of scope: money arithmetic (OD-P-130, after G1); any contract, pack, workflow or dependency change.
Definition of done: tests green; lint green; the PR answers the SoT §52 questions
```

## Pre-work for the next agent
- Exact next step: SR and Codex read the lexicon fix; C1 confirms the conversation POST validates against the JSON Schema (A729 item 4).
- Files to read first: `services/model/tests/test_money_time_properties.py`, `services/model/noema_model/understanding/compiler/lexicon.py` (`_amount`, `_digits`, `_spoken`).
- Do not touch: the generated contracts; `contracts/currencies.json`; the pack's numbers.
