# Plan: the voice-subset run on Intron, paced over three sessions, synthetic audio only (BEN-23)

**Task:** BEN-23 ("Intron on the voice subset": per-locale key-slot accuracy, latency and cost, paced for Intron's documented rate limits; ruling A182). **Owner:** Benchmark Owner role agent, with the Voice Engineer. **Written:** 16 Nov 2026. **Status: a plan. Nothing here is built, no request is sent to Intron, and no credential is read.**

**Reading of "three sessions" (mine; say if it is wrong).** Three separate, short, Founder-dispatched runs, each inside Intron's documented rate limits, each gated on reading the one before. It is not three recording sessions: no person is recorded (A180).

## 1. What governs, and what is not yet in force

- **A179 to A182** (`DEC-2026-11-13-voice-provider-intron`): Intron Sahara v2.5 is the sole integrated speech provider; **A179 takes effect only with the SoT edit that adopts it (v1.12), and "no one builds against this record" until then.** The record is itself Proposed until a non-author Founder merges it. So **this plan can be written and reviewed now, but nothing is built or run on Intron until the SoT edit is approved.** Until then only synthetic audio goes to any speech provider.
- **A377 to A379 (final, recorded in docs#211, interim until a non-author Founder merges it), which replace A180's wall:** the DPA, written-terms, consent and counsel preconditions are removed from this plan. Raw audio stays in the bench Environment's private store (never in the repository, an artifact, a log or a summary). Synthetic audio stays labelled as synthetic, and **this run still uses synthetic audio only**: the check of section 5 is kept, because the label is what makes a synthetic recording distinguishable from a person's, not because a term is awaited. **No money is spent on a custom voice**: no custom voice is bought, cloned or trained, and a TTS voice is one Intron already offers.
- **A181:** spoken amounts, account identifiers and transaction references are never trusted; a missing confidence counts as low; partial transcripts never set a slot; TTS speaks only the core's masked text. This run measures a transcript. It does not feed the core's slot-setting.
- **A182:** Intron's STT (and, if used, TTS) model IDs are **pinned and qualified (UND-13) before any gate run**; **G5 thresholds do not change**; "a weak result is reported as measured".
- **OD-P-90:** "pace every run to Intron's documented limits". **The rulings give no figure and nobody may invent one (AGENT-18).** The limits are therefore a run **input** that Intron's written answer (PRE-04) must supply. **OD-P-91:** a missing per-token confidence counts as low.
- **noema#630** (Voice asks the Architect): recommendation "no replay in the POC", so `allow_replay=False` in every profile. This run uses one provider and **never replays audio to another**.
- **VOI-18:** the bench profile refuses a speech-to-speech provider; the run uses the modular pipeline only.

## 2. What the run measures, and what it cannot claim

The voice subset today is the **29 generated cases whose channel is `voice`** (all layer GENERATED, one turn each, at most 85 characters, 2,074 characters in total): locales pcm 21, en-NG 3, ha+en-NG 2, ig+en-NG 2, yo+en-NG 1. (Counted from the suite at seed 20261020, generator 1.3.3; the generated suite holds a transcript, not audio.)

| Measure (A182) | Definition |
|---|---|
| **Per-locale key-slot accuracy** | For each case, the key slots in the utterance (amount, a named person, an account or transaction reference where present) are read from the **utterance's own source text**, which is the text the audio was made from: no oracle is read (INV-22). The transcript is normalised through the same lexicon the core uses (VOI-07; the spoken-numeral forms), and a slot matches only on exact equality after that. Reported as k of n with the Wilson interval and, for the failure side, `exact_upper95` (scorecard v3), per locale |
| **Latency** | From the last byte of audio sent to the final transcript, per call, with P50 and P95, and the pacing wait **excluded** (it is Noema's pacing, not Intron's latency) |
| **Cost** | From Intron's reported usage where it reports it; a call whose cost was not reported is settled at its projection (the rule of `benchmark/bakeoff/budget.py`); never silently zero |

**What it cannot claim.** Synthetic audio is not a person's voice. A transcription of synthetic speech tells us the pipeline works and what its floor is; it says **nothing about real accents, noise or microphones, and it is not G5 evidence** (G5 needs the recorded field sessions, VOI-12 to VOI-14). Every figure is labelled **SYNTHETIC**, with the audio source named, and the report says so in its first line. n is at most 29 per variant, so no interval is a safety claim (A40's floor of 300 is a floor for Tier 0 headlines and is not approached here).

## 3. The synthetic audio

| Option | Verdict |
|---|---|
| **A. Intron's own TTS speaks each utterance; Intron's STT transcribes it** | **Proposed, labelled as a round trip.** It adds no dependency (AGENT-14) and sends only synthetic audio. It is optimistic (the same vendor's voice, clean) and it needs Intron TTS to support each locale (VOI-04 / OQ-07 is open: unknown for pcm, yo, ha, ig) |
| **B. A, then degraded by the VOI-17 simulation library** (Lagos noise, accent-like pitch and rate shifts, low-quality microphone, 3G jitter and loss) | **Proposed as the second variant of the same cases**, so each case is tested clean and degraded and the gap is visible. VOI-17 is not built; until it is, B is not run and the report says "clean only" |
| C. A TTS or voice library from another vendor | **Not proposed.** A new dependency or a second third party hearing the text needs an approval (AGENT-14); it is a question for the Architect, not a default |

If TTS does not support a locale, **that locale is reported "no synthetic audio available", not zero and not skipped silently**, and its cases are not counted in n.

## 4. The three sessions

Numbers below are **inputs, not decisions**: `R` is Intron's documented request limit (per minute, and concurrency), supplied by Intron's written answer (PRE-04, OD-P-90); `CEILING` is the cost ceiling the Founder sets for this run (the way the X5 ceiling is on noema#342).

| Session | Purpose | Cases | Calls | Gate to start the next |
|---|---|---|---|---|
| **1. Probe and calibrate** | Prove the plumbing and learn the real limit behaviour before spending the budget. Pins are read and checked. One case per locale group plus one clean control | 6 (pcm, en-NG, ha+en-NG, ig+en-NG, yo+en-NG, and one more pcm control) | 6 TTS, 12 STT (clean and degraded where B exists) | BO and Voice read the session-1 report: pins unchanged, no unexplained refusal, **observed limit behaviour matches the documented `R`**, cost per call known, every locale's TTS availability known |
| **2. The body** | The bulk, at a pace below `R` | 14 | 14 TTS, up to 28 STT | The same reads, and the session-1 pace was not exceeded |
| **3. The rest and the repeat** | The remaining cases, then a repeat of session 1's six to show run-to-run stability | 9 new plus the 6 repeats | 15 TTS, up to 30 STT | none; BO writes the report |

Total: 29 cases once, 6 of them twice; **about 35 TTS and 70 STT calls at most**, plus the pacing waits. The split 6, 14, 9 is a proposal and changes nothing in the method.

**Pacing (a dispatch input, no default in the workflow, like `max_body_bytes`):** `calls_per_minute` must be given by the dispatcher and must not exceed `R`; **my proposal is to run at no more than half of `R`**, which is a safety margin for the run and not a threshold of any gate. A refusal for a rate limit (a 429 or the vendor's equivalent) **stops the session after the documented wait and records the call as UNAVAILABLE**, never retried in a loop and never counted as a correct or incorrect transcript. The session ends early, in a stated state, rather than push through.

**Stops, each ending the session and writing a plain reason:** any pin drift (the vendor reports a model identifier other than the pinned one); the cost guard would pass `CEILING` (it refuses before the call, as `budget.py` does, and once stopped nothing cheaper slips through); a refused credential; a dead log collector (a dead collector makes a session NOT RUN, never clean); any real-audio marker (section 5).

## 4a. How a session is run

A Founder-dispatched workflow in a bench Environment (the same model as the other live runs; my token has no Actions permission), with these inputs: `session` (1 to 3), `calls_per_minute`, `cost_ceiling_micros`, the pinned STT (and TTS) model ids. The key is a file in the bench Environment, never an argument, an environment value or a log line (`read_secret`). The workflow prints counts, statuses, latencies and the digest of the audio set, **never an audio file, a transcript or an amount**. Each session uploads one summary file. The audio itself is generated inside the job, kept in a temporary directory and deleted at the end of the session, so no audio is stored; if the Founder wants it kept for a rerun, that is a separate decision.

## 5. Safeguards for "synthetic only"

1. **The audio set is produced from the generated cases only** (layer GENERATED). A case from any other layer is refused (INV-22), the same rule as `select_subset` in the qualification code.
2. **The sender accepts only audio the run itself generated**: each file carries a provenance entry (case id, generator version and seed, TTS model id, the SHA-256 of the text) and the sender refuses any file without one. A recording of a person has no such entry and cannot be sent by this path. This is a check in the sender, not a promise. **If a later run adds recordings of team members (the Voice baseline, noema#767, accepts them), this check must first learn the same origin label the Voice harness uses (`TEAM_RECORDING` beside `SYNTHETIC`, noema#694) and refuse anything else; this run stays synthetic-only and does not.**
3. **No transcript or amount is written to an artifact.** The leak scan runs over the session summary and the job log, with a planted canary it must find; digit runs over four never appear (A154): amounts are compared as booleans and reported as counts.
4. **No second provider hears any audio** (noema#630; one provider only).

## 6. What must be true before session 1 (none is true today)

1. The SoT edit that adopts A179 is approved and merged (v1.12), and the decision record is merged by a non-author Founder.
2. Intron's STT (and TTS) model ids are **pinned and qualified under UND-13** (the qualification records of noema#199, merged; the run needs a qualified record for the Intron identity, which needs the Intron adapter and a qualification run on synthetic audio first).
3. The Intron adapter exists behind the provider-neutral STT and TTS interfaces (VOI-01, rewritten by the change request), with the 10 to 100 character TTS chunking done in the adapter (A181). All 29 cases are 85 characters or fewer, so the longest is one chunk; a text shorter than 10 characters is checked and refused with a named reason.
4. Intron's written answer on capacity and rate limits (PRE-04; OD-P-90), and the Founder's capacity decision. **Without these `R` has no value and session 1 cannot be dispatched.**
5. The Founder's cost ceiling for the run, with no spend on a custom voice (A379), and the Intron key in the bench Environment (A240 is about Tushe; for Intron the same rule: bench Environment only).
6. The leak scan and its canary applied to the voice workflow; VOI-18 enforced in the bench profile.

## 7. Evidence and review

One package after session 3, in the style of the G1 package: the final head SHA; the three session summaries with links; the pins; per-locale tables (k of n, interval, `exact_upper95` where it applies); latency and cost; the stop reasons of any session that ended early; a section "what was not tested" (real speakers, real noise, locales without TTS, concurrency). The Security Reviewer reads it for A180 (no real audio) and the leak scan; the Voice Engineer for the adapter and the pacing; Codex where A14 applies; one Founder approval. The result is reported as measured whatever it is (A182).

## 8. What I do not know (asked, not guessed)

1. Intron's documented request rate, concurrency and daily limits, and whether there is a per-minute character or audio-second limit. **Owner:** Founder with Intron (PRE-04).
2. Which locales Intron's TTS can speak (pcm, yo, ha, ig). **Owner:** Voice (VOI-04, OQ-07).
3. Whether Intron's STT returns a confidence per token (OD-P-91), and whether it reports usage and cost in the response.
4. Whether the "three sessions" is the Founder's meaning (the note under the title states mine).
5. Whether the clean-only result is acceptable if VOI-17 is not ready, or the run should wait for it.
6. The cost ceiling for the run. **Owner:** Founder.
