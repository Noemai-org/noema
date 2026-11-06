# Status · EXE-03 (Core Engineer 1) · idempotency key

Updated 2026-11-05. Plan task EXE-03: owner C1+AG; REQ-EXE-002; Needs CON-03; done when "Property tests: stable, field-sensitive, ignores attempt number; a policy publish never creates a new key".

## State: the key function (`platform/crypto.IdempotencyKey`) and its golden vectors were already on `main`; this slice adds the property tests. In review (draft until CI is green and Codex is READY).

## Session 2026-11-05 · EXE-03

### Session summary
- What changed (PR links): the EXE-03 pull request on `feat/EXE-03-key-properties`: `platform/crypto/idempotency_property_test.go`. No production code.
- Why: the done-when asks for property tests; only an example test (`controller/classify_test.go`) and the golden vectors existed.
- Risks, and how they are contained: tests only.
- Tests added (REQ-EXE-002): four, three with `rapid`: stable and well formed for any input; two different field tuples never share a key (and equal ones always do); each of the seven fields, and each boundary between neighbouring fields, changes the key; the inputs are exactly the seven of PERD 27.2 with none named for an attempt, retry, version, nonce or time (a policy version is therefore never an input).
- Failing first: not applicable, the code already satisfied the properties. Mutations run and reverted instead: policy scope dropped from the hash (1 fails); an `Attempt` input added (1 fails); length prefixes made constant (1 fails, the boundary test; the random-tuple test alone does not find it, which is why the boundary test exists).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: nothing for the key itself. EXE-03 is Done when this merges, unless the Architect wants "a policy publish never creates a new key" proven at the Trust boundary too (policy_scope is chosen by the grant issuer, TRU-05, and must be the policy ID, never a version): that test belongs to TRU-05.
- Open questions (owner): none.
- Files to read first: `services/core/platform/crypto/crypto.go` (`IdempotencyKey`).
- Do not touch: noema#94, noema#90 (held, DEBT-01).
