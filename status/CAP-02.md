# Status · CAP-02 (Core Engineer 1) · compensation declared per capability, with the A647 contracts

Updated 2026-11-25. Plan task CAP-02: owner C1+AG; REQ-CAP-004; Needs PCK-02 (Done). Rulings A631, A647, A650 (interim, `DEC-2026-11-24-architect-rulings-a644-a656`).

## State: pull request noema#963; the recon-rule schema round (A659) red pushed (`d708ad42`), green below. CAP-02 is not Done until merged evidence.

## Session 2026-11-24 · CAP-02

### Session summary
- What changed (PR links): noema#963. `capability-descriptor.v2.json`; `pack.v1` (`tenant_zone`, `$ref` to v2); `intent-node.v1` (`action_graph`); regenerated Go and Pydantic models and `shape_refusals.json`; the fintech pack and the three conformance packs and the pack fixtures migrated; the fintech manifest re-pinned; packcheck checks `compensation`, `tenant_zone`, `graph_link`; the Go registry (`execution/registry`, V1 to V2, compensation rules); `contracts/INVENTORY.md` and `contracts/README.md`.
- Why: A647 settles the vocabulary conflict of CAP-02 (the PERD enum against the contract's free string and the pack's `none_required` and `not_applicable`).
- Risks, and how they are contained: (1) `pack.v1` changes in place; every pack in the repository is migrated, none outside is known. (2) The pack file's hash changed, so `fintech/manifest.v1.json` is re-pinned: the benchmark pins the pack by that hash, so **the pack hash of any run before this PR is not the pack hash after it** (the Architect/BO to note before G1). (3) `log_complaint_reference` and `record_investigation_evidence` now declare `ESCALATE_ONLY`: the descriptors change, behaviour does not (nothing reads `compensation` at run time yet).
- Tests added (REQ-CAP-004, REQ-DATA-002, BD-08): see contracts/README.md.
- Conflict raised: none open. The recon-rule window name was A647 `window_seconds` against #949's `convergence_window_seconds`; A659 settles it as `convergence_window_seconds`.

## Pre-work for the next agent
- Exact next step: read the red CI job of noema#963, push the green, answer the recon-window question once the Architect rules.
- Do not touch: `capability-descriptor.v1.json`.

## Session 2026-11-25 · CAP-02, the recon-rule schema and the write_when link

### Session summary
- What changed (PR links): noema#963. `recon-rule-set.v1.json` (new; the shape of the pack's rules file; `convergence_window_seconds` required from rule-set version 2 and not allowed in version 1), its fixtures, regenerated Go and Pydantic models, `INVENTORY.md`, `README.md`; packcheck `check_recon_rules` (run by the manifest check on the listed rules file) and `check_write_when_link` (a node whose action graph writes carries a `write_when`, SR's request on noema#935).
- Why: A659 (a `recon-rule-set.v1.json` schema in this PR that requires the window; EL's PCK-05 aligns the data), A664 (this PR lands last of the pack PRs and only re-pins).
- Risks, and how they are contained: (1) The schema is conditional on `version`, so the fintech rules file (version 1, no window) validates unchanged and `main`'s loader, which refuses an unknown member, still reads it. A version 2 file with windows loads only once #949 is on `main`. (2) The window-equals-smaller-freshness-limit check (A641, OD-P-107) is NOT built: it needs the entity-to-source link of the evidence mapping. (3) No threshold is set here: no window value is in any pack file (AGENT-16); the fixture value is a test value.
- Tests added (REQ-RES-005, REQ-CAP-001): `tools/contracts/test_recon_rule_set.py`, `ReconRuleSet` and `WriteWhenLink` in `test_packcheck.py`, `ReconRulesFile` in `test_manifest.py`, fixtures `{valid,invalid}/recon-rule-set`.
- Regression cases added: none (contracts and pack data; no benchmark case applies).
- Debt entries: none new.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: after #910, #964 and #965 merge, merge `main` and re-pin the manifest (`sha256sum packs/fintech_v1.json`); read CI; ask SR; both Founders once.
- Open questions (owner): who writes the equality check of A641 once PCK-05's rules v2 data exists (Architect: C1 as packcheck owner, or EL with PCK-05).
- Do not touch: `capability-descriptor.v1.json`; `packs/fintech/recon_rules/rules.v1.json` (PCK-05's).
