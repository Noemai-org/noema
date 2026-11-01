# Status · PCK-04 · Action Graph verification

Updated 2026-11-01. Owner: EL (task owner per tasks.csv: EL+AG). Executed by the Engineering Lead role agent.

## State
- In progress. Draft PR on branch `claude/new-session-s2li0v` (the branch the session environment requires; AGENTS.md §11 names `feat/PCK-04-...`, and the Architect allowed the designated branch on 1 Nov 2026).
- `packcheck` fails the fintech pack for exactly two nodes, by design, until E2 lands.

## Open
- **E2, revised by the Architect on 1 Nov 2026 (Core Engineer 2):** the signed Tushe worksheet does not exist, so the `beneficiary` and `account` descriptors use only the envelope fields every existing evidence descriptor already carries, marked "extend when the worksheet is signed". That PR merges before this one; then `main` is merged in here and one postcondition each is added to `lookup_beneficiary` and `lookup_account`. The packcheck rule is unchanged, and the pack is not exempted.
- **REQ-AG-003 (Core Engineer 1):** the planner (`services/core/execution/planner/`) is an empty package, so "the planner selects its path through the graph" cannot be shown yet.
- PCK-02 is a Needs task and is `Unassessed` (OPS-09).
- Codegen of the v2 model when CON-03 lands.

# Session 2026-11-01 14:52 WAT · PCK-04

## Session summary
- What changed (PR links): draft PR from `claude/new-session-s2li0v` (link in the PR list; not merged). Commits `f78ff6e` (failing tests first) and `e219e6f` (contract v2, packcheck, graph roots, READMEs).
- Why: DEC-2026-10-31 D9; PERD v1.19 REQ-AG-001 as amended; Architect decisions of 1 Nov 2026 (E1 option a with a connectedness rule; E2 option a; no exemption for the fintech pack).
- Findings (verification of the fintech graph):
  - It is a real graph, not a flat list. `g1_investigate.json`: 7 nodes, 9 edges (fan-out from `lookup_transaction`, fan-in at `reconcile_transaction_state`, then `evaluate_sla_status`). `g2_escalate.json`: a chain of 3 writes, each with preconditions, a postcondition, `recovery_ref` and `escalation_ref`. `g3_complaint.json`: 1 write node. No rebuild was needed.
  - `lookup_beneficiary` (`g1_investigate.json:46`, empty postconditions at `:50`) and `lookup_account` (`:57`, `:61`) have no postcondition, and no evidence descriptor exists for either entity (`packs/fintech_v1.json`, `evidence_descriptors`).
  - Every edge `condition` is `null` (allowed). `on_unknown` and `on_failure` were already required by the v1 schema.
  - The contract had no way to declare a root; v2 adds `roots`.
- Risks, and how they are contained: CI is red on the packcheck job until E2, which is the honest state of the pack. The contract change follows AGENT-15 (new major, v1 byte-pinned, migration note). Only packcheck loads graph files (checked by search), so nothing else breaks.
- Tests added (REQ, INV, US or EC IDs): REQ-AG-001: bad packs `graph_13` to `graph_17`; `GraphShape` (v2 loading, a seeded connectedness property against an independent union-find over 300 graphs, the single-node case); `tools/contracts/test_action_graph_v2.py` (v1 pinned, v2 is v1 plus `roots`, a graph is v1 or v2). Seen failing first: 13 run, 7 failures before the change.
- Regression cases added: the five bad packs above.
- Debt entries: none.
- Conflicts raised: none. One rule goes beyond the PERD text: connectedness, approved by the Architect on 1 Nov 2026. It should be written into the next PERD version's REQ-AG-001.

## Pre-work for the next agent
- Exact next step: when E2 lands, add one postcondition per node over the new descriptors, run `make contracts-validate`, and expect green.
- Open questions (owner): whether a declared root may also have an incoming edge (not stated by REQ-AG-001; not checked; EL to raise with the Architect).
- Files to read first: `tools/packcheck/checks.py` (`check_graph_shape`), `contracts/schemas/action-graph.v2.json`, `packs/fintech/graph/README.md`.
- Do not touch: `noema#94`, `noema#90`.
