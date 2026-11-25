# Session 2026-11-24 06:00 WAT · UND-19

## Session summary
- What changed (PR noema#936): `RecordingTransport` (`benchmark/compile_acceptance/build.py`) charges 0 for a call that ends in a local pool refusal with nothing sent, and its projection for every other failure (A618, interim, relayed). The recording keeps `projected_micros` (what the call would have cost) and records `cost_micros` 0.
- Why: in compile run 44521380842 a pool-refused call was charged 2,230 micro-dollars although nothing was sent (open question in `status/UND-16-pool-refused.md`).
- The rule: nothing was sent only when the refusal has `local is True` and kind `UNAVAILABLE`, the one refusal the transport raises before a worker takes the call. A `local` flag on another kind, a timeout, a rate limit, a malformed reply, a non-local `UNAVAILABLE` and any case where it is unknown whether something was sent keep the projection (doubt charges).
- Risks, and how they are contained: a benchmark-only accounting change; no budget cap, threshold, pool size or provider default changes (AGENT-16); `settle(0, projected)` adds 0, so the guard cannot undercount a call that was sent.
- Tests added (REQ-INT-001, REQ-INT-002; AGENT-16): `services/model/tests/test_compile_acceptance_build.py` (charged zero; doubt charges across six failure shapes; a refused call does not use up the budget of the next; the real-pool test now asserts the charge).
- Regression cases added: the same tests.
- Hand mutants (6, all killed): always 0; never 0; local flag ignored; kind check dropped; recording says projected; guard still settles at the projection.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for this item. The `prior_spent_micros` the Founder passes to the next run comes from the run's `summary.md`; a refused call no longer adds to it.
- Files to read first: `benchmark/compile_acceptance/build.py` (`RecordingTransport.post`).
- Do not touch: the guard's caps.
