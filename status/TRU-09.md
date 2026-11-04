# Status · TRU-09 · Decision determinism; decision input hash recorded

Updated 2026-11-03. Owner: C1+AG in tasks.csv; executed by the Engineering Lead role agent (TRU-01 to TRU-12). Needs: TRU-02 (merged, noema#109).

## State
- In review (draft PR from `feat/TRU-09-decision-determinism`). Built: `decide.DecisionInput`, `decide.InputHash`, and the replay and determinism tests.

## Open
- **Not yet recorded anywhere.** The hash is computed; storing it with the Decision is the Decision Record (TRU-05). Until then nothing persists it.
- **No static check that real steps are pure.** The tests prove the runner and the hash are deterministic and that steps *built the right way* replay identically; each real step (T1 to T15, TRU-03 to TRU-11) must show the same with its own replay test. The full benchmark replay (REQ-TRU-007 acceptance) needs the Trust intake and BEN-05.
- **Library.** The PERD names Go `rapid` for property tests ([REC], AGENT-14); this PR uses the standard library's `testing/quick` and adds no dependency. docs#94 proposes approving `pgregory.net/rapid`.

# Session 2026-11-03 · TRU-09

## Session summary
- What changed (PR links): `trust/decide/input.go` and `input_test.go` (new), trust README, this file.
- Why: REQ-TRU-007 and INV-21; the Founders' G2 order (TRU-09).
- Risks, and how they are contained: the hash is only as complete as `DecisionInput`; if a real step reads something not in it, the hash would not show it. The rule is stated in the type's comment, and each step's own replay test is required (Open).
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-007, INV-21: golden vectors (independent Python), order and repeat insensitivity, every input changes the hash, length prefix, 9 invalid inputs refused, 10,000 concurrent replays with 0 mismatches under `-race`, a 5,000-case property. Seen failing first (did not compile). Mutation check: 8 mutants, 8 caught.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: TRU-05 stores the hash in the Decision Record; each step task adds its own replay test; BEN-05 replays the benchmark.
- Do not touch: noema#94, noema#90.
