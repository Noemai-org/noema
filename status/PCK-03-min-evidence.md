# PCK-03 · trust-profile.v1 evidence_required minItems 1 (A437)

Authored by C2 (agent). Interim: A437 is recorded in `DEC-2026-11-20-architect-rulings-a358-a453` (docs main), not yet approved by a non-author Founder (AGENTS 2.3).

## Session 2026-11-20 · PCK-03

### Session summary
- What changed: `contracts/schemas/trust-profile.v1.json` `evidence_required` gains `minItems: 1`; one regenerated Pydantic file; the contracts README migration note; an invalid contract fixture and a crafted bad pack. No packcheck function was added: the existing `schema` check (pack.v1 refs trust-profile.v1) refuses the empty list, and the bad pack proves it.
- Why: Architect ruling A437 (a trust profile must have at least one evidence source), the open question of Codex on noema#788.
- Risks, and how they are contained: a tightening could invalidate a pack. Checked: the fintech pack (3 sources) and the three conformance packs (1 each) pass `tools/packcheck validate`; no fixture has an empty list.
- Tests added: the invalid fixture (`minItems`), `schema_04_trust_profile_requires_no_evidence.json` (BadPacks). REQ-TRU-001, INV-23.
- Regression cases added: the two above.
- Debt entries: none.
- Conflicts raised: none. Overlap with noema#765 (`contracts/README.md`, `tools/codegen/shape_refusals.json`): whoever merges second merges `main`, appends its README section, and regenerates `shape_refusals.json` (`tools/codegen/shape_refusals.py --write`).

### Pre-work for the next agent
- Exact next step: Codex then SR read; an independent role review (Tier 1, contract); one Founder approval.
- Open questions (owner): none.
- Files to read first: `contracts/README.md` (last section), `contracts/schemas/trust-profile.v1.json`.
- Do not touch: `services/core/orchestrator/portadapters/trust/profiles.go` (noema#788; its refusal stays as defence in depth).
