# Status · VOI-07 (Voice Engineer, with C2) · code-switch and numeral tests through the lexicon

Updated 2026-11-11.

## State
- **Not started; blocked on two things.** (1) VOI-06 is not Done (no recorded subset exists, see `status/VOI-06.md`). (2) There is no lexicon to test through yet: UND-05 (owner C2; the plan row says it carries the lexicon, for example "20k" to 2000000 and "₦50,000" to 5000000, `contracts/INVENTORY.md`) has no code on `main`. A test for a component that does not exist would be a mock of it, and a regression case built from no recorded utterance would be invented (AGENT-25, AGENT-02).

## Open
- UND-05's lexicon interface and its owner's schedule (C2).
- The VOI-06 subset.

## Pre-work for the next agent
- Exact next step: once the lexicon exists and the first annotated subset exists, run each annotated transcript through the lexicon, and file each failing utterance as a regression case in the benchmark's generated layer with its annotation (`done_when`: failing utterances filed as regression cases). Numeral and code-switched forms to cover are those the annotated sessions actually contain; none is invented here.
- Files to read first: `status/VOI-06.md`, `contracts/INVENTORY.md` (lexicon rows), the PERD lexicon sections cited by REQ-VOI-003.
- Do not touch: held-out material (INV-22).

## Messages
None.

# Session 2026-11-24 · VOI-07 (A636)

- **A636 answers the lexicon question:** Yoruba, Hausa and Igbo are **measured only**, with no lexicon; the demo is in Pidgin (`pcm`) and Nigerian English (`en-NG`). So VOI-07's tests through the lexicon cover `en-NG` and `pcm`; the recordings of `yo`, `ha` and `ig` are still made and measured (VOI-06, VOI-09, G5 unchanged). noema#947 is closed on this ruling.
- **The vendor host:** `infer.voice.intron.io` is set in the **bench copy** of `allowed_hosts` only; the repository keeps the placeholder `set-by-the-founder.invalid` (the strict URL test forbids the host in any file).
