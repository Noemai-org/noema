# Session 2026-11-27 · TRU-07 · A561, A724 (C1, interim, relayed): the Writer enforces the confirmation TTL ceiling

## Session summary
- What changed: `recorder.Writer.ConfirmationTTLCeiling` (the pack's `confirmation_ttl_ceiling_ms` as a duration, from the composition root; `RecorderConfig` passes it through). `checkRequest` refuses a CONFIRM request whose TTL (its expiry minus the Writer's clock) is above the ceiling, and writes none when the ceiling is zero or negative: a missing ceiling never reads as no ceiling. Both are `ErrRequest`, before any transaction opens.
- Why: A561 and A724. `trustlimits.ConfirmationTTLCeilingMS` and the pack member were on `main`, but nothing enforced the ceiling.
- Risks: (1) the composition (`cmd/core`) must set the ceiling from `trustlimits`; it does not compose Confirmations yet, so no CONFIRM request is written in production until it does (fail closed). (2) The intake does not refuse a longer TTL earlier: this is the Writer's refusal only.
- Tests added (REQ-TRU-009; INV-25): `confirmation_ttl_ceiling_test.go` (above, at, and no ceiling). Regression cases: the red at `ce6b6eab8`. Existing worlds and the adapter's test configs now set a 10 minute ceiling. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: `cmd/core` passes `limits.ConfirmationTTLCeilingMS()` as a `time.Duration` into `RecorderConfig.ConfirmationTTLCeiling`.
- Do not touch: `platform/ceilings` (the bound of the bound).

# Session 2026-11-27 · TRU-07 · A758 (C1, interim, relayed): whole-second comparison, clamp and the year 2400

- The TTL is compared with the ceiling in whole seconds (`ceilingSeconds`: the ceiling in ms divided by 1000, clamped to `platform/ceilings` `MaxConfirmationTTLMillis` / 1000 = 600), never as a `time.Duration`: the product of a far-future expiry and `time.Second` wrapped, so a year 2400 expiry was written (red at `6fcd4777f`). Tests: the year 2400, a one hour ceiling held to 600 s, a 1500 ms ceiling read as 1 s. No threshold chosen: the bound is the one already in `platform/ceilings` (AGENT-16).
