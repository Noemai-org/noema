# Status · BEN-23 (Benchmark Owner agent with the Voice Engineer) · Intron on the voice subset

Updated 2026-11-16. Task row: BEN-23 (plan; rewritten by the A179 change request to "Intron on the voice subset: per-locale key-slot accuracy, latency and cost, paced for Intron's documented rate limits", ruling A182). Gate: G5 (voice); G5 thresholds do not change.

# Session 2026-11-16 · BEN-23 · the plan for the voice-subset run, three paced sessions, synthetic audio only

## Session summary
- What changed: `benchmark/runner/PLAN-intron-voice-subset-run.md` (a plan only) and this file.
- Why: the Founder's instruction of 16 Nov 2026: plan the voice-subset run against Intron with pacing for three sessions, synthetic audio only.
- Risks, and how they are contained: documentation only. Nothing is built or sent. A179 takes effect only with the SoT edit (v1.12), so the plan says nothing runs on Intron before it, and it invents no rate limit (OD-P-90: Intron's written answer supplies it). Real audio is made impossible to send by a provenance check in the sender, not by a promise (A180).
- Tests added: none (a plan; the checks it will start with are in its sections 4 and 5). Regression cases added: none. Debt entries: none. Conflicts raised: none (A179's version number is the Steward's open question, not mine).

## Pre-work for the next agent
- Exact next step: when the SoT edit that adopts A179 is approved and Intron's written answer on limits arrives, the Voice Engineer builds the adapter (VOI-01 as rewritten); BO then builds the audio-set generator and the scorer (key slots read from the utterance's own source text, no oracle) with tests first.
- Open questions (owner): the six in section 8 of the plan (limits, locales with TTS, per-token confidence, the meaning of "three sessions", clean-only if VOI-17 is late, the cost ceiling): the Founder, Voice, Intron.
- Files to read first: `benchmark/runner/PLAN-intron-voice-subset-run.md`, `DEC-2026-11-13-voice-provider-intron` in the `docs` repository.
- Do not touch: no Intron key value in any file; no real recording on this path.
