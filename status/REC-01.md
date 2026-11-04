# Status · REC-01 (Core Engineer 1) · Record append, chain, verification

Updated 2026-11-03. Plan task REC-01: owner C1+AG; REQ-AUD-001; done when "Chain property test passes; runtime role cannot UPDATE or DELETE; tampering and a broken prior hash are both detected".

## State: in review (the property test); not Done until the founders approve and it merges

On `main`: the appender and verifier, the INSERT-only privileges, eight kinds of tampering detected, concurrency, golden vectors. Missing until this pull request: the chain **property** test. Test-only dependency `pgregory.net/rapid` v1.3.0 (DEC-2026-11-03, Noemai-org/docs#94; MPL-2.0, no dependencies).

## Session 2026-11-03 · REC-01

### Session summary
- What changed (PR links): the REC-01 property-test pull request on `test/REC-01-chain-property`.
- Why: REC-01's done-when; G1 needs the property tests green (TST-01 follows).
- Risks, and how they are contained: a new dependency (MPL-2.0), contained by being test-only, unmodified, pinned, and recorded under AGENT-14.
- Tests added (REQ-AUD-001, INV-10, INV-11): four property tests and one example test in `record/chain/property_test.go`. Mutation-checked: dropping the payload-hash check or the prior-hash check in `Verify` fails the tamper property; making `Append` return the head's sequence number fails the interleaving property.
- Regression cases added: the U+0000 payload case.
- Debt entries: none. One documented limit: the tail of a chain can be dropped without `Verify` noticing (REC-07 anchoring).
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: TST-01 (INV-03, INV-07, INV-08, INV-05, INV-18 property tests for Execution).
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-04 · REC-01 · Codex review 5763774796
- **Finding (Major), real:** the tampering properties did `SET session_replication_role = replica` on a pooled administrator connection and released it with the setting still on, so a later iteration (or test) could run with the immutability triggers disabled. **Fixed:** tampering now goes through `propDB.tamper`, which runs the statement in a transaction with `SET LOCAL`, so the setting ends with the transaction, then `assertTriggersEnabled` checks every idle connection the pool can hand out and fails unless it is `origin`. `TestTamperingLeavesNoConnectionWithTriggersDisabled` proves it, including that an ordinary UPDATE of the Record is refused afterwards. Mutation: `SET` instead of `SET LOCAL` fails both that test and the tampering property (run and reverted).
- **Not changed, and why:** `chain_test.go` (on `main`) has the same pattern in two tests; each runs on its own per-test pool that is closed at the end, so nothing leaks to another test. Left alone so this PR does not collide with noema#145, which rewrites that file's world.
