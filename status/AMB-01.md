# Status · AMB-01 (Core Engineer 2) · deterministic ranking, the 0.35 margin, the 0.70 floor, the crowd rule, `CANDIDATES_RESOLVED` with scores

Updated 2026-11-26. All data is SYNTHETIC.

## State
- Order of the Architect, 26 Nov 2026: "Start AMB-01 now that #960 merged" (relayed; not yet committed as a ruling, so this file and the PR body are its record). Plan row AMB-01: owner C2+AG; REQ-AMB-003, REQ-AMB-002; Needs EVD-04 (noema#960, merged); Done when: "Same inputs give the same order 1,000 times; boundary tests at margin 0.349, 0.35 and 0.351 pass".
- **What already existed on main.** The ranker is `platform/candidaterank` (TRU-15; A555, A559, A560, A566, A583): integer basis points, floor division, no float, the margin, the floor, the tie and the crowd rule decided on the exact scaled sums, every threshold an input from the pack, the shared vectors in `contracts/fixtures/candidate_ranking_vectors.json`. It is Go and it is the authority (A559); the Python ranker is the reference. AMB-01 therefore builds no second ranker.
- **Built in this PR (noema#1012):** (1) `Resolution.AuditPayload()`: the payload of `CANDIDATES_RESOLVED` with the scores (REQ-AMB-002 "IDs and scores, masked"; REQ-AMB-003 "Scores and feature values recorded"): the state, the reasons it did not resolve, the counts, the top, second and margin in basis points, and for each candidate in ranked order its ID, score and the five feature values; a resolved ID only for AUTO_RESOLVED; integers and strings only (PERD 32.1); no name, amount, time, digit or direction of a candidate (A676, AGENT-13). `Scored` gains the five feature values (additive). (2) After A725: `resolved_target`, `WrittenBy`/`EventType` and `ShareBP` (below). (3) The Done-when tests by name: the 1,000-run determinism proof (a stride permutation of the rows each run, ties at the top, the same order and the same payload bytes), the margin at 0.349, 0.35 and 0.351 (3490, 3500, 3510 basis points), the floor at 0.699, 0.70 and 0.701, four and five candidates above the floor, and exactness to one scaled unit at the margin and the floor (a configuration with weights that have no common factor), one candidate scoring 0 is not a tie.
- **No threshold, margin, weight or default is set here.** The tests take them from a test configuration that equals the pack's (the existing `TestThePacksOwnThresholdsAreTheOnesTheVectorsUse` pins the pack to the vectors).

## The three decisions are answered (A725, `DEC-2026-11-26-architect-rulings-a721-a737`, interim)
1. **Writer:** `CANDIDATES_RESOLVED` is written as `core-trust` (the ranking is Trust's). `candidaterank.WrittenBy` and `candidaterank.EventType` are the constants the call site must use; a test pins them to `record/chain/actors.json` (a RUNTIME emitter) and to `record-event.v3.json`'s event enum. The composition is EL's and is not edited here.
2. **Payload:** integer basis points plus `resolved_target`. I chose the resolved candidate's own **`resource_ref`** (the contract's `<vertical>.<kind>:<id>`, which the reconstruction projection already reads under `resolved_target` and masks), carried from the read through `Candidate.ResourceRef` and `Resolution.ResolvedRef`, and named only for AUTO_RESOLVED. Not `counterparty_ref` (an ordinal numbered per answer: it could be read as an identity across sets, A676) and not the candidate id (already in the payload as `resolved_candidate_id`). A `ResourceRef` that is not the contract's shape is refused whole by `Rank` (no name or free text can reach the Record through it). An empty one is allowed and gives a payload with no `resolved_target` key. `candidate_count` and `margin_bp` stay. C1 updates the reconstruct projection and its golden in a separate Tier 1 PR; `record/` is not touched here. No contract change: the payload is free-form under record-event.v3 (integers only), and `Candidate` and `Resolution` gain optional fields.
3. **Share to basis points:** `ShareBP(numerator, denominator)` floors the v2 `recipient_match` share to basis points by integer division (A560): `math/big` product, no float, never rounded; a share outside [0, 1], a zero or negative denominator is `ErrShare`. 1/3 gives 3333, 2/3 gives 6666, 34999/100000 gives 3499, 57/100 gives 5700.

## Still not built here, and who owns it
- The append of the event (EL's T7 composition, with `WrittenBy`, `EventType` and `AuditPayload()`), the `CandidateRanker` adapter over `candidates-response.v2` (EL, A676) calling `ShareBP`, and the reconstruct projection and golden (C1). `evidence/recipientmatch` (another C2 PR) is not touched.

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
- Conflicts raised: none; the three questions were answered by A725.

## Pre-work for the next agent
- Exact next step: EL appends the event from T7 as `candidaterank.WrittenBy` with `AuditPayload()`, and builds the `CandidateRanker` adapter over `candidates-response.v2` using `ShareBP`, passing each candidate's `resource_ref` as `ResourceRef`; C1 updates the reconstruct projection and golden (it reads `margin_bp`).
- Open questions (owner): none from A725. Whether `resolved_target` should be required for an AUTO_RESOLVED set (a candidate with no `resource_ref` gives none): EL's adapter always passes it.
- Files to read first: `services/core/platform/candidaterank/audit.go`, `README.md`, `record/reconstruct/project.go`, `orchestrator/portadapters/trust/candidates.go`.
- Do not touch: `contracts/fixtures/candidate_ranking_vectors.json` (shared with the Python reference).

## Session 2026-11-26 (second part) · AMB-01, A725
- What changed: `resolved_target`, the `core-trust` constants, `ShareBP`, the resource reference shape check (noema#1012). Tests first (red `8813b04bf`). 48 hand mutants, all killed. No threshold, margin, default, contract, pack or `record/` change.
