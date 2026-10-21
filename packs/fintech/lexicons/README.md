# packs/fintech/lexicons

**Implements:** REQ-CAP-001, REQ-AG-001, REQ-AG-002, REQ-NFR-005, REQ-AMB-003 (the D5 recipient matcher).
**Owning task:** PCK-01 (the rewrite of the fintech pack) for the amount and time lexicons; T6.3 of DEC-2026-10-20 (session D) for `recipient_matcher.json`.

Packs are data, loaded and never imported as code (SoT §51).

## `recipient_matcher.json`

The data of the composed recipient matcher (REQ-AMB-003, D5). The engine is `noema_model.understanding.resolution.matcher`;
nothing about names is in code. A pack for another industry brings its own file and the engine does not change.

| Section | What it carries | Used for |
|---|---|---|
| `tiers` | The score, as an exact fraction, of each kind of token match: `exact`, `folded`, `alias`, `phonetic`, `initial`. Non-increasing in that order (the loader checks). | The Recipient feature in [0, 1] |
| `fold` | Letters that do not decompose (Hausa ɓ ɗ ƙ ƴ, apostrophes), on top of NFKD and combining-mark removal | Transliteration and diacritic folding (Adébáyọ̀ = Adebayo) |
| `nicknames` | A short name and the full names it is short for. Nickname to full name only: two full names sharing a nickname are not aliases of each other (Ade is short for Adebayo and for Adewale; Adebayo is not Adewale) | Alias match |
| `variants` | Spellings of one name; every pair in a group is an alias | Alias match |
| `phonetic` | Ordered substitutions, the vowels dropped after the first letter, and the shortest key allowed to match | Voice path only; the caller switches it on for voice input |

**Every entry is marked `[synthetic seed]`** (T6.3): invented for the POC, not reviewed by a native speaker (PERD REQ-INT-003
dependency, owner: founder). The loader refuses an entry without a mark. The tier scores are starting values: they move only
through a benchmark run and a founder decision (AGENT-16).

Token order, initials ("T. Adeyemi") and "first name only" are handled by the engine, not by data.

## Not here yet

`fin.lex.en_ng`, `fin.lex.pcm`, `fin.lex.yo`, `fin.lex.ha`, `fin.lex.ig` (amounts, numerals and relative times, REQ-INT-003) are
named by the intent node and belong to UND-05.
