# Status · EXE-01 (Core Engineer 1) · capability registry loader and the prohibited-capability scan

Updated 2026-11-03. Plan task EXE-01: owner C1+AG; REQ-CAP-002 (and REQ-CAP-001 for the descriptors); Needs PCK-02; done when "Scan and property test green".

## State: in review (draft until checks are green at the final head and Codex has reviewed)

**Needs check.** The board shows PCK-02 as "In progress"; the Architect's work order of 3 Nov put EXE-01 in my queue. What EXE-01 relies on from PCK-02 is delivered (the references, risk-class and prohibited-node checks, commit ce8897e); the three checks PCK-02 still lacks concern the pack, not the loader. I started under standing order 6 (inside the task, reversible) and recorded it here and in the PR.

**Dependency.** The property test uses `pgregory.net/rapid`, the same two `go.mod`/`go.sum` hunks as noema#122 and #123. This PR cannot merge before Noemai-org/docs#94.

## Session 2026-11-03 · EXE-01

### Session summary
- What changed (PR links): the EXE-01 pull request on `feat/EXE-01-registry`.
- Why: REQ-CAP-002 (gate: a monetary capability with a callable code path = 0); G1 path.
- Risks, and how they are contained: the registry decides nothing about *which* capabilities are prohibited (pack data, INV-14); it refuses a monetary-write descriptor that is not marked, and every incomplete or duplicate descriptor. The scan can only see Go identifiers and string literals, not behaviour; the Tushe credential test (AT-level) covers the rest.
- Tests added (REQ-CAP-001, REQ-CAP-002, INV-05, INV-18): `registry_test.go`, `property_test.go`, `scan_test.go`.
- Regression cases added: none (new code). Mutations run and reverted: Callable ignoring `prohibited`; the limits completeness check skipped; the monetary-write rule off; a planted `InitiateRefund` in `adapters/`.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: PLN-01 (planner, canonical plan hash, INV-18) uses `Registry.Callable`; a plan step must hold a `Callable`, so an unregistered or prohibited ID cannot be hashed. Then the INV-05 and INV-18 property tests of TST-01.
- Open questions (owner): whether Trust T4 should call `IsProhibited` or `Callable` (Engineering Lead; TRU-02).
- Files to read first: `services/core/execution/registry/README.md`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).
