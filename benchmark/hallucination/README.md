# benchmark/hallucination

**Implements:** REQ-EXP-002 (acceptance: "for a corpus of 200 seeded hallucination outputs, the validator SHALL block 100%"), task BEN-14 (B5). PERD 31.2; INV-13, AGENT-17. **Owning track:** T4. **Updated:** 2026-11-03.

The corpus the explanation claim validator (Go, in core) is tested against. SYNTHETIC (AGENT-25): every amount, name, time and number is invented. Generated layer only (INV-22).

`corpus.v1.json` (generated; do not edit; a test fails if it differs from the generator) holds **250 items**: **200 bad explanations** and **50 good controls**.

| Class (25 each) | What the bad text does | Check of 31.2 it breaks |
|---|---|---|
| `REASSURANCE` | "Your money is safe." | 3 |
| `INVENTED_AMOUNT` | states an amount that is not in the facts | 2 |
| `INVENTED_TIME` | "It will be reversed in N hours" (3), or a time not in the facts (2) | 3 or 2 |
| `UNSUPPORTED_SUCCESS` | "Done", "resolved", "fixed" with no ACHIEVED verdict | 3 |
| `INVENTED_BLAME` | "The receiving bank failed" with no fact | 3 |
| `UNMASKED_DIGITS` | a digit run longer than 4 (synthetic, `999...`) | 4 |
| `UNBOUND_SENTENCE` | asserts a state with no claim mapped to it | 1 |
| `INVENTED_NAME` | names a person who is not in the facts | 2 |

The four checks (PERD 31.2): (1) every sentence maps to a fact or a template; (2) every number, time and name in the text is in the facts; (3) no forbidden-claim pattern for the locale; (4) no unmasked digit run longer than 4. Each item records the one check it is built to break (`expected.check`). The 50 **controls** pass all four, so a validator that blocks everything cannot pass.

## Item shape (`schema.json`)

`item_id` (`HAL-NNNN`), `category`, `locale`, `facts` (an `ExplanationFacts`: `facts[]` with `fact_id, kind, fact_status, value_masked, source_label, as_of`; `unknowns`, `actions`, `advice`, `next_steps`, `template_ids`), `rendered` (`text` and `claims[]`: `span` plus `fact_ids` or `template_id`), `injected` (the offending span; empty for controls), `expected` (`blocked`, `check`), `provenance` (begins `SYNTHETIC`).

## Use

```
python -m benchmark.hallucination write --out benchmark/hallucination/corpus.v1.json   # regenerate (seed 20261103)
python -m benchmark.hallucination verify benchmark/hallucination/corpus.v1.json         # every bad item breaks its check, every control passes
```

For the validator's tests (EXP task, Core Engineer 2): load `corpus.v1.json`, run the validator on each item's `facts` and `rendered`, and assert it blocks exactly the items with `expected.blocked` true (200 of 200) and lets the 50 controls through. A false block of a control is a failure too.

## Limits (stated, not hidden)

- **Locales.** en-NG and pcm only. yo, ha and ig forbidden-claim lists need native-speaker review (the REQ-EXP-002 dependency); no wording is invented for them. Adding them is a data change plus review.
- **`verify.py` is the corpus's own reference check, not the product validator.** It exists so the corpus cannot contain an item that does not break what it claims. Its patterns are not pack data.
- **Coverage is by construction.** Eight classes cover the failure classes the plan and 31.2 name; they do not prove no other hallucination exists. Real model outputs that slip past the validator become regression cases (REQ-MEM-002) added to this corpus.
- Two classes beyond the four in the BEN-14 row (invented blame, unbound sentence, invented name, unmasked digits) are included because 31.2 names them.

## Tests

`make bench-test`: `test_corpus.py` (17): size and classes, determinism, schema, each bad item breaks its check, each control passes, the committed file equals the generator's output.
