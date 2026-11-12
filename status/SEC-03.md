# Status · SEC-03 · leak scanner

Date: 2026-11-10 · Owner: Security Reviewer · Status: in review (noema#222) · Defines no requirement; REQ-SEC-003 does.

## Session 2026-11-10 · SEC-03

### Session summary
- What changed: noema#222 fixes C1's two blockers (the scan no longer passes vacuously; the fingerprint is a per-run salted HMAC) and Codex's two P2 findings on 4a36112 (date masking only as a whole token; the whole assigned value of a secret-named key is captured). Also C1 should-fix items 3, 4, 5, 7, 8.
- Tests added (REQ-SEC-003, AGENT-13): `Vacuous`, `Fingerprint`, `CodexP2Round2` in `tools/ci/test_leakscan.py`; seen failing first (25 failures, 1 error) before the change.
- Mutation checks: Luhn forced true, path exemption removed, plain-hash fingerprint, no zero-files check, ISO-date lookahead removed, unbounded fraction: each fails a test.
- Debt: dot/slash-split and encoded numbers, and secrets without a secret word in the name, stay out of scope (SEC-07).

## Pre-work for the next agent
- Exact next step: SEC-07 wires the scanner to real log and API output.
- Do not touch: the synthetic fixtures in `test_leakscan.py` (assembled at run time).

## Session 2026-11-12 · SEC-03 follow-up, the shared masking limit (EL; A154 as corrected, relayed, not yet committed)

## Session summary
- What changed: `masking-limits.v1` (schema, fixtures, `packs/fintech/limits/masking_limits.json` with `max_unmasked_digit_run` 4, INVENTORY row, regenerated Go and Pydantic models, shape refusals); `platform/masking` (Go loader, no default, zero value is "not loaded"); `explanation.Templates.WithMaskingLimits` and check 4 reading it (templates without it block every text); `noema_model.platform.masking` and `Redactor.from_limits`; the error scanner `benchmark/isolation/check_isolation.py` reads the same file (it used 6, PERD line 4428 says longer than 4).
- Why: one value, one source, for the redactor, the explanation validator (PERD line 3273) and the error-scanner test (line 4428), in both services.
- Tests added (REQ-SEC-003, REQ-EXP-002, AGENT-16): `platform/masking/masking_test.go`, `explanation/digitrun_test.go`, `services/model/tests/test_masking_limits.py`, `DigitRunLimitTest` and a 5-digit probe in `benchmark/isolation/test_isolation.py`. Written first; red at `6a8cf037` (no loader). Mutants run: 12; ten killed; two equivalent (unknown fields in the Go loader, because the generated shape already closes the object; a bool in the Python range check, because `True` is 1, below the minimum). The scanner hard-coding 4 survived until `judge` read the file on each call; a test now points it at a file with another value.
- Debt: the Go redactor of noema#457 does not read this file yet (it takes a threshold parameter; pass `Limits.MinMaskedRun()` after that PR merges). Other fixed `{5,}` patterns remain in `record/reconstruct/project.go`, `edge/session/http_test.go`, `benchmark/hallucination/verify.py` and `benchmark/bakeoff/test_cases.py`; they agree with 4 today and are listed for a follow-up so this PR stays one change.

## Conflicts raised
- **CONFLICT (model service harness).** `load_harness_config` reads `masking.account_run_min_digits` (8) from `benchmark/decisions/harness-gpt-6-luna.json` and `harness-gpt-6.1-terra.json`. Their SHA-256 is in the frozen block of the model-selection decision record, recomputed by `test_freeze`, and D10 says those values are run values frozen with the files. Making the harness read the shared limit means editing the frozen files and the record, which is not an agent's change. This PR therefore does not touch the harness; `Redactor.from_limits` is ready for the one-line change in `harness/core.py` once the Founders re-freeze the configs without the key.

## Pre-work for the next agent
- Exact next step: after noema#457 merges, install its redactor with `masking.Limits.MinMaskedRun()`; after a decision re-freezes the harness configs, build the harness `Redactor` with `Redactor.from_limits`.
- Do not touch: `benchmark/decisions/harness-*.json` without a decision record.

