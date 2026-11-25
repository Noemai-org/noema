# VOI-04 (text-to-speech)

State: the digit guard is in review (noema#718); the Intron text-to-speech adapter is in noema#640.

# Session 2026-11-18 · VOI-04

## Session summary
- What changed: `deliver_answer` takes the pack's masking limit and withholds, from every synthesizer, an answer text whose longest digit run (the model service's one definition, `platform.redaction.digit_runs`, A246) exceeds it; the delivery says so with the fixed code `VOI_TTS_UNMASKED_DIGITS` and the person gets the text.
- Why: A181 (TTS speaks only the core's masked text) and A246; the guard is the last line of defence before audio.
- Risks, and how they are contained: a required argument with no default (AGENT-16); the code carries none of the text; no caller outside tests exists yet.
- Tests added (REQ, INV, US or EC IDs): REQ-VOI-002, REQ-SEC-003, INV-02. Mutation check: `>` to `>=` fails 2 tests.
- Regression cases added: none (new behaviour).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: pass the pack's `MaskingLimits` where the voice session calls `deliver_answer`.
- Do not touch: the limit (it is the pack's, A154).

# Session 2026-11-24 · VOI-04 (start of the G2 batch)

## Needs check (AGENTS section 3)
- VOI-04 Needs VOI-01, which `tasks.csv` still shows In progress (the Steward moves status from merged evidence): the SpeechProvider interface, the Intron speech-to-text and text-to-speech adapters (noema#640) and the connect factory (noema#816) are on `main`. The batch note says Voice's VOI-04 to VOI-07 come "after #824" (the runner that produces the samples).

## State
- **OQ-07 is answered** in `DEC-2026-10-17-open-decisions-closure` section 3 (OD-P-20): provider-neutral per-locale routing; where streaming TTS is missing or fails its gate, the verified answer is shown as text; the demo's TTS is English and Pidgin only (clarifications C8, cut item 1); Yoruba, Hausa and Igbo are still measured.
- **What the done_when still lacks is "the test samples":** the Intron text-to-speech output for English and Pidgin. The baseline run generates exactly those (the synthetic fixtures, labelled SYNTHETIC, in the private store), so VOI-04's remaining step needs noema#824 merged, the key, the real host and the Founder's `start` on noema#767. Nothing was invented in its place.
- **Documented, not measured** (Intron's documentation pages, `docs/voice/intron-docs-notes-DRAFT.md`): Pidgin is language `pcm` with accent `pidgin`; English has accents but no Nigerian one; Yoruba, Hausa and Igbo have their own language codes; the documentation names no voices.

## Pre-work for the next agent
- Exact next step: after the baseline run, write the OQ-07 answer with the samples' labels and the measured first-audio times (counts only, no audio in the repository).
