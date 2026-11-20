# Session 2026-11-20 02:30 WAT · UND-16 (record-only pass)

## Session summary
- What changed (PR noema#811): the compile acceptance run has a record-only pass (A361, A372, the Architect's answer 5, relayed). After the five goldens it takes the five G1 inputs of BO's `benchmark/replay/g1_inputs.json` (noema#804) through the same compile service and recorder, each call under its own case id. `record_only.py` loads and proves the file (regular file, at most 1 MiB, strict JSON, the closed shape of BO's exporter, `inputs_sha256` recomputed by the exporter's rule; one fixed refusal code, no piece of the file in a message); `runner.run_cases` takes `record_only` and `record_one`; `run.json` lists the pass apart (`record_only`, `record_only_count`); `__main__` loads the file before the key check and before anything is built or written (exit 2 on a refusal), prices the pass in the dry run and prints it; `build.LiveBackend.record_one` compiles with an empty candidate source.
- Why: A372 says the next run also records the two arcs' own inputs so the replay provider covers US-01 and US-06; BO's file is the set.
- Risks, and how they are contained: benchmark-only code. No expectation is compared or invented (INV-22); the verdict and the exit code depend on the goldens only; a record-only failure is recorded and shown; the goldens run first, so they cannot be lost to the pass; the budget guard applies to every call; the file must be proven or the run does not start. No `.github`, contract, pack or setting change.
- Tests added (REQ-INT-001, REQ-BEN-004; INV-22, AGENT-12, AGENT-25): `benchmark/compile_acceptance/test_record_only.py` (loader, derived input, loop, run record, command) and two tests in `services/model/tests/test_compile_acceptance_build.py` (dry backend prices a record-only input with no socket; a live `record_one` through the real pipeline with no candidates, recorded under its own case id, no digit run over four, no key). `test_cli.py` gained a fake `record_one` and an `argv(..., record_only=)` helper; its other tests pass `--skip-record-only`.
- Regression cases added: the same tests.
- Hand mutants (34 on a copy of the tree, all killed after two tests were added for the stop-handling mutants): digest not compared; digest over the body with its own member; an extra key at each level; a second utterance; channel unchecked; duplicate keys; size; symlink; edge space; NFC; a locale hint sent; one case too many; a bool turn; duplicate ids; an id with a digit; one request id for all; the pass before the goldens; a failed compile not caught; an unexpected exception not caught or keeping its message; the stop not respected or not kept at the last input; a record-only error changing the exit code; the run record without the pass; a silent skip without a function; the inputs loaded after the key check; the refusal ignored; the pass never handed to the loop; the dry run not pricing it; the summary without it; the skip flag ignored; `record_one` keeping the goldens' candidates; the case id not set for the transport; a message carrying text.
- Debt entries: none.
- Conflicts raised: none.

## Open questions (for BO and the Architect; nothing invented)
1. **Candidates.** The goldens have a synthetic candidate list; the file is inputs only, so these cases are compiled with none. A replay of a G1 case whose real candidates differ finds no recording and fails closed. Do the G1 cases need their candidates recorded?
2. **Locale hint.** `locale_mix` is validated and not sent. Does the G1 run send a hint?

## Pre-work for the next agent
- Exact next step: noema#804 must be merged before the run is dispatched (the default run refuses without the file). After the run, UND-15 (noema#778) makes fixtures of the passing record-only calls like any other recording.
- Open questions (owner): the two above (BO, Architect).
- Files to read first: `benchmark/compile_acceptance/{record_only,runner,__main__}.py`, `benchmark/replay/g1_inputs.py` (BO's digest rule).
- Do not touch: `recording.py`'s manifest shape (UND-15 reads it closed); the guard.
