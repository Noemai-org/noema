# Status · STM-01-recorder-settings · the Recorder's three required settings

Updated 2026-11-20. Owner: Engineering Lead (built by an agent lane; unreviewed). REQ-TRU-008, REQ-TRU-009, REQ-NFR-002; AGENT-16, A330. Ruling A475 (interim, relayed, recorded in docs#216).

## State

- **Red pushed:** `cmd/core/recorder_settings.go` is a stub that accepts everything; `recorder_settings_test.go` fails on the missing behaviour. Implementation is committed locally and not pushed (EL pushes it after reading the red job).
- Settings (whole milliseconds, A330, no default in code, required): `NOEMA_TRUST_GRANT_LIFETIME_MS`, `NOEMA_TRUST_RECORD_LIFETIME_MS`, `NOEMA_TRUST_TX_TIMEOUT_MS`. The area `TRUST` follows the existing `NOEMA_TRUST_DB_TIMEOUT_MS` of `wire_trust.go`.

## Values: what the specifications name, and what is bench-only (Founder to confirm)

Searched (docs commit at 2026-11-20; PERD v1.28 is the file the PERD README lists first and marks Proposed, v1.23 is the one it marks Current; both were searched):

| Source | Grant lifetime | Record lifetime | Transaction timeout |
|---|---|---|---|
| PERD v1.28 and v1.23, REQ-TRU-008 field table | `expires_at` = `evaluated_at` + 10 min [ADOPTED DEC-2026-10-17] | none | none |
| PERD §67.3 table "Grants and confirmations" (OD-P-31) | Grant TTL 10 min | none | none |
| PERD REQ-TRU-009 (confirmation) | not a grant value: confirmation expiry 10 min | none | none |
| PERD §38.2 `decision_grant` and `decision_record` rows | `expires_at` column only, no value | `expires_at` column only, no value | none |
| PERD REQ-NFR-003 latency budgets, §47 | none | none | none |
| SoT §43.2 and the SoT body | none (expiry is a grant field) | none | none |
| Plan v1.26 (TRU-07, TRU-08 rows) | confirmation 10 min only | none | none |
| `packs/fintech/limits/trust_limits.json` | none (it holds `ambiguity_absolute_floor_bp` and `clock_at_risk_remaining_bp` only) | none | none |
| Decision A13 (DEC-2026-11-06) | binds `expires_at` into the record's signature, names no value | none | none |
| Decision A475 (docs#216) | 60 s bench-only if no spec names one | 60 s bench-only | 10 s bench-only |

So: **grant lifetime** is named, 10 minutes (600000 ms), by the PERD; use it, set by Compose or the pack, never in code. **Record lifetime** and **transaction timeout** are named nowhere: the bench-only values of A475 are 60 s (60000) and 10 s (10000). These are Founder decisions to confirm. They are not in code. No evidence page may state a grant lifetime as the product's (a test guards that).

**The gap for the Founder:** A475 gives 60 s as the bench-only grant lifetime for the case where no document names one. The PERD does name one (10 min). The loader follows the PERD. A record that expires in 60 s while its grant lives 10 min is a mismatch the Founder should see: the record would expire before the grant it carries.

## Session 2026-11-20 · STM-01-recorder-settings

## Session summary
- What changed (PR links): see the PR for branch `feat/STM-01-recorder-settings`.
- Why: A475 makes the Recorder's lifetimes required settings; the composition needs one loader.
- Risks, and how they are contained: a mis-set lifetime is a Founder decision, so no default exists; the loader refuses a missing or malformed value and names the setting only, never the value.
- Tests added (REQ-TRU-008, REQ-TRU-009; AGENT-16, A330): `cmd/core/recorder_settings_test.go`.
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: the grant lifetime of A475 (60 s if none named) against the PERD's 10 min; see above. The PERD's value is used.

## Pre-work for the next agent
- Exact next step: the final main.go PR reads `loadRecorderSettings` and passes the three durations to `trustport.RecorderConfig`; Compose sets the three variables.
- Open questions (Founder): confirm the record lifetime and the transaction timeout; confirm the grant lifetime of 10 min; the policy id and escalation set from the pack are not part of this PR (they need pack accessors outside this allowlist).
- Files to read first: `services/core/orchestrator/portadapters/trust/recorder.go` (`RecorderConfig`), `cmd/core/wire_trust.go`.
- Do not touch: `compose.go`, `main.go`.
