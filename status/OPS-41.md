# Status · OPS-41 · re-point the vendored copies to the docs commit that merged docs#244

## State
- Done in this pull request: `AGENTS.md`, `CLAUDE.md` and the three vendored extracts re-pointed to docs commit `1a257a2` with `tools/vendor/repoint_docs.py`. The only body change is the review-standing paragraph of AGENTS.md §11 (A656, A657, A669, A670), which docs#244 committed.

## Messages
None.

# Session 2026-11-25 · OPS-41

## Session summary
- What changed (PR links): see the pull request that carries this file.
- Why: ruling A671 (the noema copy of AGENTS.md goes in a pull request once docs#244 merges, stating the docs commit it was taken from).
- Risks, and how they are contained: copies only; the `docs` original wins on any difference. The drift and re-point tests pass (19 tests).
- Tests added (REQ, INV, US or EC IDs): none; `tools/vendor` tests run unchanged.
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none.
- Open questions (owner): none.
- Files to read first: `AGENTS.md` §11 "Reviews".
- Do not touch: the copies' bodies by hand; re-point them with the tool.
