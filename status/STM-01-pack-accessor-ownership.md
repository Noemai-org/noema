# Status · STM-01-pack-accessor-ownership · pack ownership accessor and the ownership map (composition follow-up)

Updated 2026-11-20. Owner: Engineering Lead (lane run by a subagent; unreviewed). REQ-TRU-002, REQ-TRU-003; INV-12, INV-14, INV-16. Rulings: A475 ("EL may add one read-only registry accessor to trustpack.Pack, and tells C1"), the ownership-map-with-a-missing-node-test condition as relayed (interim, relayed, not yet committed). All data is SYNTHETIC.

## State

- **Red pushed:** tests plus compiling stubs: `trustpack.Pack.OwnershipKinds` and `ownershipport.NodesFromKinds`.
- **Green:** written locally, not pushed (EL pushes after reading the red job).

## Findings

- The pack has no "owner queue" per intent node. `routing_allowlist` is a flat list of queue names; `owner_queue` is a parameter of a write, not node data. What the Ownership adapter (noema#766, `Config.Nodes`) needs is node to resource kind, and the pack has all of it: a node's trust profile lists `authority.principal_must_own` (slot names), and the node's slot of that name has `resource_kind`. No data is invented.
- A node with no trust profile has no ownership slot by design (the adapter answers `true`, no read). The missing-node test therefore covers every node whose profile owns a slot.

## Session 2026-11-20 · STM-01-pack-accessor-ownership

- What changed: `trustpack.Pack.OwnershipKinds() (map[string]string, error)` (a copy; refuses with ErrPack an owned slot the node lacks, a non-Resource slot, a slot without a kind, two owned slots of two kinds; ErrConfig for a nil or zero Pack); `ownershipport.NodesFromKinds` builds `Config.Nodes`.
- Why: the composition needs `ownershipport.Config.Nodes` from the verified pack, and a test that fails if a node is missing.
- Risks, and how they are contained: read-only, copies, no load rule changed (a pack loads as before; the accessor refuses). No pack identifier in code (INV-14).
- Tests added (REQ-TRU-002, REQ-TRU-003; INV-14, INV-16): `trustpack/ownership_test.go`, `trust/ownership/fromkinds_test.go`.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: main.go calls `pack.OwnershipKinds()` once, passes it through `ownershipport.NodesFromKinds` as `Config.Nodes`, per tenant.
- Open questions (EL): the pack declares one kind per node; a node owning two slots of two kinds is refused (the adapter has one kind per node).
- Files to read first: `services/core/orchestrator/portadapters/trustpack/ownership.go`, `.../trust/ownership/fromkinds.go`.
- Do not touch: `platform/packload` (noema#773), `trust/**`.
