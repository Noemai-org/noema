# Voice subset recording plan (VOI-05) · DRAFT for the Founder's sign-off

Date: 2026-11-06 · Owner: the Founder (signs) · Drafted by: Voice Engineer role · Status: **DRAFT, not signed, not in force**

Serves REQ-VOI-004, REQ-VOI-005, REQ-VOI-007; PERD §45.4; OD-08 (DEC-2026-10-17); POC-NFR-07. It defines no requirement. Every number below is marked **[PROPOSED]**: nothing here is decided until the Founder signs, and a signed version is a new file, not an edit of this one. Nothing in this draft is legal advice; the consent text needs review by someone qualified under Nigerian data-protection rules before any recording starts.

## 1. What is recorded, and why

Two separate collections, never mixed:

| Collection | Task | Purpose | Where it runs |
|---|---|---|---|
| Voice subset | VOI-06 (first half, at least 50 sessions), later the rest | Measure key-slot accuracy and the voice-versus-text gap (VOI-07, VOI-09, VOI-16) | Quiet room and simulated noise first |
| Field sessions | VOI-12, VOI-13 (at least 100 in total, at least 50% Pidgin) | The real-conditions gate G5 (REQ-VOI-005) | Tecno Spark, Airtel 3G, genuine Lagos noise |

Speakers read or act scripted voice-subset intents. **No real customer, no real account data, no real names** appear in any script: scripts use synthetic names and amounts only (AGENT-25). A speaker never speaks about their own bank account.

## 2. Speakers

- **[PROPOSED]** Balanced by gender, and by locale mix across en-NG, pcm, yo, ha, ig; at least 50% of field sessions in Pidgin (fixed by §45.4 for the field gate).
- **[DECISION NEEDED]** How many distinct speakers, and the age band. Fewer speakers with many sessions overstates accuracy; the recommendation is as many distinct speakers as the budget allows, with no speaker contributing more than a fixed share of sessions (the share is the Founder's number).
- **[DECISION NEEDED]** Who recruits, and whether speakers are paid. Native-speaker testers for the five locales (REQ-VOI-007) are recruited the same way and are also speakers' reviewers, never the same person for the same session.
- Accents: record each speaker's self-described first language and region as a free label, not as a personal identifier. The label is held apart from the audio and linked only by an opaque speaker reference.

## 3. Conditions

- Voice subset: a quiet room, then each simulated condition of VOI-17 played back or mixed in (synthetic noise at stated levels, a low-quality microphone, 3G loss and jitter). The stated levels come from the dB-meter readings below, not from this draft.
- Field: roadside and market sound levels recorded with a phone dB meter at each session (§45.4 item 2); the device model and OS recorded; Airtel 3G with 4G disabled.
- Server-side timestamps for both end-of-speech and Intent Object admission, with NTP on the logging host (§45.4 item 4).

## 4. Consent

Before the first utterance, in the speaker's own language, a person reads or plays and the speaker agrees to:

1. what is recorded (audio, the transcript, the device and noise readings);
2. why (to test and measure the voice channel, nothing else);
3. who can listen (named roles only);
4. how long it is kept and when it is deleted (section 5);
5. that they can stop at any time, and withdraw a recording, and what happens to it then;
6. that no real account or personal information should be spoken.

The consent is recorded as evidence: an opaque `consent_ref`, the consent-text version, the language it was given in, the date, and who obtained it. Audio without a `consent_ref` is not stored (REQ-VOI-004: retained audio objects without a consent reference are zero). The form text itself is a **[DECISION NEEDED]** for legal review; this draft does not write it.

## 5. Raw audio: storage, access, deletion

Runtime audio is different and not covered here: at runtime raw audio is held in memory for transcription and discarded (REQ-VOI-004). This section covers only consented benchmark and field recordings.

- **Storage.** Encrypted at rest, in one restricted location that no model-provider or core credential can read. Audio is never in the repository, a fixture, a log, a trace or a PR (AGENT-13). **[DECISION NEEDED]** which store (an AGENT-14 entry if it is a new service).
- **Access.** A named list of roles; every access logged. The Voice Engineer role works from transcripts and annotations and does not need the audio itself except to resolve a disagreement. No agent session holds the audio.
- **Provider terms.** Audio goes to a speech provider only after that provider's data terms are confirmed (REQ-INT-006, PERD R-18). Until then recorded audio is used with no live provider call, and the matrix cell stays UNVERIFIED.
- **Retention timer.** OD-08 sets no universal period. **[PROPOSED]** retention ends at the close of the gate analysis for the collection (VOI-14 for field sessions) plus a fixed grace period; the grace period is the Founder's number. Deletion covers the audio and every derived copy and is recorded without keeping the deleted data. A transcript with identifiers masked and the annotations may outlive the audio.
- **Withdrawal.** A withdrawn speaker's audio and derived copies are deleted within a fixed number of days **[DECISION NEEDED]**; their already-aggregated counts remain.
- **Record event.** `AUDIO_RETAINED{consent_ref}` only when audio is retained (REQ-VOI-004).

## 6. Annotation

Each session carries the script's intended key slots (amount, counterparty, time), an annotator's reading of the audio, and the locale label. Disagreement between two annotators is resolved by a native-speaker tester and recorded. Annotations contain no personal identifier.

## 7. What this plan does not do

- It does not pass G5, and the simulation (VOI-17) never replaces it.
- It does not choose a speech provider or write any vendor format.
- It does not set the numbers marked **[PROPOSED]** or **[DECISION NEEDED]**.

## 8. For the Founder to decide before signing

1. Distinct speaker count, age band and per-speaker share.
2. Who recruits, and payment.
3. The consent text, after legal review.
4. The storage location, and whether it needs a new service (AGENT-14).
5. The retention grace period and the withdrawal deadline.
6. The named access list.

Recommendation: decide items 3 and 4 first. They gate the first recording, and everything else can be set while the consent text is reviewed.
