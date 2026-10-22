# noema_model/understanding/resolution

**What it does.** Ranks the principal's own candidate records by deterministic code and applies the auto-resolve rule (REQ-AMB-003). Includes the D5 composed recipient matcher. No model, no clock, no randomness, no I/O. Everything is exact fractions, so a replay gives bit-identical scores.

**REQs.** REQ-AMB-002, REQ-AMB-003, REQ-AMB-007; INV-06, INV-21.

| File | Holds |
|---|---|
| `ranking.py` | `rank(...)`: score = sum of weight x feature, the auto-resolve rule, the content hash, the `CANDIDATES_RESOLVED` payload |
| `config.py` | `RankingConfig`: weights, margin, floor, bounds and tolerances, read from pack data as exact numbers (a float is refused) |
| `matcher.py` | `RecipientMatcher`: aliases, folding, phonetic keys, token order, initials; data from `packs/fintech/lexicons/recipient_matcher.json` |
| `types.py` | plain frozen data: `Candidate`, `Target`, `Context`, `Resolution`, `ResolutionState`, `Why` |

## The rule, as built

Auto-resolve only if all hold: exactly one top score; top minus second at least the margin (second is 0 when alone); top at least the floor; no material flag; no low-ASR key slot. More than `max_candidates_before_escalate` candidates at or above the floor is `TOO_MANY_CANDIDATES`, checked first. Anything else is `AMBIGUOUS` with every failing rule listed in `why_not`. Ranking proposes a state; Trust decides.

Features (each in [0, 1]): `amount` falls in a straight line from 1 (exact) to 0 at the pack tolerance; `temporal` is 1 inside the window and falls to 0 over the pack tolerance; `recipient` is the matcher; `relationship` is 1 when the beneficiary source matched; `type_direction` is 1 when the direction is the expected one. A feature with nothing to compare scores 0, which can only lower a score.

**The numbers are not here.** The weights, margin, floor, limit of four, tolerances and the matcher's tier scores come from pack data. The adopted values are in PERD REQ-AMB-003; the tests use them as test values only.

**Tests.** `tests/test_ranking.py`, `tests/test_matcher.py`.
