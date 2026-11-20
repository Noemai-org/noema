# Status · TRU-07-candidates (Core Engineer 2) · the Trust intake Candidates adapter

Updated 2026-11-18. The task row is TRU-04 (T7); the branch name is TRU-07-intake-candidates (the lead's label; `tasks.csv` TRU-07 is the Confirmation contract).

## State
- noema#741 (draft): `services/core/orchestrator/portadapters/trust/candidates.go`, its tests and `README-candidates.md`. Red first (A302): the failing job of `candidates_test.go` is the build (six undefined names), read before the implementation is pushed.
- Built locally: gofmt, go vet, `go test -race` of the package, `python3 -I tools/lint/run.py` clean; 100 hand mutants, 98 killed, 2 equivalent (see the session).
- Blocked for wiring: the candidates reader (`evidence/candidates_api`), a Go ranker and a labeller with the pack's label data do not exist.

## Open
- The thirteen silences taken by the safer reading are listed in the PR body (A332).
- A Trust re-read needing its own EVIDENCE_READ event: for SR.
- DB-backed test with a real intent store: not written (a TestMain would collide with the sibling adapters in the package).

## Messages
- 2026-11-18 · lead → C2: the task (A329, Candidates). Used for: this change.

# Session 2026-11-18 · TRU-07-candidates

## Session summary
- What changed (PR links): noema#741 adds `candidates.go`, `candidates_test.go` and `README-candidates.md`.
- Why: A329 gives the Trust intake's Candidates adapter to C2.
- Risks, and how they are contained: a leak across principals or tenants (the request is the session's; the stored intent must name exactly the session's principal; tests with two principals and a second tenant); a guessed answer (no default for any part; every answer of a port is checked; an error is never an empty list); a leak in an error (fixed text, cause behind it).
- Tests added (REQ, INV, US or EC IDs): REQ-CTX-006, REQ-AMB-002, REQ-AMB-003, REQ-AMB-004, REQ-TRU-002, REQ-TRU-003, REQ-NFR-002; INV-11, INV-12, INV-14, INV-16, INV-21; AGENT-13, AGENT-16.
- Regression cases added: none (no bug fixed). Fixture correction after the red push: the test candidate IDs had underscores that `candidates-response.v1` does not allow (`cand_[0-9A-Za-z]{2,32}`); renamed. Cases added after mutation testing: an answer with no version, an amount or a display past the contract, a currency the contract has not, an extra score, duplicate slot names, an identity that does not match the store, aliasing of labels.
- Debt entries: none.
- Conflicts raised: none. The task names `evidence/candidates_api` as an existing read; it is an empty package.

## Pre-work for the next agent
- Exact next step: build `evidence/candidates_api` (the Reader over `adapters.Transactions`, masked, at most 20, newest first), a Go ranker (REQ-AMB-003), and the labeller with the pack's attribute order, options key and templates; then wire `NewCandidates` in `wire_trust.go` (EL).
- Open questions (owner): the pack's askable attributes and label templates (pack owner); whether a Trust re-read needs EVIDENCE_READ (SR).
- Files to read first: `services/core/orchestrator/portadapters/trust/candidates.go`, `README-candidates.md`, `services/core/trust/intake/ports_t7_t9.go`, `gates_t7_t9.go`.
- Do not touch: the red tests' meaning; `packs/`, `contracts/`.
