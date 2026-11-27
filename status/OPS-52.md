# Status · OPS-52 · re-point the vendored copies to docs f94117b (main red on docs-drift)

## State
- Done in this pull request: the five vendored copies re-pointed to docs commit `f94117b` with `tools/vendor/repoint_docs.py`; `--check --ignore-commit` passes. The `docs-drift` job on `main` (run at `454ef09`) failed because docs#249 and docs#250 merged and `AGENTS.md` here differed from docs (the two §15 lessons of docs#249).

## Messages
None.

# Session 2026-11-26 · OPS-52

## Session summary
- What changed (PR links): see the pull request that carries this file.
- Why: main red on `docs-drift` (the docs original wins on any difference).
- Risks, and how they are contained: copies only; the vendor tests pass.
- Tests added (REQ, INV, US or EC IDs): none. Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none. Re-point again whenever a docs pull request changes a vendored file.
- Open questions (owner): none.
- Files to read first: `tools/vendor/repoint_docs.py`.
- Do not touch: the copies' bodies by hand.
