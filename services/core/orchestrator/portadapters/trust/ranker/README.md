# ranker

Date: 2026-11-28 · Owner: EL · Task: STM-01 · REQ-AMB-003, REQ-AMB-002 · SoT INV-01, INV-14, INV-21, AGENT-16 · All data is SYNTHETIC.

The candidate ranking port of the Trust Candidates adapter. `New(pack)` reads `understanding.ranking` (exact decimal weights, the amount tolerance ratio, the time tolerance in seconds) from the
verified pack's bytes; `Rank` scores candidates of `candidates-response.v2` in basis points, exactly (big rationals), rounded down. It mirrors `understanding/resolution/ranking.py`.

- `type_direction` scores 0 because `trustport.CandidateSearch` carries no expected direction: a feature with nothing to compare can only lower a score. Whether the search should carry one is a decision (listed in the PR).
- Tests: `cd services/core && go test -race ./orchestrator/portadapters/trust/ranker/`
