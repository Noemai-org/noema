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

## Session 2026-11-11 · SEC-03 (second slice: the core's redactor)

### Session summary
- What changed: `services/core/platform/redaction` is built (it was an empty boundary package): `New(minDigits)`, `MaskAccountNumbers`, `FilterAttributes` (the trace attribute filter) and `Handler` (an `slog.Handler` wrapper). `edge/session/no_echo_test.go` plants an account number in a refused request and checks the response. Tests written first (the package did not compile); nine mutations each turn the suite red.
- Why: task SEC-03, done_when 'a planted full account number in a fixture is caught in logs, traces and API output'. The model service already had a redactor; the core had none.
- Risks, and how they are contained: the handler is a library, not installed in `cmd/core` (see below), so no behaviour changes today.
- Tests added (REQ-SEC-003, REQ-OBS-002, AGENT-13): `platform/redaction/redaction_test.go` (9 tests), `edge/session/no_echo_test.go` (8 cases).
- Debt entries: none new; the open points below are for the Architect and EL.
- Conflicts raised: none.

### Open questions (AGENT-02)
- **Where does the core's threshold come from?** (Architect) The model service reads `masking.account_run_min_digits` from its decision config. The core has no config source. Smallest safe default proposed: the same decision-config value, read by `platform/config` when it is built (OPS-01), with the handler installed in `cmd/core` in the same change. Evidence that would invalidate it: a decision that the core masks at a different length.
- **A caller-supplied correlation ID is echoed unmasked (finding on `edge/session`, EL).** `correlationID` keeps any ID of the shape `corr_[0-9A-Za-z]{2,32}`, so `Noema-Correlation-Id: corr_0123456789` comes back in the response header and body and then rides into logs and the Record. A planted account number can travel as an ID. Reproduction: a test that sends that header through `Middleware` and asserts no response header or body holds the number; it fails on `main`. Options: always mint the ID and keep the client's as a separate untrusted value, or accept only IDs that the core minted. Raised on a WAITING issue; not fixed here because `edge/session` is EDG-01's module.

## Pre-work for the next agent
- Exact next step: install `Handler` in `cmd/core` once the threshold has a source; SEC-07 wires the scanner to real log and API output.
- Do not touch: the redaction rules without a decision record (AGENT-16).
