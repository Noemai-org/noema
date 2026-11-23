# Session 2026-11-23 · UND-04 · the pack declares the subject slot of an intent node (A520, DEBT-54 part 1)

## Session summary
- What changed: `contracts/schemas/intent-node.v1.json` gains the optional `subject_slot` (a `SlotName`); `packs/fintech_v1.json` declares `"subject_slot": "subject_resource"` on the investigate node and none on `fin.meta.out_of_scope` (no Resource slot); `packs/fintech/manifest.v1.json` re-pins the pack file's SHA-256 (`sha256sum`); `tools/packcheck/checks.py` gains `check_subject_slot` (the name must be a Resource slot of the same node); the generated Go and Pydantic models are regenerated; `contracts/INVENTORY.md` carries the AGENT-15 migration note.
- Why: A520 (interim, DEBT-54): the subject of an intent is a pack-declared slot of its node, so the core reads the slot name from the pack and never writes it in code (INV-14). This is part 1; part 2 (the epoch from the Evidence reader into `Save`, migration 0037) reads this field.
- Contract decision (AGENT-15): `intent-node.v1.json` IS a contract (`pack.v1.json` embeds it; models are generated). The change is an optional property: no document valid before is invalid now, so the file stays `v1`, with a migration note and regenerated models (`codegen --check` passes).
- Reading taken: a node with no Resource slot declares no subject; the epoch of such an intent stays 1 and its subject reference is NULL (part 2).
- Tests added: `tools/contracts/test_intent_node_subject_slot.py` (the member is optional, a slot name, null/empty/other types refused); `tools/packcheck/test_packcheck.py::SubjectSlot` (the real pack declares it for investigate only and passes; a name that is not a slot of the node, not a Resource slot, or on a node with no slots is refused; a malformed name is refused). Red head: tests only.
- Test defect found and fixed in the green: my first version of `test_a_subject_slot_that_is_not_a_slot_of_its_node_is_refused` looked for a `subject` problem for names the contract's pattern already refuses (code `schema`); the green asserts any problem for those.
- Debt: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: part 2 (migration 0037, `intentstore.Save` with the epoch and subject_ref, the epoch port from the Evidence reader). The Evidence reader's `ReadCases` RECORDS evidence items as it reads, so part 2 needs a non-recording closure query in `evidence/portread` (see the PR for part 2); wire it into `cmd/core` only when the epoch lookup is wanted (not on the G1 path: a nil port keeps epoch 1).
- Files to read first: `tools/packcheck/checks.py` (`check_subject_slot`), `contracts/schemas/intent-node.v1.json`.
- Do not touch: the graph files, `plangraphs`.
