# Status · TRU-15-candidate-ranker (Core Engineer 2) · the candidate ranker in Go

Updated 2026-11-23. Rulings A555, A559, A560 (interim, relayed, not yet committed).

## State
- noema#918: `services/core/platform/candidaterank` (the ranker, `ParseBP`, tests), `contracts/fixtures/candidate_ranking_vectors.json` (shared vectors), `services/model/tests/test_ranking_vectors.py` (the Python reference against the same vectors).
- Placed in `platform/` because the threshold-literal lint forbids numbers other than 0 and 1 under `trust/` (precedent: `platform/trustlimits`).
- Local: gofmt, go vet, go test, `tools/lint/run.py`, mypy strict, the contracts and hard-code guards; 17 hand mutants, 15 killed, 2 equivalent (`distance >= tolerance` versus `>` when the left side is 0; the window start inclusive versus a zero gap).

## Open
- The recipient matcher in Go (`golang.org/x/text`, indirect to direct): an [OPEN] entry for the Steward, with the safest default that the recipient feature is an input from the caller. Owner: Architect.
- The `CandidateRanker` adapter and wiring (`wire_trust.go`): EL.
- The model CI job runs only when `services/model/` changes, so a later edit of the vector file alone would not run the Python side. A candidate: a path filter in `.github/workflows/model.yml` for `contracts/fixtures/candidate_ranking_vectors.json`. Not done here (a `.github` change).
- The Python and Go margins can differ below one basis point; the vectors avoid it.

## Messages
- 2026-11-23 · Founder → C2: A555, A559, A560. Used for: this change.

# Session 2026-11-23 · TRU-15

## Session summary
- What changed (PR links): noema#918.
- Why: A555 gives the Go port of the candidate ranker, with shared vectors, in Trust; A559 and A560 give the integer rule and the thresholds from data.
- Risks, and how they are contained: a drift between the two implementations (one shared vector file with both thresholds on each side; the pack's values are read and compared); a float creeping in (an AST guard); a boundary changed by a refactor (17 hand mutants).
- Tests added (REQ, INV, US or EC IDs): REQ-AMB-003, REQ-AMB-004; INV-01, INV-14, INV-21; AGENT-16.
- Regression cases added: the shared vectors.
- Debt entries: none filed; the CI path filter above is a candidate.
- Conflicts raised: the earlier A546 CONFLICT (placement) is settled by A555.

## Pre-work for the next agent
- Exact next step: EL builds the `CandidateRanker` adapter over `candidaterank.Config.Rank` and the matcher once the dependency is approved; the composition reads the pack with `ParseBP`.
- Open questions (owner): the matcher dependency (Architect).
- Files to read first: `platform/candidaterank/README.md`, `contracts/fixtures/README.md`.
- Do not touch: the vector file's thresholds without a decision record.
