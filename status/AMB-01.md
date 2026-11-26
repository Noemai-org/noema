# Status · AMB-01 (Core Engineer 2) · deterministic ranking, the 0.35 margin, the 0.70 floor, the crowd rule, `CANDIDATES_RESOLVED` with scores

Updated 2026-11-26. All data is SYNTHETIC.

## State
- Order of the Architect, 26 Nov 2026: "Start AMB-01 now that #960 merged" (relayed; not yet committed as a ruling, so this file and the PR body are its record). Plan row AMB-01: owner C2+AG; REQ-AMB-003, REQ-AMB-002; Needs EVD-04 (noema#960, merged); Done when: "Same inputs give the same order 1,000 times; boundary tests at margin 0.349, 0.35 and 0.351 pass".
- **What already existed on main.** The ranker is `platform/candidaterank` (TRU-15; A555, A559, A560, A566, A583): integer basis points, floor division, no float, the margin, the floor, the tie and the crowd rule decided on the exact scaled sums, every threshold an input from the pack, the shared vectors in `contracts/fixtures/candidate_ranking_vectors.json`. It is Go and it is the authority (A559); the Python ranker is the reference. AMB-01 therefore builds no second ranker.
- **Built in this PR (noema#1012):** (1) `Resolution.AuditPayload()`: the payload of `CANDIDATES_RESOLVED` with the scores (REQ-AMB-002 "IDs and scores, masked"; REQ-AMB-003 "Scores and feature values recorded"): the state, the reasons it did not resolve, the counts, the top, second and margin in basis points, and for each candidate in ranked order its ID, score and the five feature values; a resolved ID only for AUTO_RESOLVED; integers and strings only (PERD 32.1); no name, amount, time, digit or direction of a candidate (A676, AGENT-13). `Scored` gains the five feature values (additive). (2) The Done-when tests by name: the 1,000-run determinism proof (a stride permutation of the rows each run, ties at the top, the same order and the same payload bytes), the margin at 0.349, 0.35 and 0.351 (3490, 3500, 3510 basis points), the floor at 0.699, 0.70 and 0.701, four and five candidates above the floor, and exactness to one scaled unit at the margin and the floor (a configuration with weights that have no common factor), one candidate scoring 0 is not a tie.
- **No threshold, margin, weight or default is set here.** The tests take them from a test configuration that equals the pack's (the existing `TestThePacksOwnThresholdsAreTheOnesTheVectorsUse` pins the pack to the vectors).

## Not built here, and who owns it (the named blockers)
1. **Appending the event.** Nothing writes `CANDIDATES_RESOLVED` yet. The call site is the Trust T7 step through the Candidates adapter (`orchestrator/portadapters/trust`), which is EL's composition. The identity is open: the reconstruction golden and `record/export` use `core-evidence`, but since A555 the ranking is Trust's, so `core-trust` is the identity of the code that has the result. **Decision needed from the Architect: which `written_by`.** (The Record rows per power are bound by role in the database; the writer must hold that role.)
2. **The payload's shape against the reconstruction.** `record/reconstruct/project.go` projects `CANDIDATES_RESOLVED` with the keys `resolved_target`, `candidate_count` and `margin`, and its golden writes `margin` as a decimal string ("0.41"). The Python reference writes `margin` as `n/d` text. This payload carries `candidate_count` and `margin_bp` (an integer) and has no `resolved_target` (the ranker has no resource reference). **Decision needed: either the projection reads `margin_bp` and the resolved target is added by the caller from the candidate's `resource_ref`, or the payload also carries the decimal string.** `record/` is not C2's to edit.
3. **The v2 consumer.** `trustport.CandidateRanker` still takes `candidates-response.v1` rows (with `counterparty_display`, a name). The v2 read (A676) has `recipient_match` as an exact share numerator/denominator. The ranker takes the recipient feature as basis points, so the consumer must convert a share to basis points: **decision needed: floor division, as every other feature (A560), or an exact rational feature.** Flooring a share lowers a candidate's score, which is against acting for the top and toward acting for the second; the other features are floored the same way, but the EVD-04 status asked for an exact share. This is EL's adapter (A676: "C1 and EL consume the new shape") and the Architect's numeric ruling; not built here.
4. **The `CandidateRanker` adapter** (`platform/candidaterank` into the port), after 3.
5. The recipient matcher in Go (a separate C2 PR in `evidence/recipientmatch`): not touched.

## Messages
- 2026-11-26 · Architect (relayed by the Founder's session) to C2: "Start AMB-01 now that #960 merged". Used for: this task.

# Session 2026-11-26 · AMB-01

## Session summary
- What changed (PR links): noema#1012 (draft).
- Why: the Done-when of AMB-01 and the payload of `CANDIDATES_RESOLVED` (REQ-AMB-002, REQ-AMB-003).
- Risks, and how they are contained: a payload that leaks a name, an amount or a digit (a key-set test and an integers-only walk); a payload that differs between runs (1,000-run byte comparison); a threshold read wrongly at its edge (named boundary tests, exact to one scaled unit). No behaviour of the ranker changes: `Scored` only gains fields.
- Tests added (REQ, INV, US or EC IDs): REQ-AMB-003, REQ-AMB-002; INV-21; AGENT-13, AGENT-16.
- Regression cases added: none (no bug fixed). The boundary and determinism tests are characterisation tests of the merged ranker, with mutants as their evidence.
- Debt entries: none.
- Conflicts raised: none as a CONFLICT block; the two shape questions above are decisions needed.

## Pre-work for the next agent
- Exact next step: the Architect answers blockers 1 to 3; EL appends the event from T7 with `AuditPayload()`, and builds the `CandidateRanker` adapter over `candidates-response.v2`.
- Open questions (owner): which `written_by` (Architect); the projection's key for the margin (Architect); the share to basis points rule (Architect).
- Files to read first: `services/core/platform/candidaterank/audit.go`, `README.md`, `record/reconstruct/project.go`, `orchestrator/portadapters/trust/candidates.go`.
- Do not touch: `contracts/fixtures/candidate_ranking_vectors.json` (shared with the Python reference).
