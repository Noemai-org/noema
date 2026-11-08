# Packs

The four packs are written in the SoT 15.3 shapes against the CON-01 pack schema (`contracts/schemas/pack.v1.json`): `fintech_v1.json` and the three conformance packs in `conformance/` (telecom, insurance, healthcare administration; SoT 15.4 cap: at most 3 intent nodes, 1 trust profile, 1 outcome definition, no adapters, no executed capabilities). PCK-01.

All content is SYNTHETIC. Values the SoT or PERD give are used as given. Where a predicate names a field, the field vocabulary is not yet defined: predicates are parsed by `internal/predicate` (T6.1) and type-checked by `packcheck` (PCK-02), and stay unconfirmed until then.

Packs are loaded as data, never imported as code (SoT §51). The files are `fintech_v1.json`, the three conformance packs in `conformance/`, and the eight crafted bad packs in `fixtures/bad_packs/`. Shape tests: `python -m unittest discover -s packs/tests` (run by `make contracts-validate`).

## Action Graph, packcheck and the predicate language (T2)

- The fintech Action Graph is pack data in `fintech/graph/` (G1 investigate, G2 escalate, G3 complaint; PERD section 24). The pack lists the files in `action_graph_files`.
- `python tools/packcheck validate [--conformance] <pack>` runs every check: schema, references, capabilities (REQ-CAP-001), predicates (parse and type-check against `evidence_descriptors` and `predicate_variables`), graphs (REQ-AG-001), routing (REQ-AG-002) and reason-code namespaces. `fixtures/bad_packs/` holds one patch per failing case; each names the check that must fail.
- The predicate grammar and the shared corpus are in `contracts/predicates/`. The Go evaluator (T6.1) must pass the same corpus.

## Status (Session A, 24 Oct 2026)

- **Built:** the four packs, the fintech Action Graph and the `packcheck` checks. #59 (open) adds the `understanding` block and `matcher_ref`.
- **Constraints:** packs are data, loaded never imported; both founders review changes; all content is synthetic.
- **Open:** non-author founder review on #59; OD-P-69 (the format of `counterparty_display`); OD-P-70 (the pack directory is mounted read-only in compose, a default waiting for the Founder's "yes").

## Role-to-capability map (noema#341, ruling A3)

`fintech/role_capabilities.json` lists, for each of the four A1 roles, the capabilities the pack permits it. It is derived from `fintech_v1.json` (`capabilities[].authority.roles_allowed`) by `tools/derive_role_capabilities.py` and checked by `packs/tests/test_role_capabilities.py`; never edit it by hand. Three things to know:
- **Prohibited capabilities are in no role.** The pack names `customer` and `ops` in the `roles_allowed` of the eight prohibited capabilities too, but they have no adapter and no callable path (BD-08, REQ-CAP-002), so the map lists them apart under `prohibited`. Reading `roles_allowed` directly would offer them to every arm.
- `customer` and `ops` hold the same twelve capabilities; they differ in scope (an `ops` token acts within its own tenant, REQ-SEC-006), not in capability. `approver` and `audit` hold none: approvers confirm, `audit` reads the Record.
- The role names are the four of A1. The older pack names (`ops_agent`, `viewer`) are mapped by A37 at the benchmark generator only.

## Policy conditions that name a slot

`fin.pol.default.permit_case_writes` has the condition `subject_resource.status == 'RESOLVED'`. `subject_resource` is the **intent slot** of that name (type Resource, kind `fin.transaction`), and a slot's `status` is `RESOLVED`, `UNRESOLVED`, `AMBIGUOUS` or `ABSENT` (`intent-slot.v1`; a slot is RESOLVED only with a resolved reference, INV-06). So the rule permits a case write only against one transaction the person's reference was resolved to; it does not say the payment problem is resolved. The `transaction` descriptor has no `status` field and needs none for this rule.
