# Session 2026-11-24 · UND-06 · slice 1 of the lexicon (ruling A634, interim, relayed)

## Session summary
- What changed (PR noema#951): `packs/fintech/lexicons/fin.lex.en_ng.json` gains the sections `numerals` (one to nineteen, twenty to ninety), `joiners` (`and`) and `uncertain_words` (about, around, roughly, approximately), the multipliers `hundred` and `million`, and the direction words `receive`, `credited`, `debited` and the phrases `sent me`, `send me`, `sent to me` (money sent to the customer is INCOMING, EC-A20). `services/model/noema_model/understanding/compiler/lexicon.py` reads numeral words, sets `amount_uncertain`, and matches a direction phrase (longest first) before its words.
- Why: UND-06's Done-when ("20k", "twenty thousand", "20,000" and "20 thousand naira" give one Quantity; the direction slot covers money received; no amount or alias is ever invented) and A634.
- Risks, and how they are contained: INV-02. A spoken number is read only if it is one well-formed number; every other sequence is unread, never repaired and never the nearest number. The quote is bounded at 64 characters, as for digits. A phrase may not be longer than three words. A word may be one kind only (numeral, multiplier, uncertainty word, joiner); two lexicons may not disagree about it (the merge refuses).
- Tests added (REQ-INT-003; EC-A17 to EC-A20; INV-02, INV-21): `tests/test_lexicon_numerals.py`, the SYNTHETIC golden set `tests/fixtures/lexicon_golden.v1.json` (amounts, uncertain amounts, directions), the round trip of every number 1 to 999 (no scale, thousand, million), the refusals at load and at merge. `test_lexicon.py`: "twenty thousand" leaves the list of unread surfaces (it is read now), the other eleven stay.
- Not built, and why: numeral tables for Yoruba, Hausa and Igbo (UND-06 names them; they need a native reviewer, OQ-12 and GOV-10, and nobody invents them), Pidgin numerals (no input needs them yet), negation (no slot reads it yet), and the lexicon files are not pinned in the manifest (the run record pins their merged digest).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the native reviewers' notes (GOV-10) and the yo, ha and ig numeral tables as three more files named by the intent node's `lexicon_refs`; each keeps its own file and is marked SYNTHETIC.
- Files to read first: `lexicon.py`, `tests/fixtures/lexicon_golden.v1.json`.
- Do not touch: the English file's multipliers without a golden row (a multiplier is read for digits too).
