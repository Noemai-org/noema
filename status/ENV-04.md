# Status · ENV-04 (Integration Lead) · normalised adapter payloads

Updated 2026-11-06. Plan task ENV-04 (owner IL+AG; REQ-ENV-001, REQ-NFR-006). Ruling DEC-2026-11-06 A10 (proposed, docs#117): the Integration Lead owns the stand-in and the Tushe adapters, built on the PERD 34.4 shapes; the Tushe route mapping follows Tushe's final written response (R13, due Wed 11 Nov 2026).

## State

- Slice 1: the read ports `Transactions`, `Ledger`, `Switch`, `Destination` and the `fin.ledger_entry`, `fin.switch_record`, `fin.destination_status` payloads with `Validate` (`services/core/adapters/fin.go`, `fin_test.go`). SYNTHETIC label on the raw switch code is enforced.
- Not built: any client of hosted Tushe (no route is mapped; nothing is inferred from Tushe deck v4, which Noema has not observed); beneficiary and principal-context reads (PERD 34.4 fixes no normalised shape for them); (nothing).

- Slice 2 (same PR, 6 Nov): synthetic_tushe.StandinClient, the stand-in adapter A10 gives this role (Core Engineer 2 confirmed on noema#174 that they are not building it), to the Evidence Layer's expectations; Provenance (wire hash and label) on every read.

## Open points recorded, not decided

1. **Settlement states. Resolved by A31 (DEC-2026-11-06):** REVERSAL_PENDING is non-terminal and never SUCCEEDED, FAILED or REVERSED; a decision on it is wait or escalate. The port keeps it as its own state and `SettlementState.IsTerminal` says so (UNAVAILABLE stays a read result, not a state).
2. **`ReadError.Error()`** still says "case read failed" for every read; harmless, to be generalised in a later slice.

## Messages

### 2026-11-06 accepted · Founder → Integration Lead
- Purpose: work queue of 5 Nov 15:30: item 5, Tushe adapters per the committed decision records; collect Founder clicks into one numbered click-script.
- Used it for: this slice; R13 (ENV-05 pause lifted) and A10 as the authority; the route mapping left unbuilt because A10 says it follows Tushe's response.
- Evidence: this pull request.

# Session 2026-11-11 · ENV-04 (bench wiring, Compose and credentials side)

## Session summary
- What changed: `bench-runner` service (bench_net + edge_net, bench profile only) and its image; `bench-stack.yml` overlay (the core's stand-in settings, the proxy on the stand-in route table, the runner's output directory); `make bench-up` and `bench-down`; `profiles.py` resolves the bench profile with both overlays and has new rules and 11 new self-test mutations (the runner placement and networks, no bench credential volume on the core or the model); runner flags `--standin-credential-file` and `--proxy-credential-file`; the `bench-g1-stack` dispatch workflow (mock-case live log captured; not dispatched).
- Why: the Architect's order of 11 Nov (noema#441): the runner could not reach `bench_net`, three services generate three bench credentials, and the mock-case log must be a live file.
- Risks, and how they are contained: the runner joins `edge_net` and `bench_net` (the one bridge, by design); `profiles.py` fails if the core or the model ever join `bench_net` or mount a bench credential. The core settings are inert until the call site exists; the workflow ends NOT RUN until tokens and the call site exist, and fails on purpose.
- Tests added: `benchmark/runner/test_cli.py` PerServiceCredentials (4, red first); `profiles.py --self-test` (41 mutations, all caught).
- Conflicts raised: none. Not done, and not mine: session tokens (Edge), the core's read call site (Evidence Layer, EL/C2).

## Pre-work for the next agent
- Exact next step: when the Edge issues bench session tokens, add `--tokens-dir` to the workflow's runner line; when the core reads the stand-in, check the `NOEMA_STANDIN_*` names against its config.
- Open: the proxy's route table attributes the principal from a bare `X-Principal` header, which the adapter does not send (INV-12); the request log's principal field will be empty until the table follows the assertion (owner: IL, next).
- Files to read first: `deploy/compose/bench-stack.yml`, `deploy/compose/services/bench-runner.yml`, `.github/workflows/bench-g1-stack.yml`.
- Do not touch: the hostility proxy's data plane; `benchmark-heldout`.
