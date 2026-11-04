# execution/planner

The Planner. Created by T1 of DEC-2026-10-20; slice 1 built by PLN-01 (2026-11-03).

**Implements:** REQ-PLN-002 (canonical plan hash), part of REQ-PLN-001. SoT IDs: INV-18, INV-05.

## What it does (slice 1)

- `New(reg, graphVersion, intentNode, specs)` builds a `plan.v1` document from step specs. Every capability must resolve through `registry.Callable`, so an unregistered or prohibited capability cannot be put in a plan (`CAPABILITY_NOT_REGISTERED`, `CAPABILITY_PROHIBITED`). Steps are numbered 0, 1, 2 in order. Params are strings, integers, booleans and null only. `Plan` has unexported fields, so the only non-empty `Plan` comes from `New`.
- `Plan.Hash()` is `plan_hash`: SHA-256 of the RFC 8785 form of the document, as `sha256:<64 hex>`. The zero `Plan` has no hash.
- `HashDocument(reg, doc)` hashes a plan that arrives as a document (a stored row). It checks the registry, the step numbering and the required fields first, so a document naming a prohibited or unregistered capability has no hash.

## What it does not do yet

- **Choosing the steps from the Action Graph** (`plan(intent_node, disposition, classification, clock_status, graph_version)`, REQ-PLN-001) and `PLAN_PRECONDITION_UNMET`. That needs the Action Graph contract (PCK-04, `action-graph.v2`) to settle; it is slice 2.
- **The `PLAN_CREATED` Record event** (Trust writes it) and storing the plan.
- It calls no model and imports neither Trust nor Outcome.

## Tests

```
cd services/core && go test -race -count=1 ./execution/planner
```

`planner_test.go`: the hash equals the hash of the canonical document computed by a second route; none of the 8 prohibited and no unregistered ID can be built or hashed; documents with gaps or missing fields have no hash; params of other types are refused. `property_test.go` (`rapid`): identical inputs give an identical hash whatever the map order; changing any one field (graph version, intent node, node ID, capability, a param, a precondition, a postcondition) changes it; changing a param value changes it; swapping two different steps changes it. Use `-rapid.checks=N` for a deeper run and `-rapid.nofailfile` for mutation runs.
