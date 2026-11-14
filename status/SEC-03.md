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
