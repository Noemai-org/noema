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

## Session 2026-11-10 · REC-09: ruling A115 (the digest commitment)
- What changed (PR links): noema#377: `export.go` (outputs_digest is `sha256(JCS(["noema/record-export/v1", event_id, payload_hash]))`; inputs_digest chains the previous outputs_digest, never a payload hash), README.
- Why: the Security Reviewer's F1 (the unkeyed payload_hash was an offline guessing oracle) and the Architect's ruling A115 on noema#383.
- Tests: failing first at `71b9350` ([run](https://github.com/Noemai-org/noema/actions/runs/42755347161/job/129628332799)): the reviewer test (outputs_digest is the A115 commitment, not the Record's payload_hash) and the guessed-payload test; `TestNoRecordEventIDAppearsAnywhereInTheExport` passes at once (a guard: the earlier code never exported an event id). The Benchmark Owner's worked example of noema#355 item 7 used the old outputs_digest, so its three entry digests change; the whole-digest function (`sha256_of(entries)`) is unchanged and still checked against their example's entries.
- Debt entries: none. Conflicts raised: none.

## Session 2026-11-10 · REC-09: the Benchmark Owner's answers on noema#355 (points 1, 2, 4 and the A115 worked example)
- What changed (PR links): noema#377: `record/export/export.go` (a `VERIFICATION_READ` takes the next `tc_n` of the same chain-order sequence as `EVIDENCE_READ`; `VERIFY` entries list the verification reads at or before them; `READ_EVIDENCE` and `DECIDE` list only evidence reads; `EXPLAIN` lists every id read so far), `table.go` (an `approver` principal reads as `operator`; `auditor`, `regulator` and `admin` stay `system`), README.
- Why: the Benchmark Owner's answer of 10 Nov on noema#355 (accepted my four numbered points; item 3, the fail-closed evidence reference, was withdrawn: no Record payload carries a per-event evidence reference, so the export fails closed on an unmapped event type, an illegal disposition, an unreadable verdict, a broken chain and a tenant mismatch, as it already does).
- Tests: failing first at `b959f8b` (tests only; `core` red: [run](https://github.com/Noemai-org/noema/actions/runs/42757746409/job/129635969245)): `TestAReviewerReconstructsTheDecisionsFromTheExportAlone` (VERIFY entries list `tc_3`, EXPLAIN lists `tc_1..tc_3`), `TestAPersonsConfirmationReadsAsTheOperatorForAnApproverAndTheSystemForOthers`; `TestTheWorkedExampleOfNoema355HasTheSameDigest` now pins the Benchmark Owner's recomputed A115 digests (three outputs, three inputs, the record digest `sha256:280b9ff9...c325f`) and builds the entries with the A115 formula.
- Debt entries: none. Conflicts raised: none.

## Session 2026-11-12 15:30 WAT · REC-09: noema#249 is on main; the test world signs its Record events
### Session summary
- What changed (PR links): noema#377: merged `main` (noema#249 and the signed Record events of DEBT-03 are on it). The export's test world now builds its `chain.Appender` with the version-1 signers of `internal/recordsig` (`recordsig.All`), and the story's `INTENT_PROPOSED` is written by `core-edge`, because `model-service` holds no signing key under OD-P-73's default and cannot append. No library code changed: the neutral actor of `INTENT_PROPOSED` is a function of the event type (`model`), which `TestAReviewerReconstructsTheDecisionsFromTheExportAlone` still asserts.
- Why: after the merge all 22 tests of the package failed at the first append (`no signing key for the emitter`); this is the test fixture catching up with main, not a behaviour change of the export.
- Tests: `go test ./record/export/` passes (8.9 s) on the merged tree.
- Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the Security Reviewer's exposure check (condition 3 of A109), then EL's review.
- Do not touch: anything outside `record/export/**` and this file.
