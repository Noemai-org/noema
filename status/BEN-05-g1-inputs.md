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
