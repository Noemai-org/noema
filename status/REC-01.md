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
