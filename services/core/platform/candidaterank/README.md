# platform/candidaterank

Date: 2026-11-23 · Owner: Core Engineer 2 (C2) · Task: TRU-15 · All data is SYNTHETIC.

**What it does.** The deterministic candidate ranker of REQ-AMB-003 for the Trust side (rulings A555, A559 and A560, interim, relayed): integer basis points (10000 is the best there is), floor division, no float. `Config.Rank` scores each candidate (`ScoreBP`: the weighted sum of five features, floored once), orders them (score, then the newer time, then the ID: REQ-AMB-004), and applies the auto-resolve rule. `ParseBP` reads a pack's decimal or fraction ("0.35", "7/20") as an exact number of basis points; it reads the text and never makes a float, so a pack's JSON number must be passed as the text it is written in (`json.Decoder.UseNumber`).

**REQs.** REQ-AMB-003, REQ-AMB-004. **SoT.** INV-01 (no model), INV-14 (no industry or country term), INV-21 (replayable), AGENT-16 (no default for any threshold, weight or tolerance).

**Where it is, and why.** `platform/` and not `trust/`: the lint forbids a numeric literal other than 0 and 1 in decision code under `trust/`, and the unit of a basis point is 10000 (the same reason as `platform/trustlimits` and `platform/ceilings`). Trust reads the result through the `CandidateRanker` port of `orchestrator/portadapters/trust` (scores in basis points, the unit of the pack's floor). The adapter that implements that port is not built here: it needs the recipient matcher (below).

**Every threshold is an input.** `Config` carries the five weights (they total 10000), the floor (7000 in the pack), the margin (3500), the most candidates above the floor (4), the amount and time tolerances and the raw bound. A zero `Config` is refused (`ErrConfig`). The two thresholds are read from the pack: the floor from `understanding.ranking.absolute_floor` (and `limits/trust_limits.json` carries the same 7000 as a platform minimum), the margin and the cap from the intent node's `disambiguation`. `TestThePacksOwnThresholdsAreTheOnesTheVectorsUse` reads the pack and fails if a value is not the one the vectors were built for.

**The rule.** Auto-resolve only if exactly one candidate has the top score (a tie is never broken), top minus second is at least the margin (the second is 0 for a single candidate), the top is at least the floor, and there is no material flag and no low ASR confidence. More than the cap above the floor is TOO_MANY_CANDIDATES, whatever else holds. A feature with nothing to compare scores 0, which only lowers a score.

**Shared vectors.** `contracts/fixtures/candidate_ranking_vectors.json` is loaded by this package's tests and by the Python reference's (`services/model/tests/test_ranking_vectors.py`), with each threshold one basis point below, at and above (margin 3499, 3500, 3501; floor 6999, 7000, 7001; four and five candidates above the floor). See `contracts/fixtures/README.md`.

**Python.** `understanding/resolution/ranking.py` has a production caller (`understanding/pipeline/run.py`, in the compile pipeline of the model service), so it is non-authoritative: its answer is a proposal that Trust does not rely on. It uses exact fractions, so the two can differ below one basis point (the Go margin is the difference of two floored scores). The vectors avoid such cases and say so.

**Not built here.** The recipient matcher (the fifth feature is an input, `Candidate.RecipientBP`): its Python version needs Unicode NFKD and case folding, and a Go version needs `golang.org/x/text` promoted from an indirect to a direct dependency of the core (AGENT-14: an [OPEN] entry and approval first). The `CandidateRanker` adapter and its wiring are EL's.

**Tests.** `go test -race -count=1 ./platform/candidaterank/` from `services/core/`; `python -m pytest tests/test_ranking_vectors.py` from `services/model/`.
