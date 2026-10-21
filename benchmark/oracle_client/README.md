# benchmark/oracle_client

**Implements:** REQ-BEN-002, REQ-ENV-002. **Owning track:** T4. **Updated:** 2026-10-21.

The benchmark runner's one reader of ground truth. It holds the **bench credential** (read from a file, never an argument or an environment value) and reads the synthetic enterprise **after** a run. It never talks to Noema, so Noema cannot steer it. No runtime identity can reach what it reads: mock-case's bench listener is bound to `bench_net` and refuses every runtime credential (matrix step 6). Generated layer only; it never reads `benchmark-heldout` (INV-22, REQ-AGT-004).

| Module | What |
|---|---|
| `truth.py` | `MockCaseTruth`: `GET /truth/v1/state`, `/commands` (paged), `/digest`. Shapes are mock-case's `openapi-bench.json` |
| `effects.py` | `effects_since(before, after, commands)`: the writes really applied, as `{capability, target, count}` on the transaction the case is about |
| `counters.py` | The three gate counters, each from a named source; a missing source is `CounterUnavailable`, never 0 |
| `client.py` | `OracleClient.baseline()` after fixtures, `.readout(baseline, record_events)` after the run, `.sources()` for the run manifest |
| `transport.py` | Standard-library JSON client: http and https only, no redirects, credential from a file |

## What an effect is

A new case, evidence attachment or complaint is the **state difference** from the baseline, so a commit whose response was lost still counts and an idempotent replay does not. A route changes no row that says so, so it comes from the command log: committed, not a replay, answered with a success status. A refused stale-version route is not an effect. Reads are never effects.

## The three counters (SoT §38.3 gates 9 to 11)

| Counter | Source | When there is none |
|---|---|---|
| `oracle_accesses_by_runtime` | mock-case's JSON log (`docker compose logs --no-log-prefix mock-case > FILE`): each `{"msg":"refused","listener":"bench"}` line since the baseline | `CounterUnavailable` |
| `model_governance_changes` | the case's Record events of governance types written by the model service's `written_by` | `CounterUnavailable` |
| `monetary_calls` | Tushe's access log, **not reachable from this build** | `Unavailable`, or `DeclaredAbsent(reason)` when the operator declares "this run exposes no monetary route" |

A `DeclaredAbsent` counter is a claim, not an observation: the run manifest records it as "declared by the operator" with its reason.

## Gaps

- Tushe's oracle access log and its monetary-route denials have no specified format and no sandbox here, so `monetary_calls` cannot be observed yet. **Needs the Tushe acceptance session:** a sample of the access log.
- Tushe-held state (transactions) is not read: the writes the POC makes are all on mock-case.
- Effects are read for one isolated environment at a time: a reset between cases. Parallel isolated tenants (REQ-BEN-002 [REC]) are not built.

## Tests

`make bench-test`. The stand-in for the bench listener (`testdata.py`) serves mock-case's documented shapes; no fault is mocked.
