# Status · A259 shared digit-run vectors (Core Engineer 1)

Updated 2026-11-18. Task: ruling A259 (one definition of a digit run for three consumers, tied by one shared vector file) with A279/A270 (money exemption). Tier 1 (the Record guard is a P0 path). Rulings are INTERIM, relayed by the user, and not committed to `docs/decisions/` yet; this work follows them as written and does not make them authoritative.

## State: local branch `prep/a259-vectors`, not pushed, no PR

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
