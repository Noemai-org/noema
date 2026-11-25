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

# Session 2026-11-24 · VOI-07 (start of the G2 batch)

## Needs check
- VOI-07 Needs VOI-06 (not Done: no recordings). The lexicon it tests through now exists on `main` (`understanding/compiler/lexicon.py`; the pack's `fin.lex.en_ng.json` and `fin.lex.pcm.json`). **There is no Yoruba, Hausa or Igbo lexicon file in the pack**, so for those three locales there is nothing to test through; whether the pack gets them or the three stay measured only is for C2 and the Architect.

## State
- Nothing to run yet: no recorded utterance exists, and a regression case written without one would be invented (AGENT-25).
- Ready when recordings exist: `study.AnnotatedSession` carries each utterance's annotation; the step is to run each annotated transcript through the lexicon and file each failing utterance as a regression case with its annotation (`done_when`).

## Pre-work for the next agent
- Exact next step: after VOI-06, write the runner that reads annotated sessions, applies the lexicon, and lists the failing utterances (tests first, on synthetic labelled records until real ones exist). Ask C2 about the three locales without a lexicon.
