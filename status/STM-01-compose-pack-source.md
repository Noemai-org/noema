# Status · STM-01-compose-pack-source · lane 1 of the G1 final composition: the pack source and what is built from it

Updated 2026-11-23 WAT. Owner: Engineering Lead (lane run by a subagent; unreviewed work). REQ-STM-001, REQ-TRU-004, REQ-TRU-005, REQ-OPS-003; INV-14, INV-16, INV-21; AGENT-13, AGENT-16. Rulings (all "interim, relayed, not yet committed"): A537 (the four lanes), A333 and A377 items 6 and 7 (PackVersion from the manifest-verified pack), A469 and A486 (a part not built is a named gap; an invalid value is fatal), A571 (a partial pack-settings set is a named gap), A475, A510 (an action graph is served only from the manifest's pins). All data is SYNTHETIC.

## State

- **Red pushed** (`903b531b`): `trustpack/verified_test.go`, `cmd/core/compose_packsource_test.go`, the changed `main_compose_adapters_test.go`, and stubs (`LoadVerified`, `Pack.Ref`, the rename `errPackNotOnMain` to `errPackNotConfigured`).
- **Green held locally, not pushed** (EL pushes after reading the red job).
- DB tests of `cmd/core` run in CI only (pgsodium); 11 of them fail locally for that reason and have nothing to do with this lane.

## Component table

| Component | State | Where |
|---|---|---|
| Pack source (production `PackSource`) | built | `cmd/core/compose_packsource.go`: `packload.Load` then `trustpack.LoadVerified`; settings `NOEMA_PACK_DIR`, `NOEMA_PACK_MANIFEST`, `NOEMA_PACK_MAX_BYTES` (the Understanding half's; no new setting) |
| Graphs served from the verified source | built | `trustpack.LoadVerified` reads graphs from the verified bytes only (in-memory root); a graph the pack names and the bytes lack, or the bytes hold and the pack does not name, is `ErrPack` |
| PackVersion | built | `Pack.Ref()` = `<pack_id>@<pack_version>`; equals the Interpreter port's `view.Ref()` (tested) |
| Capabilities, Scope, Authority | built | already built by `trustAdaptersSource`; now reachable because production has a pack |
| Candidates | **named gap** | `trustport.NewCandidates` needs a Reader (`evidence/candidates_api` is empty), a Ranker (REQ-AMB-003 is Python) and a Labeller (the pack has no attribute order or label templates), and it is one instance per tenant while `TrustAdapters.Candidates` is one port. No fake, no quiet nil: `/readyz` names `trust.Candidates` |
| CorrelationOf (A377 item 7) | not in this lane | it is a `payloadread.CorrelationOf` of the payload reader (`wire_screened_payloads.go`, `intakesources/lists.go`), part of Evidence (lane 2); not touched |

## Behaviour of the pack settings

- None of the three set: the pack is a named gap (`errPackNotConfigured`): the process starts, `/readyz` names `trust.Pack` and every adapter (A469, A486).
- Some of the three set (A571, interim, relayed): a NAMED GAP, not fatal: `/readyz` names `trust.Pack` and, for each missing setting, `trust.Pack: <NAME> missing` (fixed text, never a value), exactly as the Understanding half does. An invalid present bound stays fatal beside a missing one, naming the setting, never the value.
- A file changed after the manifest was made, a link, an unlisted file, an unpinned graph, an id other than the manifest's: fatal with `packload`'s text (a closed kind, no path). A pack that verifies but that the Trust adapters cannot use: fatal with fixed text; the cause stays behind `errors.Is` (`trustpack.ErrPack`).
- A guard test fails if any non-test file of `cmd/core` calls `LoadPack` (a second read of the directory).

## Session 2026-11-23 · STM-01-compose-pack-source

- What changed: see the table. Files: `trustpack/verified.go`, `trustpack/pack.go` (the `ref` field, one line), `cmd/core/compose_packsource.go`, `cmd/core/wire_trust_adapters.go` (the `productionPack` stub removed, the rename, `PackVersion: p.Ref()`), tests, the trustpack README, `status/README.md`.
- Why: `/readyz` named `trust.Pack`, `trust.Capabilities`, `trust.Scope`, `trust.Authority` and `trust.PackVersion` as not built although all of them can be built from the merged pack load (#773) and the pinned graphs (#879).
- Risks, and how they are contained: the pack is read once (packload) and the Trust Pack is built from those bytes, so a file changed between the check and the read is never served (INV-21). Errors carry no path, value or pack text (AGENT-13).
- Tests added (REQ-STM-001, REQ-TRU-004, REQ-TRU-005; INV-14, INV-16, INV-21): `trustpack/verified_test.go`, `cmd/core/compose_packsource_test.go`; `main_compose_adapters_test.go` changed (PackVersion is built).
- Hand mutation check: 15 mutants; 13 were killed at first, and the 2 survivors were killed by two corrected or added tests (15 of 15 now): the unusable-pack case now plants a member the contract lacks so the cause's text quotes it, and a graph with the pack file's name is refused).
- Two red tests were corrected in the green commit: the unpinned-graph case accepts `packload.ErrUnlisted` as well as `ErrGraph` (dropping a manifest entry yields the first), and the unusable-pack case (above).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when the Candidates reader, ranker and labeller exist, build `CandidateNodes` from the pack through one read-only accessor (A475) and a per-tenant router, then drop the gap.
- Open questions (EL): (1) Candidates: is a per-tenant router in `TrustAdapters.Candidates` the intended shape? (2) `NOEMA_PACK_MAX_BYTES` is shared with Understanding (#900): both load the pack; should one load be shared (a `PackSource` memo) once #900 is on main?
- Files to read first: `cmd/core/compose_packsource.go`, `orchestrator/portadapters/trustpack/verified.go`.
- Do not touch: `platform/packload`, `trust/**`, `wire_understanding*.go` (#900).
