# Status · A259 shared digit-run vectors (Core Engineer 1)

Updated 2026-11-18. Task: ruling A259 (one definition of a digit run for three consumers, tied by one shared vector file) with A279/A270 (money exemption). Tier 1 (the Record guard is a P0 path). Rulings are INTERIM, relayed by the user, and not committed to `docs/decisions/` yet; this work follows them as written and does not make them authoritative.

## State: commits on local branch `prep/a259-vectors` (PR noema#709 is pushed at 8f865f9c; later commits are local only). Draft until the Architect confirms the points under "A308" below and the founders review (protected paths: `services/core/record/`, `contracts/`).

## Plan (written before the first edit)

Consumers and where each lives today:

| Consumer | Code | Verdict used by the vector test | Exemptions today |
|---|---|---|---|
| Record guard (Go, source of truth for the class) | `services/core/record/payloadguard/guard.go`: `separators`, `digitRun`, `Check`, `Mask`, `exempt` | `Check` of `{"v": <input>}` returns an error | by shape and path: typed IDs, content hashes, client request UUID, RFC 3339 timestamp, key ID, signature value, receipt transport classes, target reference |
| Model-service redactor (Python) | `services/model/noema_model/platform/redaction/digit_runs.py`, `__init__.py` | `Redactor.mask_account_numbers(input) != input` | none |
| UI scan (TypeScript) | `ui/src/privacy.ts` `scanVisibleText` | the findings hold `digit-run` | none |

Reading the code found three differences before any change (each listed in the final report): (1) the UI separator class is far narrower than the guard's; (2) the Python class also joins U+001C to U+001F (the guard does not); (3) the existing exemptions (typed IDs, RFC 3339 and the rest) exist in the guard only.

Where the vector file lives: `contracts/fixtures/digit_run_vectors.json`, beside `golden_vectors.json`, the repo's cross-language fixture. The Go test, the Python test and the UI test already read files under `contracts/` by a relative path, so no tooling is new. `tools/contracts/validate.py` reads only `fixtures/valid/*` and `fixtures/invalid/*`, so a top-level file is not validated as a document. It is a fixture, not a contract: no schema, no version of a contract, no codegen.

Steps:

