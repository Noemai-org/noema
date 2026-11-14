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
## Session 2026-11-12 · SEC-03 follow-up, the shared masking limit (EL; A154 as corrected, relayed, not yet committed)

## Session summary
- What changed: `masking-limits.v1` (schema, fixtures, `packs/fintech/limits/masking_limits.json` with `max_unmasked_digit_run` 4, INVENTORY row, regenerated Go and Pydantic models, shape refusals); `platform/masking` (Go loader, no default, zero value is "not loaded"); `explanation.Templates.WithMaskingLimits` and check 4 reading it (templates without it block every text); `noema_model.platform.masking` and `Redactor.from_limits`; the error scanner `benchmark/isolation/check_isolation.py` reads the same file (it used 6, PERD line 4428 says longer than 4).
- Why: one value (4), read from one file, by the model redactor, the explanation validator (PERD line 3273, not yet attached by a production caller) and the error-scanner test (line 4428). Not one definition of a digit run: other surfaces still hold their own (see the masking README).
- Tests added (REQ-SEC-003, REQ-EXP-002, AGENT-16): `platform/masking/masking_test.go`, `explanation/digitrun_test.go`, `services/model/tests/test_masking_limits.py`, `DigitRunLimitTest` and a 5-digit probe in `benchmark/isolation/test_isolation.py`. Written first; red at `6a8cf037` (no loader). Mutants run: 12; ten killed; two equivalent (unknown fields in the Go loader, because the generated shape already closes the object; a bool in the Python range check, because `True` is 1, below the minimum). The scanner hard-coding 4 survived until `judge` read the file on each call; a test now points it at a file with another value.
- Debt: the Go redactor of noema#457 does not read this file yet (it takes a threshold parameter; pass `Limits.MinMaskedRun()` after that PR merges). Other fixed `{5,}` patterns remain in `record/reconstruct/project.go`, `edge/session/http_test.go`, `benchmark/hallucination/verify.py` and `benchmark/bakeoff/test_cases.py`; they agree with 4 today and are listed for a follow-up so this PR stays one change.

## Conflicts raised
- **CONFLICT (model service harness).** `load_harness_config` reads `masking.account_run_min_digits` (8) from `benchmark/decisions/harness-gpt-6-luna.json` and `harness-gpt-6.1-terra.json`. Their SHA-256 is in the frozen block of the model-selection decision record, recomputed by `test_freeze`, and D10 says those values are run values frozen with the files. Making the harness read the shared limit means editing the frozen files and the record, which is not an agent's change. This PR therefore does not touch the harness; `Redactor.from_limits` is ready for the one-line change in `harness/core.py` once the Founders re-freeze the configs without the key.

## Pre-work for the next agent
- Exact next step: after noema#457 merges, install its redactor with `masking.Limits.MinMaskedRun()`; after a decision re-freezes the harness configs, build the harness `Redactor` with `Redactor.from_limits`.
- Do not touch: `benchmark/decisions/harness-*.json` without a decision record.


### Update 2026-11-12 · reviewer P1 on noema#468 (the schema must not accept a looser policy)
- `masking-limits.v1` now admits exactly 4 (`const`), not 4 to 32: A154 as corrected caps every outward surface at four, and a range let another consumer validate a looser policy. The Go and Python loaders and the scanner reader repeat the const because generated types do not check values. A different value is a new major version of the contract and a Founder decision. Tests first at `9eae9478` (red for 5, 6, 8 and 32).
- Consequence for the mutation list: the validator or scanner "hard-coding 4" is now an equivalent mutant (the contract guarantees 4), so the tests that used a limit of 6 were removed; the refusals of 5, 6, 8, 32 and 33 replace them.

## Session 2026-11-13 18:00 WAT · SEC-03 · the harness half (A162, option A; EL)

