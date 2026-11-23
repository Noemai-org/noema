# Status · BEN-05 g1_inputs (Benchmark Owner agent) · the inputs of five G1 cases, for C2's compile run

# Session 2026-11-19 · BEN-05 · `benchmark/replay/g1_inputs.json`

## Session summary
- What changed: `benchmark/replay/g1_inputs.py` (the exporter), `g1_inputs.json` (its output) and 9 tests. `python -m benchmark.replay.g1_inputs --write` regenerates the file and `--check` fails when the committed file is not equal to a fresh export.
- What it holds: for BEN-CLEAN-0001, -0014, -0027 and BEN-TMO-0001, -0018 of the generated suite at seed 20261020: case id, channel, locale mix and the utterances. Nothing else: no expectation, no oracle reference, no fault plan, no category, no goal and no principal (INV-22; the generated case file carries an `expected` block, which is never copied).
- Digest-pinned: the generator (`noema-casegen`, version **1.3.3**; the order named 1.3.0, but the generator in this repository is 1.3.3, so that is what the file records), the seed, the suite manifest digest `sha256:c6bdd3a6...0191` (the value pinned in `test_generate.py`), the digest of the whole suite tree (the rule of `benchmark.generator.reproduce`), each case file's digest from the manifest, and a digest of the exported inputs. Reproducible: the committed file must equal a fresh export.
- Tests: 9, written first and seen failing (module missing); one more failure was found in my own test (it scanned the label, which says "no oracle reference"), and the manifest digest was first computed by the wrong rule and caught by the pinned value; mypy --strict and lint clean. **No mutation run:** each test run takes about 200 seconds on Windows (the suite is generated several times), so mutants were not run; CI on Linux is faster and the pinned digest and the equality-to-a-fresh-export test are the guards.
- Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: C2 records compile outputs for these five cases in the next compile run (noema#546, IL), and the replay provider reads them.
- Do not touch: the exporter's list of keys; adding any case field needs a check that it is an input and not an expectation.

# Session 2026-11-20 · BEN-05 · A439: the candidate world rows

## Session summary
- A439 (rulings of 20 Nov, the record-only compile pass): the candidate world rows (inputs only, no oracle labels) are added to `g1_inputs.json`, because candidates are part of the request. Each case now also carries the synthetic session's principal (`principal_id` and `roles`) and `candidates`: the beneficiaries the principal owns (`beneficiary_id`, `name`, `aliases`, `account_last4`) and the transactions it owns (`ref`, `direction`, `amount_minor`, `currency`, `age_minutes`, `beneficiary`). Every row of the five cases comes from the case's own generated fixture.
- What is deliberately not exported: the world's state fields (`ledger`, `switch`, `destination`, `settlement`: what a case is built to find out, so a label in all but name), `owner` (redundant with the principal), `display_name`, `enterprise_strings`, and rows of any other principal (a test adds a row of another principal and checks it is never exported). The allowlist is the assumption to confirm: C2 says on the PR whether the Interpreter adapter needs a field I left out.
- Canonical locale (A439: the locale sent is the canonical locale only; locale_mix is not sent): the generated case names no canonical locale. `canonical_locale` is the only entry of `locale_mix` when it has one, and **null** when it has several (BEN-CLEAN-0027 is `pcm` and `en-NG`); the rule is written in the file. I did not choose one for C2. C2 or the Architect names the rule for mixed cases and I change one function.
- Tests: 12 (3 new for A439, written first and seen failing against the old exporter); mypy --strict and lint clean; the file's `inputs_sha256` is now `sha256:3c74262b...a214`. No mutation run (each test run is about 230 seconds on Windows).
- Regression cases: none. Debt: none. Conflicts: none.

# Session 2026-11-20 (second) · BEN-05 · A460: the canonical locale is en-NG for all five cases

## Session summary
- A460 (rulings of 20 Nov): the locale is en-NG for all five cases (what the UI sends). `canonical_locale` is now the constant `"en-NG"` for every case, `locale_mix` stays a tag only (the generated case's own, unchanged) and no locale hint is exported. This replaces my earlier "null for a mix" rule, which was only there because no ruling named one. The rule is written in the file's `canonical_locale_rule`.
- The candidate world rows (A439) are as in the previous section; A460 also settles them: the fixture sits under `benchmark/replay/`, pinned to this file's `inputs_sha256`, and CI proves it equals fresh output (`python -m benchmark.replay.g1_inputs --check`, and a test); the Go test that builds the five canonical inputs is C2's, in `orchestrator/portadapters/interpreter`.
- `inputs_sha256` is now `sha256:874b2289...cf26`. Tests: 12 pass (the canonical-locale test was rewritten for the ruling); mypy --strict and lint clean. No mutation run (each test run is about 270 seconds on Windows).
- Why a new PR: noema#804 merged before the A439 and A460 commits, so they come in a new PR off current main.

