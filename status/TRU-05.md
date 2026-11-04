# Status · TRU-05 (Decision Records before grants; HMAC grants over JCS; two active keys)

State: In progress (slice 1 built). Needs REC-01: merged (noema#122, 4 Nov 2026 12:58 UTC), so the task started the same day.

## Session 2026-11-04 · slice 1: grant issuance, the JCS HMAC, key rotation
## Session summary
- What changed: `services/core/trust/grants/{grants.go,grants_test.go,doc.go}`.
- Why: REQ-TRU-008, REQ-SEC-005, REQ-TRU-001; INV-03, INV-25. TRU-05 is the G1 blocker I own (the US-01/US-06 write path needs a grant).
- What it does: `Issuer.Issue(decision, step)` returns the grant for one write plan step of an ALLOW decision with a complete basis, and nothing for any other outcome or an ALLOW without its basis (`ErrNotAllow`). It binds tenant, correlation, decision, principal, intent hash, capability, target, params hash, evidence-set hash, plan hash, plan step, target versions, policy version (which must be one the decision's basis names, INV-09), pack version, `not_before` = the evaluation time, `expires_at` = + the configured validity, a 128-bit CSPRNG nonce, `single_use`, and an optional confirmation event. The shapes of the identifiers are checked against the contract's patterns before anything is signed (the generated Go types check presence, not form). `Keys` holds one or two keys: the first signs, any verifies; a retired key is absent, so its grants are rejected. HMAC-SHA-256 over the JCS form of the grant without `sig`, with `key_id`. An entropy failure is an error, never a panic.
- Tests added (REQ-TRU-008, REQ-SEC-005, INV-03, EC-X09): all-field binding; the six non-ALLOW outcomes, a zero decision, an ALLOW without a basis and one with a minimal basis issue nothing; ten malformed steps refused; 500 grants with distinct IDs and nonces and an entropy failure as an error; a change to any of 23 fields breaks the signature; rotation (old and new key both verify during the overlap, the old key is rejected after retirement, a claimed key that did not sign is rejected); the key set refuses none, three, short secrets, an empty or repeated ID and a shared secret; the signature is over the canonical JSON and a re-spelled equal grant verifies; tamper fuzzing (20,000 seeded byte changes, 0 verified). Seen failing first (did not compile). Mutation: 12 mutants, 11 caught at first, the survivor (the basis check) got a test, then 12 of 12.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Not built (named; TRU-05 is not Done)
- **The `decision_record` table and the durable write before any grant.** The table does not exist (migration `0001` says the Trust tables arrive later), and the migration numbers 0004 to 0006 are claimed by noema#145 and 0004 by noema#136. I need a migration number and the Steward's or Architect's confirmation of which comes first before I write it. **Question to the Architect:** which number do I take, and does the `decision_record` migration go before or after noema#145's?
- The database trigger that forbids a `decision_grant` row whose decision is not ALLOW (needs the table).
- Grants persisted in the same transaction as their `GRANT_ISSUED` events, and the `KEY_ROTATED` event (need the Record writer wiring).
- The key source: keys come from a file-mounted secret in `cmd/core` (OQ-09 default); loading is not built. The validity (10 minutes, ADOPTED) is a constructor input.
- Open (INV-09): a decision can use rules of two policy versions; the grant binds one `policy_version`. The caller names it and `Issue` checks it is one the basis names. Which one the Execution void-on-supersession (REQ-TRU-005) should follow is for the Architect.

## Pre-work for the next agent
- Exact next step: the `decision_record` migration (after the number is settled), the trigger, the Record-first write in one transaction with the grants, and the 2-key rotation test against the database.
- Files to read first: `trust/grants/grants.go`, PERD REQ-TRU-008 and REQ-SEC-005.
- Do not touch: `noema#94`, `noema#90`.
