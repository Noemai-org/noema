# EXP-01 · a pack template key per part of the answer (A271)

# Session 2026-11-17 · EXP-01 · the mechanics (EL; A271 interim, relayed, not yet committed)

## Session summary
- What changed: `explanation.Part`, `Parts()` and `FallbackPart` (a part is rendered from `<outcome_id>/<classification>/<part>`; a part outside the closed list, or one the pack does not hold, is `ErrNoTemplate` and nothing is borrowed from the summary, another part or another class); `Validate` accepts a template that is the facts' own or one of its parts and nothing else (`selects`); `tools/packcheck` allows part keys (`TEMPLATE_PARTS`, optional for now, non-empty when present) and refuses an unknown part, a part of a key the pack lacks, a deeper path and an empty text; a test ties the two lists (Go and Python) together. The summary key and `Fallback` are unchanged.
- The closed list: found, not_known_what, not_known_why, advice_do, advice_do_not, next_what, next_when (the members of customer-answer.v1 besides `done.summary` and `case_ref_masked`). The Architect's A271 text is not committed; if it names other parts or another key shape, this changes.
- NOT here: the pack's sentences. The wording of a customer-facing sentence (and advice such as "do not send the money again") is the Founder's and a native speaker's (REQ-EXP-002), so no sentence was authored; `en-NG.json` is unchanged and packcheck requires no part yet. Once the wording is approved, a follow-up commit adds the sentences and makes every part required (the line in `check_templates`).
- Tests (REQ-EXP-002, INV-13, AGENT-17): parts listed once and closed; a part is rendered from its own key with its template ID and claim; a missing part is blocked and never borrowed; the summary lookup never returns a part; a part fills its slots by the same rule; Validate refuses another class, another outcome, an unknown part and a deeper path (loaded on purpose so only the rule can refuse them); packcheck cases. Red locally (the type and function did not exist) and one push; four mutants killed (prefix match, no closed-list check, any part accepted, and the Python lists drifting).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the sentences (Founder and native speaker), then require every part in `check_templates`, then the investigation route builds each `answer.Text` through `FallbackPart`.
- Open questions (owner): the exact A271 key shape and part list (Architect); who writes the 7 parts x 35 keys of `en-NG` (Founder).
