# Status · UND-04 · the manifest pins the action graphs (A510)

# Session 2026-11-22 · UND-04 (C2)

## Session summary
- What changed (PR links): noema#879. The pack manifest gets the kind `action_graph` (additive: `manifest.v1` stays, the document `version` is 2), three entries with the SHA-256 of `fintech/graph/g1_investigate.json`, `g2_escalate.json` and `g3_complaint.json` (`sha256sum`, as every other kind), the Go loader (`packload.Load` verifies them and returns `Pack.ActionGraphs`; `ErrGraph`) and `tools/packcheck manifest` (the pinned graphs are exactly the pack file's `action_graph_files`).
- Why: A510 (interim, `DEC-2026-11-21-architect-rulings-a507-a517`): no unverified graph is ever served; EL's Planner (A511) reads graphs through the verified pack.
- AGENT-15: the manifest schema is pack data (`packs/schemas/`), not a contract (not in `contracts/`, not in `INVENTORY.md`, no generated model), so no contract bump or codegen applies. The change is additive; a manifest of the three earlier kinds still validates; both readers (loader, packcheck) change in this PR.
- Reading taken (AGENT-20): the pinned set equals the set the pack file names; a graph named and not pinned, pinned and not named, or an unlisted `*.json` in the graph folder is refused; a pack that names none pins none and loads with none.
- Risks, and how they are contained: any edit to a graph now needs its digest updated in the same PR (packcheck fails otherwise; documented in `packs/README.md`). No graph content changed. No consumer (`plangraphs`) was touched.
- Tests added (REQ-OPS-003, REQ-PLN-002; INV-14, INV-21, AGENT-13, AGENT-16): `packload_graphs_test.go`; `tools/packcheck/test_manifest.py` (`ActionGraphs` and the real manifest); `packs/tests/test_manifest_schema.py`; `cmd/core` real-pack test. Seen failing at the red head (run ids in the PR).
- Regression cases added: the tamper tests above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's `plangraphs` production source takes the graph bytes from `packload.Pack.ActionGraphs` (by the path in the pack's `action_graph_files`) and never from disk.
- Open questions (owner): none.
- Files to read first: `services/core/platform/packload/packload.go` (`checkGraphs`), `packs/fintech/manifest.v1.json`.
- Do not touch: the graph files' content (a change needs the digest updated in the same PR).
