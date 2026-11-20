# Status · EDG-03 · cap every response of POST /v1/conversations

State: RED pushed (noema#822); green committed locally, not pushed. Ruling A457 (interim, relayed, recorded in docs#213, not yet merged).

## Session 2026-11-20 (Claude Code, EDG-03)
## Session summary
- What changed (PR links): noema#822. Tests `conversations_cap_test.go`; `conversations.go`: bounded `capture` (limit+1 bytes), fixed-text 500 for any over-long answer, bounded correlation echo.
- Why: `capture` is unbounded and non-200/202 bodies pass through byte for byte; `failStart` does not bound the correlation ID. The UI transport limit is 65,536 bytes (A446).
- Risks, and how they are contained: a fixed-text 500 `INTERNAL_SAFETY_BLOCK` replaces any over-long answer; no upstream text leaves.
- Tests added (REQ, INV, US or EC IDs): REQ-API-001, REQ-INP-001, INV-02, AGENT-13.
- Mutations by hand: 10 mutants, 8 killed; 2 equivalent (`room <= 0` vs `< 0`; an unreachable post-build length guard, removed).
- Regression cases added: over-long 4xx/5xx, exact limit, 200/202 limit, property, bounded capture, bounded correlation.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: read the red job, push the green commit.
- Open questions (owner): status for an over-long upstream answer (500, the safer reading) (EL).
- Files to read first: `services/core/cmd/core/conversations.go`.
- Do not touch: `edge/events` (noema#726 caps SSE frames).
