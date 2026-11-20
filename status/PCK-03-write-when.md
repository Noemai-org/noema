# Status · PCK-03 · write_when, params, case_reasons

Updated 2026-11-19. Open: noema#765 (Tier 1: contract, loader).

# Session 2026-11-19 WAT · PCK-03

## Session summary
- What changed (PR links): noema#765. `write_when` is a member of the intent node (A364); `produced_by` dropped (A363) and `may_write` gone; optional node `params` of six kinds on action-graph.v2 and an optional `case_reasons` catalogue on pack.v1 (A369). All interim, relayed, not yet committed.
- Why: the Founder's order of 19 Nov 2026; it replaced the first red (ed6c4342), which put `write_when` on evidence-source.
- Risks, and how they are contained: additive optional members only; closed generated types refuse the withdrawn members; packcheck holds params to the capability's input schema.
- Tests added (REQ-CAP-001, REQ-AG-001, INV-03): `tools/contracts/test_intent_node_write_when.py`, `test_action_graph_params.py`, `tools/packcheck/test_packcheck.py` (WriteWhen, GraphParams), 16 bad packs, contract fixtures, `services/core/execution/registry/packmembers_test.go`.
- Regression cases added: the bad packs above.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: `Writes.NeedsWrite` (noema#746) reads `intent_nodes[].write_when`; the plan-graph adapter (noema#762) reads `params`. The pack loader must expose both.
- Open questions (Founders): the `case_reasons` code names; params of `log_complaint_reference` (g3) are not declared.
- Files to read first: `contracts/README.md` (last section), `tools/packcheck/checks.py` (`check_write_when`, `check_graph_params`).
- Do not touch: `contracts/schemas/action-graph.v1.json` (hash-pinned).

# Session 2026-11-20 WAT · PCK-03 (review fix, A440)

## Session summary
- What changed (PR links): noema#765. A slot param is `{name, slot, kind}` (kind REQUIRED, the slot's declared type) and, for a Resource, `resource_kind` too; schema, fixtures, generated models, g2 graph and packcheck rule (A440, interim, relayed, not yet committed).
- Why: the Founders' ruling A440; the planner refuses a resolved value of another kind.
- Risks, and how they are contained: kind REQUIRED (safer reading, packs are data); `Enum` is not a slot type in intent-node.v1 so the kind enum has five values; `resource_kind` is an added member (the ruling names it for Resource without a place).
- Tests added (REQ-AG-001, INV-02, INV-18): `test_action_graph_params.py`, `test_packcheck.py` (GraphParams), bad packs graph_30 to graph_34, `action-graph.v2` fixtures.
- Regression cases added: the bad packs above.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the plan-graph adapter (noema#762) reads `kind` and `resource_kind` and refuses a resolved slot value of another kind.
- Open questions (Founders): is `resource_kind` on the param the intended place, or should the planner read it from the node slot?
- Files to read first: `contracts/README.md` (last section), `tools/packcheck/checks.py` (`check_graph_params`).
- Do not touch: `contracts/schemas/action-graph.v1.json` (hash-pinned).
