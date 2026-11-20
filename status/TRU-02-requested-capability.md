# TRU-02-requested-capability · RequestedCapability is a field of AdmittedIntent (A370)

State: noema#801 (draft until the green is pushed and read). Tier 1 (trust). Built by Core Engineer 2 on 19 Nov 2026 from the Founder's relay of A370 (interim, not committed; the record is in docs#211).

# Session 2026-11-19 18:00 WAT · TRU-02

## Session summary
- What changed (PR links): noema#801. `intake.AdmittedIntent.RequestedCapability`; `trust.StoredIntents.Get` fills it from the stored document and the pack (a Reference slot with `references: capability`); `Verify` holds it (`sameIntent`); `trustpack.Remedies` and the `remedies` argument of `NewCapabilities` removed, `Requested` and `Authorized` read the field.
- Why: A370: "RequestedCapability becomes a field on AdmittedIntent, replacing the second port. It is a follow-up PR before wiring."
- Risks, and how they are contained: a capability slot that cannot be read is `ErrStoredIntent` (UNKNOWN), never "none"; T4 and the authorized set refuse unknown and prohibited capabilities as before; no schema change (derived at read from the hash-covered document); `cmd/core` untouched.
- Tests added (REQ-TRU-004, REQ-TRU-003, REQ-CAP-002; INV-14, INV-16, INV-17): `intents_capability_test.go` (7 tests, real PostgreSQL) and four changed or new tests in `trustpack/capabilities_test.go`. The failed-store tests of `Requested` and `Authorized` are replaced: there is no store read any more. `TestAuthorizedRefusesAnUnknownOrProhibitedCapabilityWithAnError` no longer lists the empty string, which as a field means "none".
- Regression cases added: none (no bug fix).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's wire_trust calls `trustpack.NewCapabilities(pack, escalationGraphID)`. Noema#781 (`NeedsWrite`, A364 `write_when`) needs slot values: read them from the stored document the way `requestedCapability` does, or add a field the same way; not decided here.
- Open questions (owner): whether an AMBIGUOUS capability slot should reach clarification as "none" plus the ambiguity flag instead of `ErrStoredIntent` (Architect).
- Files to read first: `trust/intents.go` (`requestedCapability`), `trustpack/capabilities.go` (`requested`).
- Do not touch: `cmd/core`, `trust/intake` beyond the field.

## Hand mutants (local, on the DB-less tests; each applied, tested, restored; `diff` against the good copy empty after the run)
11 of 12 killed locally: absent read as an error (M1); AMBIGUOUS accepted (M2); no type check (M3); empty value accepted (M4); two different values accepted (M5); loop stops at the first missing slot (M6); sorted capability slots dropped (M8); vocabulary test inverted (M9); `requested` always present (M10); field ignored (M11); `Get` does not set the field (M13). **M12 survives locally** (`sameIntent` ignores the field): its guard is `TestVerifyRefusesAnAdmittedIntentWhoseCapabilityIsNotTheStoredOne`, a PostgreSQL test, and the local container has no `pgsodium`, so the database-backed tests run only in CI. Their result at the green head is the evidence for M12 and for the seven tests of `intents_capability_test.go`.
