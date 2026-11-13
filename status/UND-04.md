# Status · UND-04 (Core Engineer 2) · the core's side of the schema gate and the intent client

Updated 2026-11-11. Plan task UND-04: REQ-INT-001, REQ-INP-002; allowlist `services/core/understanding/**`. The Python half (`services/model`, the compile endpoint) is separate.

## State: in review (PR on `feat/UND-04-core-schema-gate`)

Built: `services/core/understanding` (gate, slot rules, identity overwrite, intent hash, compile client). Constructors only: EL wires them into `cmd/core/compose.go`.

## Open
- CONFLICT: the intent hash over a fractional number (see the PR). Fails closed meanwhile.
- The session fields `authn_ref`, `principal_type`, `asserted_by` are not on the edge's session yet.
- UND-02's core side (the handler for `POST /v1/conversations`): the Input Gateway already exists in `services/model` (435f9c37, fdc47a6f), and the dedupe of REQ-INP-003 needs a table that no migration has. Asked on a `[WAITING: C2 needs Architect]` issue before any code.

## Messages
| Date (UTC) | From | To | Purpose | What the receiver used it for |
|---|---|---|---|---|
| 2026-11-11 | Architect (A146/A147) | C2 | Start UND-02 and UND-04 as constructors EL wires into compose.go (allowlist `inputgateway/**`, `understanding/**`) | This change (UND-04); UND-02 waits on the question above |

# Session 2026-11-11 16:30 WAT · UND-04

## Session summary
- What changed (PR links): the UND-04 core PR on `feat/UND-04-core-schema-gate`.
- Why: REQ-INT-001 (the second schema gate), REQ-INP-002 (identity from the session), the compile client of PERD §39.3.
- Risks, and how they are contained: no Go JSON Schema validator (AGENT-14), so the closed shape is the generated decode and the rest is explicit rules, each with a test; the hash cannot cover fractions, so the gate refuses them (HASH_UNAVAILABLE) rather than hash by another rule.
- Tests added (REQ-INT-001, REQ-INP-002, INV-02, INV-12): `understanding/gate_test.go`, `understanding/client_test.go`.
- Regression cases added: none (new package).
- Debt entries: none.
- Conflicts raised: intent hash over JCS versus fractional confidences.

## Pre-work for the next agent
- Exact next step: decide the hash form; then the edge maps the session fields and wires `Gate` and `Client` into `compose.go`; the caller records PRINCIPAL_OVERRIDE_ATTEMPT from `Admission.Overrides`.
- Open questions (owner): hash form (Architect); UND-02 core scope (Architect).
- Files to read first: `services/core/understanding/README.md`, `gate.go`.
- Do not touch: `platform/crypto` (not on this allowlist).

# Session 2026-11-12 (evening) · UND-04 core · Session.valid() and A163

- **What changed:** `Session.valid()` checked `AssertedBy` with the lower-snake pattern, which refuses a hyphen, so a session asserted by the POC's own identity provider (`noema-poc-idp`) was `SESSION_INVALID` and nothing was admitted. It now uses the contract's `asserted_by` pattern of ruling A163, `^[a-z][a-z0-9_-]*$` (`assertedByRe` in `gate.go`); `principal_type` keeps the lower-snake pattern. README sentence added.
- **Failing first:** `TestTheAssertedByOfASessionFollowsTheContractsPattern` in its own commit, red locally on `noema-poc-idp` and `a-b_c-9` (SESSION_INVALID); green after the fix. Five accepted forms and ten refused (empty, leading hyphen, digit or underscore, upper case, space, dot, slash, trailing newline, a Unicode hyphen). Mutations by hand, each red: the old pattern back; no `$` anchor; a leading digit allowed; a dot allowed; the check removed.
- **Stated dependency, not changed here:** the gate still decodes `intent-object.v1`, whose `asserted_by` pattern has no hyphen (the Go types do not model patterns, so nothing in Go refuses it). The admitted object therefore carries a value that the v1 schema would refuse until `intent-object.v2` (noema#480) merges and the gate moves to it. Moving the gate to v2 is a separate change after #480, with the generated models; the same applies to the model service's schema check.
- Debt: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the compile route and core `Interpreter` under A166 (core signs and hashes after the gate), after this merges.
- Files to read first: `services/core/understanding/gate.go`, `identity.go`.
