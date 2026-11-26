# Status · STM-01 · compose Clocks (A612, A684 step 1)

State: red `ce3d0054d` and green committed locally; not pushed.

# Session 2026-11-26 · STM-01

## Session summary
- What changed (PR links): none yet. `cmd/core/compose_clocks.go`: `trustLimitsOf` (the pinned file that is exactly trust-limits.v2, through `trustlimits.LoadPackFile`), `newClocksAdapter`, `buildClocksInput`, `checkPackLimits`. `compose.go`: one call, `checkPackLimits`. `compose_trustinputs.go`: `Clocks` built and filled; no longer an always-gap.
- Why: A612 (the core refuses to start on a bad limits file, no default) and A684 step 1 (flip the Clocks gap).
- Risks, and how they are contained: a bad limits file now stops the process (the intent). Production still gives no list convention or screen, so Clocks stays a named gap in production until those inputs exist. Errors carry no path or value.
- Tests added (REQ-TRU-006, REQ-STM-001; INV-16, INV-19; AGENT-16): `compose_clocks_test.go`. 9 hand mutants, all killed.
- Regression cases added: the same tests.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when the production list convention (ListEntities), the screen and the item reader are supplied, `buildTrustInputs` builds Clocks with no further change.
- Open questions (owner): which pinned file is the trust limits (chosen by content: exactly one must load as v2; EL to confirm or name a path rule).
- Files to read first: `cmd/core/compose_clocks.go`.
- Do not touch: `platform/trustlimits`, `trust/**`.
