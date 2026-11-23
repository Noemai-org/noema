# Session 2026-11-23 · UND-16 (live compile run 6 findings)

## Session summary
- What changed (PR noema#901): `fin.lex.pcm.json` (Pidgin `dis morning`), `load_pack_lexicons`/`merge_lexicons` (compile config member `lexicon_dir` replaces `lexicon_file`), optional-slot question wording, `InputInvalidReason` on `InputInvalid` and `PipelineResult`, benchmark failure text `InputInvalid:<REASON>`, `summary.md`, the Summary and Result steps of `bench-live-compile.yml`.
- Why: run #6 (45120524373): G-A, G-C, G-D MISSED on `requested_remedy`; G-B, G-E and three record-only inputs ERROR (InputInvalid). The artifact could not be downloaded here, so the diagnosis is from code and from the committed recording of the earlier live run (rec-0006: for G-A the model chose `fin.cap.lookup_transaction` at p 0.78).
- Risks, and how they are contained: the prompt wording is a hypothesis until the next run's recordings; G-B and G-E need a product decision (CONFLICT block in the PR); no golden was edited (A533).
- Tests added (REQ-INT-003, REQ-CTX-006, REQ-INT-002, INV-14, INV-21): `services/model/tests/test_compile_run6.py`, `benchmark/compile_acceptance/test_summary.py`, additions to `test_compile_wiring.py`, `test_compile_acceptance_build.py`, `tools/ci/test_bench_live_compile.py`.
- Regression cases added: the same.
- Debt entries: the lexicon files are not pinned in the pack manifest (the run pin carries their merged digest).
- Conflicts raised: requests with no time expression (G-B, G-E) are refused at the gate (REQ-CTX-006); see the PR.

## Pre-work for the next agent
- Exact next step: read the recordings of the next run; compare with "What the recordings must show" in the PR.
- Open questions (owner): no-time-window utterances (Architect).
- Files to read first: `compiler/lexicon.py`, `pipeline/steps.py` (`_slot_prompt`), `pipeline/run.py`.
- Do not touch: the goldens, without a quoted recording (A533).
