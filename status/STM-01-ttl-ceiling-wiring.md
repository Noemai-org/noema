# Session 2026-11-28 · STM-01 · A561, A724 (C1, interim, relayed): the composition gives the Recorder the pack's confirmation TTL ceiling

## Session summary
- What changed: `trustpack.LoadVerifiedWithLimits` keeps the verified limits files the manifest pins (copied; `LimitsFiles` returns a copy); `productionPack` uses it, so the limits are read once, from the same verified bytes (INV-16). `cmd/core` `confirmationTTLCeiling` reads `confirmation_ttl_ceiling_ms` through `trustLimitsOf` and `trustlimits`; `decisionInputsFrom` fills `decisionInputs.ConfirmationTTLCeiling`; `decisionRecorderConfig` passes it to `RecorderConfig.ConfirmationTTLCeiling`.
- Why: the TTL ceiling PR (the Writer's refusal) had no caller that set the value, so no CONFIRM request could be written.
- Risks: no limits kept, or no pack, give a zero ceiling and the Writer writes no CONFIRM request (fail closed); limits that are kept and do not load are an error, as for Clocks (A612). `LoadVerified` is unchanged.
- Tests added (REQ-TRU-007; INV-16, INV-25): `verified_limits_test.go`, `wire_ttlceiling_test.go`. Regression cases: the red at `0aca4b302`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Confirmations adapter (the store, the request read, `RecordEvents`, the confirm policy) still has no composition; this PR adds none.
- Do not touch: `platform/ceilings`, `platform/trustlimits`.
