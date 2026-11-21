# Session 2026-11-19 · TRU-13

State: green commit local, not pushed (EL pushes after reading the red job). PR: noema#780.

## Session summary
- What changed (PR links): noema#780. `evidence/payloadread`: the Reader screens a verified payload's text with `injection.Screener` and fails closed (`ErrScreenRefused`) when the screen is refused; a flag stays advisory (alert seam when the Record refused the event). Test constructions updated in `evidence/reconciler` and `orchestrator/portadapters/trust/reconciler` tests.
- Why: A372 (interim, relayed, not yet committed); REQ-CTX-007, INV-02, INV-19, AGENT-09.
- Risks, and how they are contained: not wired in cmd/core yet (final main.go PR must build the Reader with `NewReader(payloads, screener, correlationOf, alert)`); the correlation seam is the caller's. Refused screen ends T9/T10 classification UNKNOWN through the reconciler's existing `ErrRead`.
- Tests added (REQ, INV, US or EC IDs): `screen_test.go` (REQ-CTX-007, INV-19, AGENT-13).
- Regression cases added: none beyond the tests.
- Debt entries: none.
- Conflicts raised: none. A372 text not available; safer readings listed in the PR body.

## Pre-work for the next agent
- Exact next step: the main.go PR builds the Reader (Screener with the pack detector and `RecordRecorder`, an Alert that logs names only, a correlation source) and passes it as the reconciler's `Payloads`.
- Open questions (owner): where the correlation comes from in production (EL).
- Files to read first: `services/core/evidence/payloadread/payloadread.go`, `services/core/evidence/injection/screen.go`.
- Do not touch: `evidence/injection` (EVD-06).
