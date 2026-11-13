# Harness masking re-freeze: the two harness configurations lose their masking member; the floor is the shared 4 (ruling A162)

Date: 13 November 2026. Owner: Benchmark Owner. Authority: ruling A162 and the record `DEC-2026-11-12-harness-masking-refreeze` (the `docs` repository, docs#158, merged; the Founder's approval is the one recorded there, A111). Task: the BO step named on noema#487 and noema#468. This is the dated record that `benchmark/decisions/README.md` requires when a frozen value changes.

This record changes **only** the two harness configuration files and what pins them. It does not touch the corpus, the scoring code, the gates, the retry policy, the prompt template, the rate cards or the models of the 27 Oct 2026 freeze (`2026-10-27-model-selection.md`).

## 1. What changed

1. `benchmark/decisions/harness-gpt-6-luna.json` and `harness-gpt-6.1-terra.json`: the three lines of the `masking` member (`"masking": { "account_run_min_digits": 8 }`) are deleted. Nothing else in either file changed (the diff is exactly those three lines in each).
2. The harness masking floor is the shared `masking-limits.v1` (A154, A162): the longest run of digits a prompt may carry unmasked is **4**.
3. The new hashes are in the frozen block of `2026-10-27-model-selection.md`, which `benchmark/bakeoff/test_freeze.py` and the live-run preflight recompute. The earlier hashes stay there under `superseded_harness_configs`, so a run made before this record still verifies by the hash it carried.
4. The same block now pins `masking-limits.v1` (the section 3 table). The masking value is no longer inside the two hashed configurations, so this pin is what gives a later run a frozen proof of the floor: the live-run preflight refuses to start if either file's hash, or the value, differs.

## 2. The hashes (SHA-256 of the committed bytes)

| File | Before (floor 8) | After |
|---|---|---|
| `benchmark/decisions/harness-gpt-6-luna.json` | `ee1cde86c9599ac6302374a4286373566f87b04df924bd5f225827323933f9dd` | `1b8c026b12c990047d96eb654174df10744865b67a262b6fa2f48f414c4fda6f` |
| `benchmark/decisions/harness-gpt-6.1-terra.json` | `841f01ba906f4b3663711b8cbd8d52596518a38d9a31f3176eba95d63325cd4f` | `599c123f9367d9c240c494e2d730daed649a84a41cdf959abaa5054476a8784b` |

Computed here from the committed bytes (line endings LF, as the repository stores them), not copied: they equal the values the Engineering Lead computed independently on noema#487.

## 3. The shared limit this record pins

| File | SHA-256 | Value |
|---|---|---|
| `packs/fintech/limits/masking_limits.json` (the pack file the harness will read) | `37071a85fe119256914c7dad0a51d6b2e748f2eb16135cf640df335765dffd0b` | `max_unmasked_digit_run: 4` |
| `contracts/schemas/masking-limits.v1.json` (the contract, which admits exactly 4) | `f567106a579a9c67441a96dfe212cb30d8a2cffccc8fcc7824cb0dd1f636d8db` | `const: 4` |

Both files are added by noema#468. A change to either one (a different value is a new major version of the contract and a Founder decision) fails `test_freeze.py` and stops a live run, until a new dated record pins the new hashes.

## 4. Which results used floor 8, and do they stand

Every call made before this record merges used `account_run_min_digits: 8`. Those results are records of what ran: they are **not re-scored, not reinterpreted and not mixed with results under floor 4** (the Founder's condition in the decision record, section 2).

| Result | Floor | State |
|---|---|---|
| The pilot of the 27 Oct freeze (D13: the first two base cases of every task and bucket, both models, within the $2 cap) | 8 | Stands as made. No result file is committed; its cost is not in the repository. |
| The full run, workflow run 40577810935, `gpt-6-luna`: 1,440 calls, gates G1 pass, G2 **fail**, G3 pass, G4 pass, G5 **fail**, G6 pass, G7 pass; verdict `REJECTED_SAFETY` (`benchmark/bakeoff/evidence/run-40577810935/`) | 8 | Stands as made. Its report now carries the label "made under masking floor 8", and the verifier accepts it by its superseded hash. This record does not re-score it and does not uphold or overturn that verdict; a selection that relies on it would have to be made on a floor-4 run. |
| The same run's calls to `gpt-6.1-terra` before the cancel at the 90-minute timeout | 8 | No result file exists and nothing was scored. Not usable for any claim. |
| Any UND-05 qualification call made before this record merges | 8 | None is recorded in the repository. If one exists, it carries this label. |

**Calls after this record merges** (the accidental-trigger check of #324, the live qualification of #215, any future model-selection run) use floor 4 and cannot be compared with the floor-8 rows above without saying so. Tightening masking only removes digits from a prompt, so it exposes less; it never loosens a rule (SoT section 52 holds: no threshold is loosened).

## 5. What this record does not do

- It does not change the harness code. The model service still reads `masking.account_run_min_digits` and refuses a configuration without it, so **until the Engineering Lead's code half lands (noema#468: the loader stops reading the member, refuses an old configuration that still has it, and builds the redactor from the shared file) the model-service tests that load these two files fail.** The two changes must merge in the order that keeps `main` green; the order is on noema#468.
- It does not run anything live (INV-22: nothing held out is read; no provider key is used).

## 6. What would invalidate it

A requirement that the harness mask at a different floor than the product, for example to keep prompts comparable with a floor-8 run. No such run is being compared (no comparison arm runs before G3, A118).
