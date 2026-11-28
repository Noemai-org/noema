# Session 2026-11-28 · TRU-12 · A710, A724 (C1, interim, relayed): the confirmation store gets the Record events in the composition

## Session summary
- What changed: `decisionRecorderAndStores` builds, over the same core-trust appender as the Recorder, each tenant's `trustpg2.Requests` (that tenant's own Trust login, the ID source, `trustpg2.RecordEvents`). `decisionParts.RequestStores` carries them. With the stores built, the Confirmations gap names only `needRequestRead` and `needConfirmPolicy`; with the Recorder configuration incomplete, or with no tenant logins, there is no store and the gap still names the Record events (fail closed). `decisionInputsFrom` passes the Trust logins.
- Why: A710 and A724. The store appends its events in its own transaction (PERD 32.4), and nothing in the composition gave it the appender.
- Risks: the stores are built but not yet used; the Confirmations adapter (the request read by id and the confirm policy) is a named gap, not guessed. The Trust login is assumed to be the login that may append to the Record as core-trust, as in #1028's tests.
- Tests added (REQ-TRU-009; INV-12): `wire_confirm_events_test.go` (three). Regression cases: the red at `687abea87`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Confirmations adapter over `RequestStores`: the request read by id (`needRequestRead`) and the pack's confirmation expiry and the capability an intent confirms (`needConfirmPolicy`).
- Do not touch: `trustpg2` (merged in #1028).
