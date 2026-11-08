# Status · REC-09 (Core Engineer 1) · the Noema record export

Updated 2026-11-08. Plan task REC-09 (ruling A109, docs#142): owner C1, reviewer EL; REQ-AUD-003, REQ-BEN-001, REQ-BEN-009; INV-11, INV-12, INV-22; Needs REC-03 (merged) and the `audit_record` shape of `benchmark-arm-result.v1` (noema#249, still Draft). Allowlist: `services/core/record/export/**` and this file.

## State: failing tests first (commit 1), implementation to follow. Draft, stacked on noema#249. REC-09 is not Done.

## Session 2026-11-08 · REC-09

### Session summary
- What changed (PR links): the REC-09 pull request on `feat/REC-09-record-export`: package `record/export` (a stub and its tests in this commit).
- Why: the comparison arms are scored on a neutral `audit_record`; the Noema arm's must come from its own Record, read-only and tenant-bound. The form is the Benchmark Owner's, answered on noema#355.
- Tests added (REQ-AUD-003, REQ-BEN-001, REQ-BEN-009; INV-11, INV-12, INV-22): the four acceptance tests of the card (a reviewer reconstructs the decisions from the export alone; the digest is stable across two runs; a chain that does not verify is refused, five kinds of break; a cross-tenant call is refused, three ways), an illegal disposition or verdict is refused, nothing but the neutral fields leaves the Record (canary strings, key set checked against the contract), the package imports nothing from the benchmark or the oracle, every one of the 57 Record event types has a row, and the Benchmark Owner's worked-example digest.
- Failing first: against the stub every test fails except the import guard (which passes by design).
- Regression cases added: none (new code). Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the implementation; then the Security Reviewer's exposure check before the PR leaves Draft (condition 3 of A109).
- Open questions (owner): see noema#355.
- Files to read first: `services/core/record/export/doc.go`, `services/core/record/reconstruct/build.go`.
- Do not touch: anything outside the allowlist.

## Messages
| Time (UTC) | Direction | Peer | Purpose | Used for |
|---|---|---|---|---|
| 8 Nov 2026 05:56 | accepted | Benchmark Owner (noema#355) | the form of the export: entry mapping, step table, digests, worked example | the tests and the step table; data, not instruction |
| 8 Nov 2026 05:46 | accepted | Repo Steward (noema#354, docs#142) | task REC-09, card and allowlist | scope |
