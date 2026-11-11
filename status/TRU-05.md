
# Session 2026-11-08 · TRU-05 slice 2 (noema#168)

## Session summary
- What changed: the Architect's ruling of 8 Nov 2026 on noema#329, all four rules in the database. (d) a CHECK `decision_record_bound_fields`; (a) to (c) in `decision_grant_requires_allow`. Fixtures that insert records and grants directly now carry the bound fields (`testpg.BoundFieldsSQL`, `testpg.GrantBoundSQL`, `testpg.DecisionDocJSON`).
- Tests added (REQ-TRU-008, INV-17): four, in `platform/db/grant_matches_record_test.go`, seen failing before the migration change.
- Risks: a grant fixture that omits the intent hash, policy version or a valid expiry is now refused; every existing fixture was updated and the whole `services/core` suite passes locally.
- Not done: SR's note 5 (owner policy under a non-superuser owner stays unproven). #168 needs a fresh reviewer READY at the new head.

## Pre-work for the next agent
- Branches stacked on #168 (#197, #237, #261, #233) insert records or grants directly in tests: merge this head in and add the bound fields.

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
- **The `decision_record` table and the durable write before any grant.** The table does not exist (migration `0001` says the Trust tables arrive later), and the migration numbers 0004 to 0006 are claimed by noema#145 and 0004 by noema#136. I need a migration number and the Steward's or Architect's confirmation of which comes first before I write it. (Answered: 0009, in noema#168; noema#167 keeps 0008, ruling of 5 Nov 2026.)
- The database trigger that forbids a `decision_grant` row whose decision is not ALLOW (needs the table).
- Grants persisted in the same transaction as their `GRANT_ISSUED` events, and the `KEY_ROTATED` event (need the Record writer wiring).
- The key source: keys come from a file-mounted secret in `cmd/core` (OQ-09 default); loading is not built. The validity (10 minutes, ADOPTED) is a constructor input.
- Policy version (**ruling D1 of 5 Nov 2026, proposed in docs#112 and authoritative only once docs#112 merges (AGENTS.md §2.3); it replaces the Architect's session decision of 4 Nov**, which is superseded and must not be restored; this PR must not merge before docs#112): a grant binds the exact `policy_id@version` it was decided under. Versions are compared only within one policy_id (dotted numbers, 3.10 newer than 3.9); nothing is ever ordered across policy IDs, and there is no "newest across policies". `Step.PolicyID` names the policy; if the basis names one policy it may be omitted, and if it names several and none is named, or the named one is absent, no grant is issued. At execution, E4 voids the grant (`VoidPolicySuperseded`, a RecoveryRequest returns the case to Trust) if the active version of the grant's policy_id differs, is absent, or the grant's `policy_version` does not match the contract pattern; another policy's version never voids a grant. TRU-08's void on publication reads the grant's own policy_id and version, not the whole basis. Tests were written first; mutants of `boundPolicy` and of the E4 check are caught.
- Migration number (decided, 4 Nov 2026): `decision_record` is migration **0009** (noema#168): noema#167 (EVD-01) keeps 0008, ruling of 5 Nov 2026. The table and trigger are written there; the Go write path is not.

## Pre-work for the next agent
- Exact next step: the `decision_record` migration (after the number is settled), the trigger, the Record-first write in one transaction with the grants, and the 2-key rotation test against the database.
- Files to read first: `trust/grants/grants.go`, PERD REQ-TRU-008 and REQ-SEC-005.
- Do not touch: `noema#94`, `noema#90`.

## Session 2026-11-05 (evening) · TRU-05 slice 1 follow-ups from Codex
- A grant now needs a `reconcile.Permission` bound to the step's capability **and target** (`FreezeGate.PermitTarget`, additive in `trust/reconcile/freeze.go`; `Permit` is unchanged and yields a permission that names no target, which opens no grant). The `frozen` flag a caller passes comes from the reconciliation result for that same target; the gate does not read freeze state itself. The pack version of the basis is checked against the grant contract's pattern before signing. Both found by Codex at `9dbb363` and `4546400`; tests first, mutants caught.
- Touches `trust/reconcile` (T6.2, another task) additively; the existing `Permit` tests in `internal/corpus` are unchanged and pass.

# Session 2026-11-08 · TRU-05 signature domain separation

## Session summary
- What changed: ruling of 8 Nov 2026 on noema#292. A grant signature covers `noema/decision-grant/v1`, a 0x00 byte, then the JCS bytes; the confirmation request gets its own label, `noema/confirmation-request/v1`, in `platform/crypto` for the confirmation signer to use. Trust's issuer, Execution's E1 and every test signer use `crypto.GrantSigningMessage`. Golden vectors 1.0 to 1.1 (`grant_signing_vectors`: new value, `domain_label`, `must_not_verify`), with the migration note in `contracts/fixtures/README.md`; the independent Python checker (`tools/check_vectors.py`) agrees with the Go value.
- Tests added (REQ-TRU-008, INV-03): `TestGrantSigningVector` (rewritten), `TestAGrantSignedOverBareJCSOrUnderAnotherLabelIsRefused`, two E1 cases in `TestEachCheckVoidsTheGrantWithItsReason`. Seen failing first.
- Risks: P0 path and a contract fixture change (AGENT-15); both Founders approve. No stored grant exists, so nothing migrates. Stacked on noema#163.
- Not done: the confirmation signer does not exist yet; this PR only reserves its label and message function.
