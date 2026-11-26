# orchestrator/portadapters/trustpack

The Trust intake's production adapters over the pack's data (TRU-02 ports; rulings A306, A335 and A346 relayed, interim, not committed; A313 and A319). Package `trustpack`. Built 2026-11-18 by Core Engineer 1; local, not pushed.

**Implements:** REQ-TRU-004, REQ-TRU-005, REQ-TRU-010, REQ-CAP-001, REQ-CAP-002, REQ-AG-001. **SoT IDs:** INV-05, INV-14, INV-16, INV-18, INV-25, BD-08, A255. **Never:** a capability ID, node ID, graph ID or industry term in the code (INV-14, SoT 51); an answer from a missing pack, a zero-value adapter or a failed read (INV-16); an allowed prohibited capability (BD-08).

## What it does

- `LoadPack(root fs.FS, packFile)` reads the pack file and the action graph files it lists (`action_graph_files`, relative to `root`) through the generated contract types, builds the capability registry from the descriptors (`execution/registry`), and refuses the **whole** pack (`ErrPack`) when anything is unsound: an incomplete or repeated descriptor, a monetary write not marked prohibited, a repeated intent node, trust profile or graph, a node naming a profile the pack lacks, a graph with no node or no root, a repeated node, an edge or root that names a node the graph does not have, a graph node whose capability is not registered or is prohibited.
- `LoadVerified(pack []byte, graphs map[string][]byte)` (STM-01, A333, A510; interim, relayed) is `LoadPack` over bytes that `platform/packload` verified against the manifest: it reads no directory, so the graphs served are the verified ones. It refuses (`ErrPack`) a graph the pack names and `graphs` lacks, and a graph in `graphs` the pack does not name; no pack bytes is `ErrConfig`. `Pack.Ref()` is `<pack id>@<pack version>` as the pack file carries them (the composition's `PackVersion`).
- `NewCapabilities(pack, graphID)` implements `intake.Capabilities`:
  - `Requested` (T4): the capability the admitted intent asks for: `AdmittedIntent.RequestedCapability` (A370), filled by the Intents adapter from the stored document and the pack. Unknown: `Registered` false. Prohibited: `Registered` true and `Prohibited` true (a prohibited capability is registered, REQ-CAP-002). Both are refused by the intake at T4.
  - `Check` (T14, A255): the registry's answer and `RoleAllowed`: true only for a registered, not prohibited capability whose descriptor lists the role, exactly (no case folding, no trimming, empty role never). A prohibited capability is never allowed, even for a role its descriptor lists.
  - `Authorized` (T11, T12, T14; A306, A346): for an intent whose capability is the capability of a root node of the graph `graphID` (the escalation graph, configuration), the sorted **WRITE** capabilities of that graph and nothing else (no read, no internal node, no other graph's), through the same function `EscalationSetFromPack` uses, so the two cannot drift. For any other registered capability the A306 rule is kept (**assumed**, see Decision 5): every capability of the nodes that depend on a root node whose capability is the intent's, reads included, over every graph that declares it a root, sorted. Unknown or prohibited capability: `ErrCapability` (the intake ends UNKNOWN). Registered capability that roots no graph, and an intent that requests none: empty set, no error (see Decisions). `NewCapabilities` fails closed: an empty graph ID, a graph the pack does not have and a graph with no WRITE node are `ErrEscalationGraph` and no adapter.
- `NewScope(pack)` implements `intake.Scope` (T5): an intent node is in scope when the pack has it and gives it a trust profile.
- `NewAuthority(pack)` implements `intake.Authority` (T6): the role is in the node's trust profile `roles_allowed`, exactly.
- `EscalationSetFromPack(pack, graphID)` builds `recorder.Writer.Escalation` from the WRITE nodes of the graph the composition root names (A319: the pack has no field naming the escalation graph, DEBT-48; the ID comes from configuration). Missing graph, empty ID, no WRITE node: `ErrEscalationGraph` and the empty set.

## What `cmd/core` must pass (wire_trust.go is EL's lane; not edited here)

```go
pack, err := trustpack.LoadPack(os.DirFS(packsDir), packFile)            // packsDir, packFile: configuration; the core container must mount the packs directory
caps, err := trustpack.NewCapabilities(pack, escalationGraphID) // escalationGraphID: configuration (A346)
scope, err := trustpack.NewScope(pack)
authority, err := trustpack.NewAuthority(pack)
escalation, err := trustpack.EscalationSetFromPack(pack, escalationGraphID) // escalationGraphID: configuration, the same value `NewCapabilities` takes
// ports := intake.Ports{Capabilities: caps, Scope: scope, Authority: authority, ...}; writer.Escalation = escalation
```

There is no second port for the remedy (A370). `intake.AdmittedIntent.RequestedCapability` carries the capability the stored intent asks for; `trust.StoredIntents.Get` fills it (the node's Reference slot whose pack declaration has `references: capability`; an unreadable slot is an error there, never "none"), and `Verify` holds it to the stored one. An empty field means the intent asks for none.

## Decisions made (interim, for the Architect; each has a test)

1. `Authorized`: unknown or prohibited root capability is an **error**, never "none": the intake treats it as UNKNOWN, and T4 has refused it first anyway. A registered capability that roots no graph, or an intent that requests none, is the **empty set**: it authorizes only the intent's own capability, which the intake adds, so it widens nothing and T14 still refuses any step outside it.
2. `Authorized`, for a root that is not a root of the named graph, follows the graph's edges from the root (the nodes that depend on it), not the whole graph, so a graph with two declared roots gives each root its own downstream. For the fintech pack every graph has one root and the set is the whole graph. For a root of the named graph the set is all of that graph's writes, whichever of its roots the intent's capability is (A346 says the graph's writes; the pack has one root there).
3. Scope is the pack's, the same for every tenant: the pack holds no per-tenant list of nodes. A node with no trust profile (the pack's node for a request outside its scope) is out of scope.
4. `Prohibited` capability: `Registered` true. `intake` refuses `!Registered || Prohibited`, so either reading refuses; this one follows REQ-CAP-002 ("registered, rejecting, unreachable").
5. (A346, **assumed**) An intent whose capability does not root the named graph keeps the A306 rule above: the downstream of its own root, reads included, unioned over graphs; empty for a capability that roots no graph; `ErrCapability` for an unknown or prohibited one. A346 names only the escalation graph, so the fintech investigation graph's reads and the complaint graph's write are authorized as before. If the ruling means "only writes, always", one condition changes. A capability that roots the named graph and another graph gets the named graph's writes only, not the union.

## Handoffs (TRU-14, A531, A684)

`Handoffs` implements `intake.Handoffs` over the pack's `handoff_routes`: `Routed(ctx, {tenant, intent node, reason code})` is true when the pack routes that reason to an owner queue for that node (a route with no `intent_nodes` applies to every node). A reason the pack does not list is not routed and is not an error. No tenant is `ErrSession`, no node or no reason is `ErrHandoffQuery`, a node the pack does not have is `ErrIntent`; the zero value and a nil pack are `ErrConfig`. `LoadPack` refuses a pack that routes one reason twice for the same node, or names a node it does not have. `Pack.HandoffRoutes()` is the read-only accessor (a sorted copy). The package names no reason code and no queue (a source scan pins it). The composition sets `Service.Handoffs` from `NewHandoffs`.

## Action graph data (TRU-14, A551, A684)

`Pack.ActionGraphData(graphID)` gives a graph's roots, nodes (kind, capability, preconditions as text) and edges (condition) as a copy, read at load. An empty ID or a graph the pack does not have is `ErrGraph`; a nil pack is `ErrConfig`. This package does not parse or evaluate the predicates; `portadapters/runnableport` hands the data to `portadapters/runnable`.

## Tests

```
cd services/core && go test -race -count=1 -p 2 ./orchestrator/portadapters/trustpack
```

They run on the real `packs/fintech_v1.json` and its graph files, with every expectation read independently by the test (`helpers_test.go`), a synthetic tenant, and changed copies of the pack (`testing/fstest`) for refusals. No mock of the pack. The Remedies port is a spy. 54 hand mutants, all killed (`status/TRU-adapters-capabilities-scope-authority.md`).

## Accessors of the final composition (A550, A551; STM-01)

`Pack.ConfirmationRequirements()` is what the pack's CONFIRM rules ask of the approver, by capability (SELF, MAKER_CHECKER or ROLE:<role>); `Pack.Registry()` is a new capability registry built from the verified bytes on every call. Both read only the bytes the Pack was built from, return copies, name no id of any pack, and refuse the whole answer (`ErrPack`) when the data is not sound; a nil or zero Pack is `ErrConfig`. A pack that failed verification is never a Pack, so it yields none. Tests: `accessors_test.go`.
