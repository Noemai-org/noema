# Status · UND-16 · error diagnostics of the compile acceptance recordings (A359)

Owner: C2 · Task: UND-16 (IL, C2); ruling A359 (Architect, 19 Nov 2026, interim); amends noema#720 (A320) without weakening it.

# Session 2026-11-19 · UND-16 · diagnostics for a failed call

## Session summary
- What changed (PR link: to be added when opened): `benchmark/compile_acceptance/recording.py` (`error_triple`, `failure_class`, the `failure_class` and `error` members of a recording and of a manifest row, `failure_classes` counts in the manifest, `FAILURE_CLASSES`), `test_recording.py`, `test_leakscan_allow.py`, the folder README.
- Why: live run 44521380842 had four HTTP 400s whose cause was unknowable (a non-2xx body is never written) and four calls with no status. A359: record the status, and from OpenAI's error object only `type`, `code` and `param`, each validated; never the message or the body; a failure class for every call.
- Risks, and how they are contained: a field is kept only if it is a short plain identifier (pattern in the README), has at most four digits, no long token and no key prefix, so a message, an account, a prompt echo or a key cannot pass; the body of a non-2xx reply is still never written; a malformed, huge or deeply nested body gives three nulls and never loses the recording (read up to 1 MiB).
- Tests added (REQ-INT-001, INV-29, AGENT-13, AGENT-25): 18 in `test_recording.py` (class `Diagnostics`), 2 in `test_leakscan_allow.py`; red first (88 failures and 11 errors against a stub that wrote `null`).
- Regression cases added: a 400 with a realistic OpenAI error body; the hostile values; the malformed envelopes (none, empty, not JSON, array, string, `error` not an object, fields not strings, invalid UTF-8, 200000-deep nesting, 3 MB); a 2xx that is not JSON; a deeply nested 2xx (it raised before and lost the recording).
- Debt entries: none.
- Conflicts raised: none.

## Silences (A332)
- A param, code or type that is a lowercase word that happens to be a name (for example `tunde`) passes: the pattern cannot tell it from an identifier. The model's own `param` names a field path; a name there is not expected.
- A value with a hyphen, an uppercase-only rule aside, or non-ASCII, or five digits, or a part longer than 32 characters becomes `null`, even when it was honest (a model id such as `gpt-6-luna` in `param` is lost). Safer reading chosen.
- `code` and `type` that are not strings (an integer code) become `null`.
- A non-2xx reply always carries the triple (three nulls when nothing usable); `error` is `null` only for a 2xx and for a call with no status.
- A status below 200 or 300 to 399 is class `REJECTED` (as the provider's `failure_for_status`); an unknown failure label is ignored for the class (the raw label stays in `failure`).
- A 2xx whose body is not a JSON object is class `MALFORMED`; the body is still kept as before.
- The class vocabulary is copied from the model service (`ProviderFailure`); a test compares them only where `noema_model` is importable (the model job), so the benchmark job cannot see drift.
- An error body over 1 MiB gives three nulls.

## Hand mutants (40 over `recording.py`, each killed; one survived the first tests and got a case)
M01 length cap 64 to 128 (`a_field_that_is_not_a_short_plain_identifier...`, after the 65-character case with short segments was added); M02 brackets not allowed (`a_param_that_names_a_response_format_survives`); M03 hyphen allowed; M04 leading digit allowed; M05 digit limit 4 to 5; M06 digit limit 4 to 3 (`a_task_name_is_not_taken_for_a_key_and_four_digits_are_kept`); M07 run limit 32 to 64; M08 run limit 32 to 9; M09 no key-shape check; M10 key lookbehind dropped; M11 `AKIA` prefix dropped; M12 `fullmatch` to `match`; M13 type check dropped (`a_hostile_envelope_never_loses_the_recording...`); M14 body cap 1 MiB to 1 GiB; M15 and M16 no `RecursionError` handling (`a_deeply_nested_2xx_reply...`); M17 and M18 the envelope type checks dropped; M19 error only for 400 and above; M20 error for a 2xx; M21 error for no status; M22 any failure label wins; M23 408 not TIMEOUT; M24 429 not RATE_LIMITED; M25 500 not UNAVAILABLE; M26 a 2xx always OK; M27 no status to UNAVAILABLE; M28 `message` read too (`a_400_with_an_openai_error_body_writes_the_triple...`); M29 the manifest row without `error`; M30 no counts in the manifest; M31 a non-2xx body written (`the_reply_body_of_a_response_that_is_not_2xx_is_never_written`); M32 a list counts as an object; M33 300 counts as 2xx; M34 a quote allowed; M35 a space allowed; M36 a label ignored when no status; M37 `failure_class` not in the file; M38 `error` not in the file; M39 fields read from the wrong key; M40 an empty body counts as an object. Each was restored; the script lives outside the repository.

## Pre-work for the next agent
- Exact next step: after merge, the Founder dispatches the next live run; read `error.param` of each 400 in the step summary.
- Open questions (owner): whether the class vocabulary should be generated from the model service (Architect).
- Files to read first: `benchmark/compile_acceptance/README.md` (Diagnostics), `recording.py`.
- Do not touch: the rule that a non-2xx body is never written (SR's condition, A320).