1. RED commit: the vector file and the three tests (Go `vectors_test.go`, Python `test_digit_run_vectors.py`, UI `digit_run_vectors.test.mjs`), each loading the same file and asserting `refused` per named vector. Expected red: the money vectors in all three, the separator vectors in the UI.
2. GREEN commit: the smallest change in each consumer, standard library only. The money exemption is a whole-string match, added beside the existing exemptions (the guard's `exempt`), as `is_money` in the redactor's module, and as a check before the digit-run finding in the UI. The UI class is widened to the guard's.
3. Mutants: drop each money clause (prefix, grouping, decimals, nine-digit bound) and a separator class member in each consumer; each must fail a vector.
4. READMEs and this file updated.

Tests first, faults: none to exercise (pure functions). Record events: none are asserted; the guard's accept/refuse verdict is what the Record's append relies on (`chain.Appender.Append` calls it before anything is signed).

## Session 2026-11-18 · A259 shared digit-run vectors (Core Engineer 1)

### Session summary
- What changed: `contracts/fixtures/digit_run_vectors.json` (184 named synthetic vectors, nine groups; a fixture, not a contract); `payloadguard/vectors_test.go`, `services/model/tests/test_digit_run_vectors.py` and `ui/test/digit_run_vectors.test.mjs` load it and assert one verdict per vector; `isMoney` in the Record guard's `exempt`; `redaction/money.py` `is_money` in the redactor; the UI scan's separator class widened to the guard's and the same money check; READMEs of `payloadguard`, `platform`, `contracts` and `contracts/fixtures`.
- Why: A259 (one definition of a digit run, three consumers, one tie), A279/A270 (money exemption). Interim rulings relayed by the user; not committed to `docs/decisions/`, so nothing here is authoritative until they are.
- Risks, and how they are contained: (1) the UI scan was far narrower than the guard (it missed 2,792 code points the guard joins, e.g. every combining mark, the square and curly brackets, the middle dots and the full-width forms); it is now at least as strict as the guard. (2) The money exemption applies at any path, as the whole-string rule says; it cannot be used to smuggle an account number because the shape allows at most nine integer digits, in groups, with exactly two decimals, behind three capitals and a space. (3) The Python redactor still joins U+001C to U+001F, which the guard does not (stricter, so no leak); not changed, see below.
- Tests added: REQ-SEC-003, REQ-UI-001 (the three vector tests). Mutants in the three consumers: Go 35, Python 45, UI 40; see the table.
- Regression cases added: the three Arabic-Indic money vectors (found by a surviving `\d` mutant).
- Debt entries: none filed. Conflicts raised: none; the points below are for the Architect.

### Mutants (each run with the shared vector tests only; a mutant is one edit to the production code)

| Consumer | Mutants | Killed | Survived (each shown equivalent) |
|---|---|---|---|
| Go guard | 35 (money prefix length, case and space; grouping; decimals; both anchors; bound 9 to 10, to 8 and dropped; not wired; each of the 21 separator class members) | 33 | decimals `{1,2}` instead of exactly two: a one-decimal amount has a longest run of four, so the verdict cannot differ; `\-` : U+002D is also in `\p{Pd}` |
| Python redactor | 45 (as Go, plus `fullmatch` to `match` or `search`, `$` with `match`, `\d` in each position and everywhere, each extra separator, `isspace`, each category) | 37 | the same decimals mutant; `-`, U+2010 to U+2015 (all Pd); `\x0b`, `\x85`, Zs, Zl, Zp (all `isspace`) |
| UI scan | 40 (as Go, plus the multiline flag, each of the 24 class tokens) | 37 | the same decimals mutant; `\-` (in `\p{Pd}`); the literal space (in `\p{Z}`) |

A `$` with `fullmatch` in Python is equivalent and is not counted. Bytecode caching made the first Python run wrong (same-size mutants in the same second reused a stale `.pyc`); the reported run disables it.

### Differences between the three consumers (found by reading, then measured over every code point)
1. UI separators narrower than the guard's: fixed (above).
2. Python joined U+001C to U+001F (`str.isspace`) and the guard did not: resolved by A308 (the guard now joins them).
3. Existing exemptions live in the guard only (typed IDs, content hashes, the client request UUID, RFC 3339 timestamps, key IDs, signature values, receipt transport classes, target references). The redactor and the UI scan have none, so the vector file holds none of them (their tests stay in `guard_test.go`). Money is now in all three, whole string, at any path.
4. The verdict has three shapes (guard: refuse; redactor: change the string; UI: a finding named `digit-run`); the file's `refused` is the one meaning.
5. Unicode versions: Go 1.24 is Unicode 15.0, Python 3.13 is 15.1, Node 22.23 is 17.0. Node knows 90 decimal digits and 94 separators the guard does not (stricter in the UI). The vectors use only characters of Unicode 15.0 or older. Digits and separators of the guard still agree exactly with Python's.
6. Go `Mask` (the display mask of `record/reconstruct`) and the model service's `assemble.py` had no money exemption: resolved by A308 (both have it now).

### A308 (relayed by the user, interim): the open points above, decided

- U+001C to U+001F join digit runs in every consumer: the guard's `separators` gains `\x{1c}-\x{1f}`, the UI class gains `\x1c-\x1f`, the Python redactor already joined them (`str.isspace`; docstring corrected), and the vector file has four more vectors (`joins_u001c_file_separator` to `joins_u001f_unit_separator`, refused).
- The money exemption applies in every consumer: `Mask` (Go) now returns a whole money string as it is, so `record/reconstruct` serves a stored `NGN 12,345.00` unchanged (`project_vectors_test.go`: every vector as a plain scalar fact), and the assembler's `_surface` keeps a money surface form (`test_digit_run_vectors.py`, every non-empty vector).
- The money prefix stays any three capitals.
- **Consequence on the explanation path, for the Architect to confirm:** `explanation/factsource` accepts a fact value only if `Mask` leaves it unchanged, so `TestTheAmountIsFormattedFromIntegerMinorUnits` changed: `NGN 1,234.50`, `NGN 1,234,567.89` and `NGN 1,000.50` were unknowns (noema#586, "until the Architect words such an amount") and are now shown; `NGN 1,000,000,000.50` (ten integer digits with decimals) is still an unknown. That test change is its own commit so that it can be reverted if A308 was not meant to reach the explanation.
- **Not done, stopped (AGENT-15):** `tools/contracts/digit_run.py` `DIGIT_RUN`, the pattern in 17 members of `customer-answer.v1`, `overlay-card.v1`, `errors.v3` and `reconstruction.v3` (and `FACT_VALUE`), does NOT join U+001C to U+001F (measured: `1234\x1c5` is accepted by it). Aligning it rewrites four published schemas and their generated models; that is a contract change (version bump and migration note, or a ruling that it is fixed in place as A258 did for `reconstruction.v3`). Nothing was changed there.
- Still open: the money rule is not tied to `currencies.json` (any three capitals, as ruled); a grouped amount with no decimals holds no run, so `NGN 123,456,789,012` is accepted by the comma rule (a vector pins it).

## Pre-work for the next agent
- Exact next step: review, then push when told. Commands: `cd services/core && go test ./record/payloadguard/ ./record/chain/ ./record/reconstruct/`; `cd services/model && python -m pytest tests/test_digit_run_vectors.py && python -m mypy`; `cd ui && npm run build && node --test test/*.test.mjs`; `python3 tools/lint/run.py`.
- Open questions (owner): the explanation-path consequence and the schema pattern (Architect), both under A308 above.
- Files to read first: `contracts/fixtures/README.md` (digit-run section), `services/core/record/payloadguard/guard.go` (`separators`, `isMoney`).
- Do not touch: `testdata/rapid` files; `golden_vectors.json`.
