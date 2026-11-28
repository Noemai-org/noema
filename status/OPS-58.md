# Status · OPS-58 · re-point the vendored copies to docs bb5ef7a (main red on docs-drift)

## State
- Done in this pull request: the five vendored copies re-pointed to docs commit `bb5ef7a` (docs#253: A770 to A776, AGENTS Review standing) with `tools/vendor/repoint_docs.py`; `status/OPS-56.md` no longer cites the old "A770" (now SR-APP).

## Messages
None.

# Session 2026-11-28 · OPS-58

## Session summary
- What changed (PR links): see the pull request that carries this file.
- Why: main red on `docs-drift` after docs#253 merged (the docs original wins on any difference).
- Risks, and how they are contained: copies and a status note only; the vendor tests pass.
- Tests added (REQ, INV, US or EC IDs): none. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none. Re-point again whenever a docs pull request changes a vendored file.
- Open questions (owner): none.
- Files to read first: `tools/vendor/repoint_docs.py`.
- Do not touch: the copies' bodies by hand.
