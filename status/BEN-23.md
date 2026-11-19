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

# Session 2026-11-19 · BEN-23 · A377 to A379: the DPA, terms, consent and counsel preconditions are removed from the plan

## Session summary
- What changed: `benchmark/runner/PLAN-intron-voice-subset-run.md` only. The A180 bullet is replaced by A377 to A379 (docs#211, interim until a non-author Founder merges it): no DPA, written-terms, consent or counsel precondition; raw audio stays in the bench Environment's private store; synthetic audio stays labelled and this run still uses synthetic audio only (the section 5 check is kept as the label, not as a wait for a term); no spend for a custom voice (also added to precondition 5 of section 6).
- Not changed: the other preconditions of section 6 (the SoT edit adopting A179, the qualified model ids under UND-13, the adapter, Intron's capacity answer, the cost ceiling and the key, the leak scan), the pacing, and the three sessions.
- Risks, and how they are contained: documentation only; the plan cites a ruling that is not yet merged, and says so.
- Tests: none (a plan). Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: when docs#211 merges, drop the word "interim" from the A377 to A379 bullet.
- Do not touch: section 5's check that only audio the run itself generated can be sent.

# Session 2026-11-20 · BEN-23 · Codex's P1 on noema#795: A180 governs until docs#211 merges

## Session summary
- P1 (Codex, at b75bf347): the plan said A377 to A379 replace A180's written-terms precondition, but docs#211 is open, so the rulings are relayed and not committed (AGENTS section 2, item 3). Fixed: the A180 bullet is restored word for word as the governing rule; A377 to A379 are stated as **proposed, not in force**, with what they would change; the "no spend on a custom voice" in precondition 5 of section 6 is conditional on A379 being committed. The sentence in section 5 about a future TEAM_RECORDING label stays (it only applies if a later run adds recordings).
- The run itself is unchanged and synthetic-only, so it is consistent with A180 and with A377 to A379 alike.
- Tests: none (a plan). Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: when docs#211 merges, replace the A180 bullet with the A377 to A379 one (a follow-up edit).

# Session 2026-11-20 (second) · BEN-23 · docs#211 merged: A377 to A379 are the rule

## Session summary
- docs#211 merged on 20 Nov 2026 (the rulings are committed). The plan is brought in line, as Codex's P1 asked for: the A180 bullet and the "proposed, not in force" bullet are replaced by one A377 to A379 bullet stating them as the rule (no DPA, written-terms, consent or counsel precondition; raw audio in the bench Environment's private store; synthetic audio labelled and this run synthetic-only; no spend for a custom voice), and precondition 5 of section 6 is unconditional again. The TEAM_RECORDING sentence of section 5 stays.
- Tests: none (a plan). Regression cases: none. Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: none for this plan; the run waits on the SoT edit adopting A179, the qualified model ids, the adapter, Intron's capacity answer and the cost ceiling and key (section 6).

