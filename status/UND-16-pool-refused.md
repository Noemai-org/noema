# Session 2026-11-19 18:00 WAT · UND-16 (POOL_REFUSED)

## Session summary
- What changed (PR noema#794 or the number the PR carries): the recorder has a closed failure class `POOL_REFUSED` (A372). `RecordingTransport` (`benchmark/compile_acceptance/build.py`) labels a call that the model service's own full worker pool refused (`ProviderError.local`, nothing sent) `POOL_REFUSED` in `failure` and `failure_class`; the manifest counts it apart from `UNAVAILABLE`. The error is re-raised unchanged, so the harness still fails over. Only the label is recorded: no message, no body (SR's condition, A359).
- Why: in compile run 44521380842 a pool-refused call looked like a provider outage in the recording (noema#777, silence 2; noema#779 added classes but not this one).
- Risks, and how they are contained: a benchmark-only recorder label; no setting, no contract, no production code. `local` is set only by the transport's pool refusal inside `RecordingTransport` (the budget refusal is raised before the call is recorded and is not recorded).
- Tests added (REQ-INT-001, REQ-INT-002; INV-22, AGENT-12, AGENT-25): `benchmark/compile_acceptance/test_recording.py` (own class, manifest count, closed vocabulary now includes POOL_REFUSED); `services/model/tests/test_compile_acceptance_build.py` (real `UrllibTransport` with a real `BoundedBlockingPool` held full by a blocked call: recorded POOL_REFUSED, raised unchanged, no key in the files; a non-local failure keeps its own class). The closed-vocabulary test was changed to add the new member; nothing was weakened.
- Regression cases added: the same tests.
- Hand mutants (6, all killed): always the kind name; inverted `local` test; always POOL_REFUSED; label not a known transport label; vocabulary without the member; non-local collapsed to UNAVAILABLE.
- Debt entries: none.
- Conflicts raised: none.

## Open question (for the Architect, no change made)
A pool-refused call (nothing sent) is still charged its projection by the budget guard (2,230 micro-dollars in run 44521380842). Charging it 0 would change what the guard counts, a cost-guard behaviour. It is not changed here.

## Pre-work for the next agent
- Exact next step: UND-15 (noema#778) reads `failure_class`; a POOL_REFUSED recording is a negative fixture like any failed call.
- Open questions (owner): charge of a locally refused call (Architect).
- Files to read first: `benchmark/compile_acceptance/{recording,build}.py`.
- Do not touch: the guard's accounting.
