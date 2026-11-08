# Voice subset recording plan (VOI-05) · DRAFT with the Founder's decisions of 8 Nov 2026 applied

Date: 2026-11-08 · Owner: the Founder (signs) · Drafted by: Voice Engineer role · Status: **DRAFT. The Founder decided the open choices on 8 Nov 2026 (decision F3). The plan is signed only after counsel approves the consent text. Nothing is recorded before then.**

Serves REQ-VOI-004, REQ-VOI-005, REQ-VOI-007; PERD §45.4; OD-08 (DEC-2026-10-17); POC-NFR-07. It defines no requirement. The numbers below are the Founder's, not mine. Nothing here is legal advice; counsel reviews the consent text.

## 1. What is recorded, and why

Two separate collections, never mixed:

| Collection | Task | Purpose | Where it runs |
|---|---|---|---|
| Voice subset | VOI-06 (first half, at least 50 sessions), later the rest | Key-slot accuracy and the voice-versus-text gap (VOI-07, VOI-09, VOI-16) | Quiet room, then simulated noise |
| Field sessions | VOI-12, VOI-13 (the F5 gate: at least 30 sessions in each required locale, and at least 100 in all with at least 50% Pidgin) | The real-conditions gate G5 (REQ-VOI-005) | Tecno Spark, Airtel 3G, genuine Lagos noise |

Scripts use **synthetic names and amounts only** (AGENT-25). A speaker never speaks about their own bank account or any real person's details.

## 2. Speakers (decided)

- **At least 30 distinct speakers, and no speaker above 10% of the sessions.** Balanced by gender and by locale mix across en-NG, pcm, yo, ha and ig.
- **The Founders recruit.** Payment, if any, is a small fixed amount, the same for everyone.
- Each speaker's self-described first language and region are recorded as a free label, held apart from the audio and linked only by an opaque speaker reference.
- Native-speaker testers for the five locales (REQ-VOI-007) are recruited the same way. A tester never reviews a session they spoke in.

## 3. Conditions

- Voice subset: a quiet room, then each simulated condition of VOI-17, with levels set from the dB-meter readings below.
- Field: roadside and market sound levels with a phone dB meter at each session (§45.4 item 2); device model and OS recorded; Airtel 3G with 4G disabled.
- Server-side timestamps for end of speech and Intent Object admission, with NTP on the logging host (§45.4 item 4).

## 4. Consent (decided: counsel first)

**Legal review of the consent text comes first. Nothing is recorded before the text is approved by counsel and the plan is signed.** Before the first utterance, in the speaker's own language, a person reads or plays and the speaker agrees to:

1. what is recorded (audio, transcript, device and noise readings);
2. why (to test and measure the voice channel, nothing else);
3. who can listen (the two Founders only);
4. how long it is kept and when it is deleted (section 5);
5. that they can stop at any time and withdraw a recording, and what happens then;
6. that no real account or personal information should be spoken.

The consent is recorded as evidence: an opaque `consent_ref`, the consent-text version, the language it was given in, the date, and who obtained it. Audio without a `consent_ref` is never stored (REQ-VOI-004: zero retained audio objects without a consent reference). The consent wording itself is counsel's, not drafted here.

## 5. Raw audio: storage, access, retention, withdrawal (decided)

Runtime audio is different and not covered here: at runtime raw audio is held in memory for transcription only and discarded (REQ-VOI-004). This section covers only consented benchmark and field recordings.

- **Storage.** One separate encrypted bucket in the approved self-hosted S3-compatible store (the anchor-store decision), **without Object Lock, so deletion works**. It has its own credential. No model-provider credential and no core credential can read it. This is not a new service, so no new AGENT-14 entry is needed beyond a note. Audio is never in the repository, a fixture, a log, a trace or a PR (AGENT-13).
- **Access.** The two Founders only. The Voice Engineer role works from transcripts and annotations, never from raw audio; no agent session holds the audio.
- **Provider terms.** Audio goes to a speech provider only after that provider's data terms are confirmed (REQ-INT-006, PERD R-18). The first provider samples are synthetic audio only (F4).
- **Retention.** Audio is kept for the study only and **deleted 90 days after analysis**. Deletion is logged, without keeping the deleted data. Masked transcripts and annotations may outlive the audio.
- **Withdrawal.** A speaker can withdraw at any time before deletion. A withdrawn recording and its derived copies are **deleted within 7 days**; counts already aggregated remain.
- **Record event.** `AUDIO_RETAINED{consent_ref}` only when audio is retained (REQ-VOI-004).

## 6. Annotation

Each session carries the script's intended key slots (amount, counterparty, time), an annotator's reading, and the locale label. Two annotators; disagreement is resolved by a native-speaker tester and recorded. Annotations contain no personal identifier.

## 7. What this plan does not do

- It does not pass G5; the simulation (VOI-17) never replaces the field gate.
- It does not choose a speech provider or write any vendor format.
- It does not draft the consent wording.

## 8. What remains before signing

1. Counsel approves the consent text (the Founders).
2. The Founder signs this plan, as a new signed file; this draft is then closed.
3. The bucket and its credential are created and the access list (the two Founders) is recorded. Until then no recording starts.
