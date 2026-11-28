# Status · OPS-56 · re-point the vendored copies to docs 1049dbc (main red on docs-drift)

## State
- Done in this pull request: the five vendored copies re-pointed to docs commit `1049dbc` (docs#252: A753 to A770, one stacking rule in AGENTS §11) with `tools/vendor/repoint_docs.py`.

## Messages
None.

# Session 2026-11-28 · OPS-56

## Session summary
- What changed (PR links): see the pull request that carries this file.
- Why: main red on `docs-drift` after docs#252 merged (the docs original wins on any difference).
- Risks, and how they are contained: copies only; the vendor tests pass.
- Tests added (REQ, INV, US or EC IDs): none. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none. Re-point again whenever a docs pull request changes a vendored file.
- Open questions (owner): none.
- Files to read first: `tools/vendor/repoint_docs.py`.
- Do not touch: the copies' bodies by hand.
