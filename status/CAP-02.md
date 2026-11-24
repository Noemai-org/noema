# Status · CAP-02 (Core Engineer 1) · compensation declared per capability, with the A647 contracts

Updated 2026-11-24. Plan task CAP-02: owner C1+AG; REQ-CAP-004; Needs PCK-02 (Done). Rulings A631, A647, A650 (interim, `DEC-2026-11-24-architect-rulings-a644-a656`).

## State: draft pull request noema#963; red pushed (`412902ae`), green local until the red CI job is read. CAP-02 is not Done until merged evidence.

## Session 2026-11-24 · CAP-02

### Session summary
- What changed (PR links): noema#963. `capability-descriptor.v2.json`; `pack.v1` (`tenant_zone`, `$ref` to v2); `intent-node.v1` (`action_graph`); regenerated Go and Pydantic models and `shape_refusals.json`; the fintech pack and the three conformance packs and the pack fixtures migrated; the fintech manifest re-pinned; packcheck checks `compensation`, `tenant_zone`, `graph_link`; the Go registry (`execution/registry`, V1 to V2, compensation rules); `contracts/INVENTORY.md` and `contracts/README.md`.
- Why: A647 settles the vocabulary conflict of CAP-02 (the PERD enum against the contract's free string and the pack's `none_required` and `not_applicable`).
- Risks, and how they are contained: (1) `pack.v1` changes in place; every pack in the repository is migrated, none outside is known. (2) The pack file's hash changed, so `fintech/manifest.v1.json` is re-pinned: the benchmark pins the pack by that hash, so **the pack hash of any run before this PR is not the pack hash after it** (the Architect/BO to note before G1). (3) `log_complaint_reference` and `record_investigation_evidence` now declare `ESCALATE_ONLY`: the descriptors change, behaviour does not (nothing reads `compensation` at run time yet).
- Tests added (REQ-CAP-004, REQ-DATA-002, BD-08): see contracts/README.md.
- Conflict raised: the recon-rule window member (A647 `window_seconds`, C2's #949 `convergence_window_seconds`, no schema): PR body.

## Pre-work for the next agent
- Exact next step: read the red CI job of noema#963, push the green, answer the recon-window question once the Architect rules.
- Do not touch: `capability-descriptor.v1.json`.
