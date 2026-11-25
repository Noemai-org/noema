# Status · CON-04 (Session A) · evidence_set_hash vectors

Updated 2026-10-24.

## State
- Merged: the `evidence_set_hash` golden vectors (`contracts/fixtures/golden_vectors.json`), a fixtures README, a packs status banner and a reference check (`18bfc62`; DEC-2026-10-16 C1, C4).
- The vectors hold public SHA-256 digests and a synthetic signing-test key; they are the only file the noema gitleaks allowlist exempts, and only for `generic-api-key`.

## Open
- None for Session A.

## Messages
None sent or accepted for this task.

# Session 2026-11-17 · CON-04 (the vector check in CI; the Go side of every family)

## Session summary
- What changed: `services/core/internal/vectors/families_test.go`: Go tests for the three families no Go test read (`intent_hash_vectors` and `plan_hash_vectors`, the JCS-primitive vectors: RFC 8785 form and SHA-256; `binding_hash_vectors` through `trust/confirm.BindingHash`), and `TestEveryVectorFamilyHasAGoTest`, which fails when a family is added to `golden_vectors.json` with no Go test (it names the file for each family and checks the file holds the family key).
- Where each family runs in CI (the answer to "confirm the vector check runs in CI"): Go: the `core` job runs `go test ./...` (`internal/vectors`, `evidence/snapshot`, `record/chain`). Python: `make contracts-validate` runs `tools/contracts/test_check_vectors_negatives.py` (`test_the_committed_fixture_passes` runs `tools/check_vectors.py` on the committed file, every family) in the `contracts` workflow.
- Tests added (REQ-API-001, REQ-EXE-002, REQ-TRU-008; INV-18, INV-21): above. No red run (the vectors already agree); mutant: swapping two keys in `BindingHash` fails `TestBindingHashVectors`.
- Not done: schema-valid `intent_hash` and `plan_hash` vectors (the plan adds them "once CON-01 exists"; CON-01 is merged): inputs for a v3 intent and a real plan are the Architect's to choose; the shared vector file of A259 (masking, after noema#636) is a later PR.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: schema-valid intent and plan vectors, with the Architect's inputs; then A259's shared masking vector file.

# Session 2026-11-25 · CON-04 (schema-valid intent_hash and plan_hash vectors; A672)

## Task card
- Task: CON-04 (shared vectors). Ruling: A672 (interim, relayed, not yet committed): shared Go and Python golden vectors for schema-valid `intent_hash` and `plan_hash`, red first, a small `/contracts` fixtures PR, no schema change.
- REQ: REQ-INT-001, REQ-PLN-002, REQ-API-001. SoT: INV-17 (a grant binds `intent_hash` and `plan_hash`), INV-18 (no hash for a plan that names a prohibited or unregistered capability), INV-21.
- Allowlist: `contracts/fixtures/golden_vectors.json`, `contracts/fixtures/README.md`, `services/core/internal/vectors/families_test.go`, `tools/contracts/test_schema_valid_hash_vectors.py`, `status/CON-04.md`. No schema, no production code, no tool.
- Plan: three new families in the existing vector file (it is the one file the gitleaks allowlist already covers): `intent_hash_v3_vectors` (intent-object.v3), `plan_hash_v1_vectors` (plan.v1), `plan_hash_v1_refused_vectors` (plan.v1 with no hash). Red: both tests and the family names land first, with no family in the file, so they fail on "missing or empty". Green: the inputs, the RFC 8785 form and the hashes, all computed by `tools/check_vectors.py` (written from the PERD text, no shared code with Go), then read by Go through the generated types and `planner.HashDocument` over the pack's registry, and by Python through `validate.py`.
- Faults exercised: a plan with a prohibited capability, an unregistered one and a gap in the step numbers (refused, no hash); a plan with its steps reversed (another hash).
- Record events asserted: none (no Record write on this path).
