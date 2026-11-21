# Session 2026-11-19 14:30 WAT · UND-16 · the acceptance run's pool covers the pipeline's fan-out (Core Engineer 2)

## Session summary
- What changed (local branch `fix/UND-16-pool-covers-fanout` from main `cb77c746`; no PR yet):
  - `benchmark/compile_acceptance/build.py`: `POOL_SIZE` 4 to 6, with the reason in its comment.
  - `benchmark/compile_acceptance/test_pool.py` (new, runs in `make bench-test`): reads the pipeline's fan-out from `services/model/.../pipeline/run.py` by `ast` (the benchmark environment does not install the model service) and asserts `POOL_SIZE` is the fan-out plus one spare.
  - `services/model/tests/test_compile_acceptance_pool.py` (new; outside the card's allowlist, see the silences): the real pipeline and harness against a scripted model behind the real `BoundedBlockingPool`, at `POOL_SIZE` and at 4, and that `build_backend` builds the wire transport with `POOL_SIZE`.
- Why: compile run 44521380842 (gpt-6-luna). In each of G-A to G-D the fifth of the five calls the pipeline starts together (enumerated slots, flags, locale, slot mentions, quiet screen) was refused at once by the full pool (`UNAVAILABLE`, no status), and the refused call was the quiet injection screen, so the screen was silently empty.
- Risks, and how they are contained: `POOL_SIZE` is a run setting, not a safety threshold (AGENT-16 does not apply); it only raises the calls in flight at the provider from 4 to at most 6, within one case. Nothing in the model service changed.
- Tests added (REQ-INT-002, REQ-TST-010, INV-16): the fan-out is 5; the pool is fan-out plus one spare, and no larger; at `POOL_SIZE` five calls are in flight, none refused, the screen answered; at 4 the fifth is refused and the screen is empty (the control, reproducing the finding); the transport is built with `POOL_SIZE`.
- Regression cases added: the control above.
- Debt entries: none.
- Conflicts raised: none.

## The number
`POOL_SIZE = 6`: the five calls of `run.py` step 2 (one in flight per step; failover is sequential and the acceptance configuration has one provider), plus one spare, because a worker thread cannot be cancelled and a call whose caller timed out keeps its slot until the thread returns (`platform/blocking`). Five passes the first of the two checks and fails the spare. The second screen (of the candidates) runs alone after the gather.

## Silences (A332)
- Distinguishability: a pool-refused call is NOT distinguishable from a provider outage in the recording or in the harness's `CallRecord`. The transport raises `ProviderError(UNAVAILABLE, local=True)`, and `RecordingTransport` writes only `err.kind.name`, so both are `failure: UNAVAILABLE`, `status: null`; `AttemptRecord` has no `local` field. The harness does tell them apart in one place only: a local refusal is not charged to the provider's breaker. Telling them apart in the recording needs a recording field or label (UND-15 replays from these files), so no test and no field was added. Candidate for a ruling.
- Related, unchanged: `RecordingTransport` settles a failed call at its projection, so a call the pool refused (nothing sent) was charged its projected cost in the run (for example rec-0002, 2,230 micro-dollars). That overstates spend; not changed here.
- The file `services/model/tests/test_compile_acceptance_pool.py` is outside the allowlist `benchmark/compile_acceptance/**`. It is the only test that runs the real pipeline against the real pool and that checks the wiring of `POOL_SIZE` into the transport (neither can run in the benchmark environment). It can be dropped from the PR; then mutants 7 to 9 below survive and the AST check is the only guard.
- The `ast` check reads the width of the one `asyncio.gather` in `run_pipeline`; a second concurrent start elsewhere would not be seen by it (the dynamic test would see it). Exactly one gather without a starred argument is required, or the test fails and must be taught the new shape.
- Not run: any call to the provider. The next acceptance run is the evidence that the screen is answered.

## Mutants (15; each restored; run on a copy of the tree)
Run against `test_pool.py` (bench) and `test_compile_acceptance_pool.py` with `test_compile_acceptance_build.py` (model). All killed:
1 `POOL_SIZE = 4`. 2 `= 5`. 3 `= 7`. 4 `= 0`. 5 `= 6.0`. 6 `= True`. 7 transport built with `POOL_SIZE - 1`. 8 with `POOL_SIZE + 1`. 9 with a literal 4. 10 `run.py`: a sixth awaitable in the gather. 11 `run.py`: one awaitable removed. 12 `run.py`: `asyncio.wait` for `asyncio.gather`. 13 `run.py`: a starred gather. 14 `BoundedBlockingPool`: refuses only above its size. 15 `BoundedBlockingPool`: allows one extra call. (5 is killed by the spare rule only; the pool-size rule is a policy, not a measurement.)

## Pre-work for the next agent
- Exact next step: re-run the compile acceptance; check that every `pipeline_slot_mentions` and screen call has a status (no `UNAVAILABLE` with `status: null` except a real outage).
- Open questions (Architect): whether the recording should label a locally refused call; whether a refused call should be charged.
- Files to read first: `benchmark/compile_acceptance/test_pool.py`, `services/model/noema_model/platform/blocking/__init__.py`.
- Do not touch: the pipeline's fan-out and `BoundedBlockingPool` (model service).

# Session 2026-11-20 · UND-16 · the fan-out reader and noema#792 (Integration Lead)
- `main`'s `benchmark` job went red after noema#792: `run_pipeline` gained a second `asyncio.gather` (the candidates' screening, one awaitable), and `test_pool.py` required exactly one. The reader now takes the widest gather (still 5), refuses a starred gather and a pipeline with none, and counts created tasks as before. Four unit cases on synthetic source were written first and failed.
- Not changed: the pool size (6) and the rest of C2's test.

# Session 2026-11-20 (later) · UND-16 · the fan-out is the sum of all gathers (Integration Lead)
- A480 (noema#832 follow-up): `fan_out_of` sums the calls every `asyncio.gather` of `run_pipeline` starts (a bare-name argument is an existing task or coroutine, not a new call) and takes the larger of that and the tasks `create_task` makes. The real pipeline's is 5 against a pool of 6 (unchanged). A synthetic pipeline of two gathers (three and two calls) is 5, not the widest 3; that case failed first.
