# Session 2026-11-19 17:30 WAT · UND-05 · a screen that did not run fails the compile closed (A372, relayed, not yet committed)

## Session summary
- What changed (PR: noema#792): `screen_quietly` removed from `services/model/noema_model/understanding/pipeline/steps.py`; `run.py` calls `screen_strings` in step 2 and for the candidates' display names in step 4. A `HarnessRejection` from either screen ends the turn `REJECTED_AT_GATE` with the harness code and the failed call kept in the call list; the compiler already maps that to `ProviderUnavailable`. The old test `test_a_failed_screening_never_blocks_the_turn` is replaced by `tests/test_pipeline_screen_closed.py`.
- Why: A372 as relayed: a screen that did not run must be a recorded state and must not let the turn through. The ruling text was not available; this is the safer reading (A332).
- Risks, and how they are contained: a flaky screen route now stops turns (availability down, safety up). No contract, setting or dependency changed. A distinct error code for "the screen did not run" is not built (needs a contract change, AGENT-15); asked of the Architect on the PR.
- Tests added (REQ-CTX-007, REQ-AUD-001, REQ-INT-002; INV-02): `tests/test_pipeline_screen_closed.py`: utterance and candidate screen refused as outage, timeout, non-JSON, no answers, no route; call recorded; code pinned; no candidates means no screen; a clean screen proposes; a client defect is raised. Hand mutants killed: candidate rejection ignored, failed call not recorded, wrong code, utterance rejection ignored, defect swallowed.
- Regression cases added: the file above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: Codex then SR read noema#792 at its green head; the Architect answers whether the screen refusal needs its own error code.
- Open questions (Architect): the distinct code.
- Files to read first: `pipeline/run.py` (steps 2 and 4), `tests/test_pipeline_screen_closed.py`.
- Do not touch: `noema#780` (EL's Go-side payload screen).
