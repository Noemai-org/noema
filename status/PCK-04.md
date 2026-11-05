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

## Session 2026-11-04 · Codex's reviews on noema#104
- docs#88 is merged (DEC-2026-11-03-action-graph-shape, D1 to D4). Done now, test first: D3 "a root has no incoming edge" (fixture `graph_18_root_with_incoming_edge.json` failed first; the rule in `check_graph_shape` now passes it); main merged in; `make codegen` run for `action-graph.v2` (generated Go and Python types plus the registry; `codegen-check` is clean).
- PCK-04's Need on PCK-02: the Architect's decision of 3 Nov 2026 splits PCK-02: the template-coverage mechanism (merged, noema#149) is what PCK-04 needs; enforcement on the real pack moves to EXP-04. The Steward records that decision; until it is committed Codex's blocking finding on the Need stands.
- Still blocked (not mine to fix): the fintech graph's `lookup_beneficiary` and `lookup_account` nodes have no postcondition because no authoritative evidence descriptors exist. noema#148 proposes minimal ones and Codex says their source mappings are unsupported by the governing worksheet. The four tests that run the real pack (`test_fintech_passes`, `test_a_fixture_never_passes_the_unpatched_pack`, the routing allowlist test and the real-pack template-coverage test) fail for that reason only; they pass the moment the two postconditions exist. The pack is not exempted.

## Session 2026-11-05 ~16:30 WAT · PCK-04 (ruling D6)
- **Postconditions added** (the two read nodes in `g1_investigate`), per the Architect's ruling D6:
  - `lookup_beneficiary`: `exists(beneficiary where beneficiary.transaction == subject_resource)`, from descriptor `beneficiary` on `fin.src.beneficiary` (authoritative for `beneficiary_list_and_aliases`).
  - `lookup_account`: `exists(account where account.owner == principal_id)`, ownership only, from descriptor `account` on `fin.src.customer` (authoritative for `customer_identity_and_ownership`).
  - New predicate variable `principal_id` (Reference), the authenticated principal; it mirrors the capability's `principal_must_own`. The evaluator (T6.1) must bind it from the session, never from the request (INV-12); not yet bound anywhere.
- **No balance or state postcondition:** no source covers account balance or state, and `fin.src.core_ledger` is authoritative only for `debit_posted_or_reversed`. A request to Tushe and a Tier 1 debt row are the Steward's to file; this gap is open, not closed.
- Evidence at `da02a0b` plus this change, local: `make contracts-validate` OK (19 packcheck tests, fintech and three conformance packs), `packs/tests` 24 OK, `make codegen-check` OK, repo lint OK. `platform/db` `TestAMigrationLockIsNeverLeftOnAPooledConnectionWhenTheUnlockFails` failed once in a full parallel run and passed 3 of 3 alone; this branch has no change under `platform/`, so it is main's test and I treat it as a flake to report, not a pass.
- noema#148 must carry the same two descriptors (sources `fin.src.beneficiary` and `fin.src.customer`, fields as in `packs/fintech_v1.json` here) so the two PRs merge without conflict.
