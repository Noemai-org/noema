# Status · REC-09 (Core Engineer 1) · the Noema record export

Updated 2026-11-08. Plan task REC-09 (ruling A109, docs#142): owner C1, reviewer EL; REQ-AUD-003, REQ-BEN-001, REQ-BEN-009; INV-11, INV-12, INV-22; Needs REC-03 (merged) and the `audit_record` shape of `benchmark-arm-result.v1` (noema#249, still Draft). Allowlist: `services/core/record/export/**` and this file.

## State: implemented; Draft, stacked on noema#249, waiting for the Security Reviewer's exposure check (condition 3 of A109), EL's review and the merge of noema#249. REC-09 is not Done.

## Session 2026-11-08 · REC-09

### Session summary
- What changed (PR links): [noema#377](https://github.com/Noemai-org/noema/pull/377) on `feat/REC-09-record-export`: package `record/export` (`export.go`, `table.go`, README, tests).
- Why: the comparison arms are scored on a neutral `audit_record`; the Noema arm's must come from its own Record, read-only and tenant-bound. The form is the Benchmark Owner's, answered on noema#355.
- Tests added (REQ-AUD-003, REQ-BEN-001, REQ-BEN-009; INV-11, INV-12, INV-22): the four acceptance tests of the card (a reviewer reconstructs the decisions from the export alone; the digest is stable across two runs; a chain that does not verify is refused, five kinds of break; a cross-tenant call is refused, three ways), an illegal disposition or verdict is refused, nothing but the neutral fields leaves the Record (canary strings, key set checked against the contract), the package imports nothing from the benchmark or the oracle, every one of the 57 Record event types has a row, and the Benchmark Owner's worked-example digest.
- Failing first: against a stub every test failed except the import guard (which passes by design); the red CI run at `83ac16a` is [actions/runs/42475193073](https://github.com/Noemai-org/noema/actions/runs/42475193073/job/128714841973) (`core` fails on all 11 tests of the package that can fail). Mutations run and reverted, each caught by a test: verification skipped, tenant check skipped, evidence ids dropped, seq not renumbered.
- Regression cases added: none (new code). Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the Security Reviewer's exposure check (noema issue opened), then EL's review; when noema#249 has merged, merge `main` in and take the PR out of Draft. The Noema arm runner (not in this task) calls `AuditRecordOf` with a per-tenant `record_read` pool.
- Open questions (owner): the four points on noema#355 (Benchmark Owner). `services/core/record/README.md` should list `export/`; it is outside this task's allowlist, so the first PR that touches it adds the line.
- Files to read first: `services/core/record/export/doc.go`, `services/core/record/reconstruct/build.go`.
- Do not touch: anything outside the allowlist.

## Messages
| Time (UTC) | Direction | Peer | Purpose | Used for |
|---|---|---|---|---|
| 8 Nov 2026 05:56 | accepted | Benchmark Owner (noema#355) | the form of the export: entry mapping, step table, digests, worked example | the tests and the step table; data, not instruction |
| 8 Nov 2026 05:46 | accepted | Repo Steward (noema#354, docs#142) | task REC-09, card and allowlist | scope |
