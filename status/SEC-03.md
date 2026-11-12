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

# Session 2026-11-12 · SEC-03 slice 2 follow-up: ruling A154 and the two Codex P2s on c9cfce9

## Session summary
- What changed (PR links): noema#457 only. The threshold parameter is gone; the rule is the PERD's (no digit run over four on an outward surface). Typed exemptions in `redaction.Exemptions()` (typed ID, RFC 3339 timestamp, `_minor`, `_count`, `_bytes`, duration), each with tests and each shown not to skip a ten-digit string field. Codex P2 1 (nested secret fields): maps, slices and structs are read member by member or replaced by `[REDACTED]`. Codex P2 2 (`WithGroup`): a secret-looking group name removes everything under it, also through `With`.
- Why: Architect order of 12 Nov (A154 corrected: the PERD wins).
- Risks, and how they are contained: the exemptions are the only way a long run survives; they are by type, in one list, with a test that fails when the list and the tests differ. Residual: a typed ID or a timestamp token can hold digits inside its own shape (README, known limits).
- Tests added (REQ-SEC-003, REQ-OBS-002, AGENT-13): `platform/redaction/redaction_test.go` (21 tests); `edge/session/no_echo_test.go` now checks runs of five.
- Regression cases added: the two Codex findings are `TestASecretInsideAMapAStructOrASliceIsNeverPrinted` and `TestASecretLookingGroupOpenedWithWithGroupRemovesWhatIsLoggedUnderIt`, committed red in 3c58e356 before the fix.
- Debt entries: none. Conflicts raised: none.
- Mutations: 21 run (limit, Unicode digits, each exemption removed or loosened, number exemption always or never, string field skipped under an exempt key, duration, secret keys, `WithGroup`, `WithAttrs`, nested containers, message, trace filter, time kind). One survived at first (timestamp parse check) and has a test now.

## Pre-work for the next agent
- Exact next step: a Founder approves the exemption list (AGENT-16); the composition root (EL) installs `Handler` on the core logger and `FilterAttributes` on the trace exporter; the correlation-ID echo of noema#458 still breaks the A154 rule on `main` and waits for EL.
- Open questions (owner): whether a date alone should be exempt (Architect); whether the numeric suffixes need a name list instead (Founder).
- Files to read first: `platform/redaction/redaction.go`, `README.md`.
- Do not touch: the exemption list without a Founder's approval.

## Session 2026-11-12 (later) · Codex P2 on 476f42e: account numbers in keys and group names
- Test first (red, committed before the fix): `TestAnAccountNumberUsedAsAKeyOrAGroupNameIsMasked`. Fix: every emitted attribute key, group name, `WithGroup` name and trace attribute key is masked; secret-key classification still uses the original name. Four mutations of this fix run, all red (one survived until the secret-key-with-digits case was added to the test).

## Session 2026-11-12 (third) · Codex P2s on 5fa16fb: named numeric types and unbounded traversal
- Tests first (red, committed before the fix): `TestAnExemptNumberOfANamedTypeIsWrittenAsAPlainNumberNeverByItsOwnSerialisation` (a `MarshalJSON` on an `int64` type leaked through an exempt key) and `TestACyclicOrVeryDeepOrVeryWideValueIsBoundedAndNeverCrashes` (a cyclic map crashed the process with a stack overflow). Fix: exempt numbers of named types are written as plain `int64`, `uint64` or `float64`; the traversal of maps and slices is bounded by depth 8 and 4096 members per attribute, beyond which a value is `[REDACTED]`.
- Self-review added, to cut review cycles: a generated-value property (`TestNoGeneratedNestedValueLetsADigitRunOrASecretThrough`, planted digits in keys, values, group names and nested members), unsigned and float aliases, and the depth bound. Mutations: no bound, no node budget, no depth bound, and each of the three alias paths forwarded: all red.
- Waiting on noema#468 (EL): one source for the limit. After it merges, `redaction.MaxUnmaskedRun` reads `platform/masking`'s `Limits` instead of a second constant, in a follow-up commit here.
