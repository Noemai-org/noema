# benchmark/runner

**Implements:** REQ-BEN-002. **Owning track:** T4. **Updated:** 2026-10-21.

Drives a generated suite through Noema's **public API only**, as a partner app would (PERD §39.2), applies each case's fault plan, advances the simulated clock, collects the Record, reads the environment through the oracle client (`benchmark/oracle_client`), and writes one `benchmark-case-result` per case plus a `benchmark-run-manifest` for the run. The scorer (`benchmark/scoring`) reproduces a score from those files. One code path: `run_case` is the only thing that runs a case. Generated layer only; it never reads `benchmark-heldout` or any oracle document (INV-22, REQ-AGT-004).

```
python -m benchmark.runner --suite benchmark/out/suite --out benchmark/out/run [--support] [options]
```

Exit 0: every case completed. 1: incomplete (an ERROR, or a case excluded for failing its schema). 2: an input is unusable, or an argument names the held-out set. **3: not run, no access** (a required part of the environment is missing; the run manifest says which, no case is run and nothing is faked).

## Order of one case

1. refuse the case if it needs a fault or an event kind this build cannot drive (before anything is touched)
2. reset mock-case, load the fixture, load the fault plan, take the oracle baseline
3. start the conversation (`POST /v1/conversations`) and wait until the investigation is at rest (PERD §28.2: the states that move on their own are in flight)
4. drive the timeline events, then the follow-up turns, each followed by a wait
5. fetch the reconstruction by correlation id and read the Record from it
6. read the environment through the oracle client: effects, and the three gate counters

A timeline's offsets **order** its events; they are not waited for. The clock is simulated and only `clock_advance` moves it. An event at `+00:00` runs before the first input, every later one after the first input has settled, and follow-up turns after the events (the cases that need a different interleaving would need a per-event step in the case schema).

## What it can drive, and what it refuses

| Interface | Used for | Source |
|---|---|---|
| Public API | `POST /v1/conversations`, `/messages`, `/v1/confirmations/{id}`, `GET /v1/investigations/{id}`, `/events` (SSE), `GET /v1/reconstructions/{id}` | PERD §39.2, §39.4 |
| Admin API | `kill_switch:on\|off` | PERD §39.4 |
| mock-case control | reset, fault plan, `clock_advance` | `synthetic/mock_case` README |
| Timeline kinds driven | `session_start`, `clock_advance:<ISO duration>`, `confirm:ACCEPT\|DECLINE`, `kill_switch:on\|off` | `drivers.py` |

Everything else is an **ERROR with a code**, never skipped (REQ-BEN-001): `EVENT_KIND_UNSUPPORTED` (no driver), `FAULT_UNSUPPORTED` (the source is not reachable, or mock-case does not produce the effect), `FIXTURE_UNAVAILABLE`, `SESSION_UNAVAILABLE`, `ORACLE_COUNTER_UNAVAILABLE`, `NOT_SETTLED`, `API_REFUSED`, `API_FAILED`, `API_UNREACHABLE`, `RECONSTRUCTION_UNEXPECTED`, `FAULT_PLAN_REJECTED`, `RESULT_INVALID`.

`python -m benchmark.runner --suite DIR --out DIR --support` prints how many cases this build can run and what stops the rest. On the default suite today: **125 of 320 can run** once the core and a fixture loader exist; the rest are blocked by faults on Tushe-held sources (`mock-customer`, `mock-ledger`, `mock-switch`, `mock-destination`), by effects mock-case does not produce, and by event kinds with no driver (webhook relay, process control, provider control, the policy and session APIs). Faults relative to the grant's lifetime need `--grant-ttl-seconds`.

## Fault plans

The generator's plan files are provisional (PERD §37.1 shape). `faultplan.py` translates the faults aimed at `mock-case` into mock-case's own format (`route` to `route_case`, `committed` to `after_commit`, a delay or Retry-After "past the grant" to seconds from `--grant-ttl-seconds`) and lists the rest as unsupported. mock-case accepts only a plan it can apply exactly; `python -m benchmark.oracle_client.live_check` loads every translated plan of the default suite into a real mock-case (46 today) and none is refused. The simulated clock starts at `2026-10-01T09:40:00+01:00` (the PERD §37.1 example, SYNTHETIC).

## Gaps (nothing is faked)

- **Reconstruction format is provisional** (`reconstruction.py`): PERD §39.4 gives no field names. The runner reads the smallest shape the scorer needs and derives `audit_events`, `clarification_asked` and `escalation_opened` from the Record. **Needs session A:** the real response, or a recorded body in `benchmark/runner/recorded/` (a test then forces `PROVISIONAL = False`).
- **No fixture loader** (`UnavailableFixtureLoader`): how a fixture field maps to a Tushe call is the adapter task's, and Tushe's sandbox is not reachable here. Until one exists a run is `NOT_RUN`. Cases whose fixture already holds enterprise cases also need a way to seed them into mock-case (the bench credential cannot create a case).
- **Session tokens** come from a directory (`--tokens-dir`, one file per principal id) until the synthetic IdP exists.
- **`monetary_calls`** has no observable source (Tushe's access log); the operator may declare "no monetary route" with a reason (`--declare-no-monetary-route`), which the run manifest records as declared, not observed.
- **No runner service in Compose.** PERD §65.1 puts the runner on `bench_net` and has it call the public API as a partner app; the network that gives it a route to the core's public API is a topology decision this PR does not make. **Needs session A.**
- One isolated environment at a time (a reset between cases). Parallel isolated tenants (REQ-BEN-002 [REC]) are not built.
- Replay (REQ-BEN-002 acceptance) is two runs of the same manifest compared by the scorer (`--replay-results`); mock-case's `/truth/v1/digest` is available to compare the environment state of two runs.

## Tests

`make bench-test`. `FakeNoema` (`testdata.py`) is a stand-in for the core's public API, which does not exist yet; it injects no fault and records every request so a test can prove the runner never leaves the public API. `benchmark.oracle_client.live_check` runs the oracle client and the translated plans against a real mock-case and PostgreSQL.
