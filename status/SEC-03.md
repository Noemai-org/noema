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