## Session summary
- What changed (noema#468; order confirmed by the Founders: my code half first, then BO's re-freeze noema#538, then a follow-up that refuses the old member): the model service's harness masks at the pack's `masking-limits.v1` value, not at a number in its configuration. `Harness(..., masking=MaskingLimits)` is a required keyword (no default); `assemble_harness` reads the required setting `NOEMA_MODEL_MASKING_LIMITS_FILE` (a missing, unreadable or wrong document leaves no harness and a reason that carries no value); `load_harness_config` accepts a config with or without the old `masking` member and ignores it; `HarnessConfig.account_run_min_digits` is gone; the bake-off's `build_real_decider` takes the limits path (`live_run.py` passes `packs/fintech/limits/masking_limits.json`).
- Why: A154 as corrected (nothing longer than 4 digits leaves unmasked) and A162 (option A). Before this, 5 to 8 digit runs reached a provider unmasked on the harness path.
- Tests added (REQ-SEC-003, AGENT-13, AGENT-16): `tests/test_harness_masking_source.py` (a config with and without the member loads and is equal; a 5-digit run is masked whatever the member says; a 4-digit run is left alone; the Harness cannot be built without the limits; the service builds with the shared file; a missing, unreadable or wrong limits file leaves no harness). 22 existing call sites pass the limits (`tests/support.py: MASKING`); the frozen configs are not edited (their hashes are BO's re-freeze).
- One existing test changed on purpose: `test_a_bad_config_is_refused` no longer lists "the masking value is missing", because that member is not required any more (the new tests cover its replacement).
- Not done, stated: the Compose mount of the limits file (no harness configuration is mounted by Compose yet, so nothing in `deploy/compose` needs it today; a protected path); the follow-up that refuses the old member (after noema#538 merges).

## Pre-work for the next agent
- Exact next step: after noema#538 merges, a small PR makes `load_harness_config` refuse a `masking` member, with the test that an old config is refused.
- Do not touch: `benchmark/decisions/harness-*.json` (frozen; BO's record).

# Session 2026-11-14 · SEC-03 · Security Reviewer's re-check of noema#468 at `b8fd1971`

## Session summary
- What changed (noema#468): wording only, in `platform/masking/README.md`, `services/model/README.md` and this file: they said "the one source" for every surface; the code reads the limit of 4 from one file on some surfaces only, and the run's definition (digit alphabet, separators, count) is still held separately by `project.go`, the Python redactor, the recorder and the validator. The PR title and body now say "limit of 4 read from one file".
- Why: the Security Reviewer's finding (A184 asks for one definition on every surface; this PR does not deliver it and must not claim it).
- Not done: a `masking-limits.v1` that carries the run's definition, every surface loading it, and a shared test-vector file (a contract change, a new major or an additive member: Architect; the core redactor is noema#457's). The `WithMaskingLimits` caller belongs with the composition (mine, a later PR); mounting the limits file in Compose belongs to IL (protected path).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Architect decides the shape of the shared definition; then each surface loads it with the shared vectors.
- Files to read first: `platform/masking/README.md`, `record/reconstruct/project.go`, `noema_model/platform/redaction/__init__.py`.

# Session 2026-11-14 · SEC-03 (SR, fix round for Codex's P2s on e095b99)

## Session summary
- What changed: `platform/redaction` removes a field named `private_key`, `signing_key` or `raw_audio` whatever its value looks like, and replaces a map, slice or group wider than the node budget left with one `[REDACTED]` instead of expanding it. The Codex P2 on account numbers as keys and group names was already fixed at 5fa16fb.
- Tests added: `TestAFieldNamedAsAPrivateKeyOrRawAudioIsRemovedWhatItsValueLooksLike`, `TestAnAudioSizeInBytesIsStillANumberAndNotRemoved`, `TestAWideSliceOrMapIsReplacedOnceAndNeverMaterialisedMemberByMember`; the first and last were red against e095b99 (a 178 KB record for 10,000 members), green after.
- Conflicts raised: none; main merged in once (A176) for a conflict in this file only.

## Pre-work for the next agent
- Exact next step: Codex re-read at the final head; EL's independent read; one Founder approval. The redactor is still not installed in `cmd/core`; the one masking definition is a follow-up (A184: `masking-limits.v1`, noema#468).
