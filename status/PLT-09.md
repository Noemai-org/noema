# Status · PLT-09 · Static-analysis gates

Updated 2026-11-03. Owner: EL+AG in tasks.csv; executed by the Engineering Lead role agent. Task: golangci-lint with `errcheck`, `contextcheck` and a `panic` ban outside `main` and tests; `mypy --strict`; Ruff with the ASYNC rules; each a required check (REQ-TST-010; DEC-2026-10-31 D24).

## State
- In progress. Part 1 (this PR): the findings in `services/core` that are the Engineering Lead's to fix. The gate itself (pinned config, CI jobs, one fixture per rule) is not added yet, because it would fail `main` on the findings below.
- The baseline was measured on 3 Nov 2026 with golangci-lint 2.5.0 and `errcheck`, `contextcheck` and `forbidigo` (`^panic$`), with the tool's cap of 3 identical findings **turned off** (`--max-same-issues=0 --max-issues-per-linter=0`). An earlier count of 23 used the default cap and **undercounted**: the true baseline is **27**.

| Module | Before | After this PR | Owner of the rest |
|---|---|---|---|
| `services/core` | 10 | 1 (`platform/ids/ids.go:81`, `panic` in `Generator.Must`) | Core Engineer 1 (see below) |
| `benchmark/hostility_proxy` | 6 | 6 | Integration Lead |
| `synthetic/mock_case` | 6 | 6 | Integration Lead |
| `synthetic/webhook_relay` | 5 | 5 | Integration Lead |

## Open
- **`ids.Generator.Must` (Core Engineer 1).** `ReceiptID()` returns a string and calls `Must`, which panics when the entropy reader fails (the reader is injectable, so this path is real). The fix changes `execution/controller.IDs.ReceiptID()` to return an error, which touches `controller.go` and `recover.go`, where the held EXE-04 branches (`noema#90`, `noema#94`) also work. Do it after those merge, or as part of EXE-07/EXE-08, to avoid conflicts.
- **The 17 findings in the Integration Lead's three modules** (CODEOWNERS and SR-19 keep core engineers out of `mock-case` and the proxy): 8 unchecked `Close` calls, 2 `context.Background` in non-root functions (`hostility_proxy/internal/proxy/proxy.go:191`, `webhook_relay/cmd/webhook-relay/main.go:81`) and 7 `panic` calls (`hostility_proxy/internal/jsonx/jsonx.go:37`, `internal/proxy/proxy.go:201`, `internal/proxy/state.go:54`; `mock_case/internal/api/benchapi.go:125`, `pipeline.go:116` and `142`, `internal/store/store.go:222`). The exact list is in the PR body of the PLT-09 pull request.
- **Python.** `ruff check --select ASYNC` is clean on `services/model`. `mypy --strict` has not been measured: the sandbox lacks the dev dependencies, so the baseline must come from a CI run.
- **Pins.** The tool versions in CI must be pinned (D24; `tools/ci/check_pins.py` rules). Not yet done.

# Session 2026-11-03 · PLT-09

## Session summary
- What changed (PR links): `services/core/record/chain` loads its actors table through `loadActors`, which returns an error (`ErrActorsTable`), instead of an `init()` that panics; every append (`validate`) and `Verify` refuse when it did not load. Four mechanical fixes (three unchecked `Close` calls, one shutdown context). This file.
- Why: REQ-TST-010 and the Founders' work order of 3 Nov, item 5 (PLT-09, pin the dev dependencies).
- Risks, and how they are contained: `record/chain` is a P0 path (Record). The change keeps the same sets and the same checks; it only moves the failure from a panic at start-up to a refusal on every append and verification, which is visible and tested (`TestAFailedLoadRefusesEverything`). The shipped table is tested to load.
- Tests added (REQ, INV, US or EC IDs): REQ-TST-010, REQ-AUD-001, INV-11: `actors_load_test.go` (the shipped table loads; 12 malformed tables are refused; a failed load refuses everything). Seen failing first (did not compile).
- Regression cases added: none. Debt entries: none (no baseline list and no `nolint` was added). Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Integration Lead fixes the 17 findings in their three modules; Core Engineer 1 changes `ReceiptID()` after EXE-04 merges; then the gate PR (config, CI, fixtures, pins) can land green.
- Do not touch: `noema#94`, `noema#90`.
