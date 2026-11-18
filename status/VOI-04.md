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
