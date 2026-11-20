# Status · EDG-03 · cap every response of POST /v1/conversations

State: RED pushed (noema#822); green committed locally, not pushed. Ruling A457 (interim, relayed, recorded in docs#213, not yet merged).

## Session 2026-11-20 (Claude Code, EDG-03)
## Session summary
- What changed (PR links): noema#822. Tests `conversations_cap_test.go`; `conversations.go`: bounded `capture` (limit+1 bytes), fixed-text 500 for any over-long answer, bounded correlation echo.
- Why: `capture` is unbounded and non-200/202 bodies pass through byte for byte; `failStart` does not bound the correlation ID. The UI transport limit is 65,536 bytes (A446).
- Risks, and how they are contained: a fixed-text 502 `DEPENDENCY_UNAVAILABLE` (retryable, as errors.v3 defines the code) replaces any over-long answer (A471, interim, relayed, recorded in docs#216, not yet merged; code as used at `inputgateway/handler.go:45`); no upstream text leaves.
- Tests added (REQ, INV, US or EC IDs): REQ-API-001, REQ-INP-001, INV-02, AGENT-13.
- Mutations by hand: 10 mutants, 8 killed; 2 equivalent (`room <= 0` vs `< 0`; an unreachable post-build length guard, removed).
- Regression cases added: over-long 4xx/5xx, exact limit, 200/202 limit, property, bounded capture, bounded correlation.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: read the red job, push the green commit.
- Open questions (owner): none for the status (A471 settled it).
- Safer readings taken (for the PR body): (1) `failStart` stays 500 `INTERNAL_SAFETY_BLOCK` for an answer the core cannot turn into an investigation (unreadable JSON, a correlation ID the core did not mint): our own fault, A471 speaks only of the over-long answer. (2) `retryable: true`, because errors.v3 defines DEPENDENCY_UNAVAILABLE as retryable and retryability is a property of the code. (3) The message is the gateway's own fixed text for that code. (4) The code constant is the errors.v2 one, as the gateway uses; its string equals errors.v3's.
- Mutations by hand (round 2): 5 mutants, 5 killed. The 7 failing tests in the package need pgsodium and run in CI only.
- Files to read first: `services/core/cmd/core/conversations.go`.
- Do not touch: `edge/events` (noema#726 caps SSE frames).
