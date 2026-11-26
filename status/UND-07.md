# Session 2026-11-25 · UND-07 (C2: out-of-scope and prohibited intents end to end; REQ-INT-005, REQ-CAP-002)

## Session summary
- What changed (PR links): one test file, `services/core/orchestrator/portadapters/trustpack/und07_refusals_test.go`. No production code. This note.
- Why: the Done-when is "US-10, US-22 and PUS-03 pass". What merged already does the work: the pipeline stops an out-of-scope request at `fin.meta.out_of_scope` with no slots (`understanding/pipeline/run.py`, tests in `test_pipeline.py`, `test_compile_assemble.py`); T4 refuses a prohibited or unregistered capability and T5 refuses a node without a trust profile (`trust/intake/gates.go`). What was missing is one test that runs the REAL intake over the REAL pack adapters and asserts the whole of what the PERD asks. This is that test (characterisation, no red: the behaviour exists). Its teeth are shown by hand mutants.
- What the test proves (real `trustpack.Capabilities`, `Scope`, `Authority` of the shipped pack; counting doubles for every port that reads about a target): every prohibited capability of the pack, named by an intent of the investigation node and for every role (customer, ops, audit, none), is REJECT `PROHIBITED_CAPABILITY` at T4; a capability the registry does not know (and near-spellings: upper case, a space) is the same refusal (INV-18, BD-08); the out-of-scope node, an unknown node and an empty node are REJECT `OUT_OF_SCOPE` at T5; T4 comes before T5; in each case NO candidate, ownership, evidence, reconciliation, policy, clock, confirmation, plan or dispatch call is made, no grant exists, and a recorded decision (if the intake records one) has no step and no plan hash. A control shows an allowed capability passes T4 and T5.
- Hand mutants, all killed: prohibited never reported; unknown capability reported registered; scope always true; T4 ignoring Prohibited; T5 not refusing.
- Risks: none (tests only).
- Tests added (REQ-INT-005, REQ-CAP-002; INV-05, INV-16, INV-18, BD-08; US-10, US-22, PUS-03): the file above.
- Regression cases added: the same. Debt entries: none.
- Conflicts raised: none.

## What this does NOT cover, and who owns it
1. **The refusal is not recorded by the intake today.** `intake.evaluate`/`run` return the REJECT of T1 to T9 without calling `Ports.Recorder`; only T13 and T15 record (`gates_t13_t15.go`). PERD 23.1 and REQ-TRU-008 want the Decision Record first for every outcome (A456: "the Recorder port serves every outcome"). The early-decision key and its null basis are A574, A577, A581 (migration 0038 merged): C1's. The test is written to hold before and after that change. **C1: record the early REJECT at T4 and T5 (with the null-basis form), then the assertion "exactly one record, no step, no plan hash" can be made strict.**
2. **"The customer is told what Noema can do instead" has no pack data.** The explanation templates are keyed by outcome and classification (`fin.outcome.*/<classification>`), and `check_templates` refuses other keys; no template exists for a refusal reason (`PROHIBITED_CAPABILITY`, `OUT_OF_SCOPE`). A decline text is new pack data plus a renderer path: a contract and pack-schema question (AGENT-15) and an explanation-module change, not a side edit. **Architect: where does the decline text live (a template keyed by core reason code, in the pack, SYNTHETIC; rendered by the explanation module)?** Safest default if none is ruled: the UI shows a fixed generic decline with no internal code.
3. **Benchmark phrasings** (PERD REQ-INT-005 acceptance: at least 3 per prohibited capability across locales) are BO's.

## Pre-work for the next agent
- Exact next step: C1 records the early REJECT; the Architect rules on the decline template; then a pack-data PR (C2) adds the template and a renderer test.
- Files to read first: `trust/intake/gates.go` (t4, t5), `trust/intake/gates_t13_t15.go` (the record step), `packs/fintech/templates/README.md`.
- Do not touch: `trust/intake` (C1's), the held-out set.
