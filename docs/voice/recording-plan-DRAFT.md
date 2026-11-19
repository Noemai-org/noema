# Voice subset recording plan (VOI-05) · DRAFT, amended by A377 to A379 (19 Nov 2026)

Date: 2026-11-09, amended 2026-11-19 · Owner: the Founder · Drafted by: Voice Engineer role · Status: **DRAFT.** The Founder decided the open choices on 8 Nov 2026 (decision F3). On 19 Nov 2026 rulings A377 to A379 made Intron's email of 18 Nov the data terms of record for the POC and removed the consent form and the counsel and signature steps; this draft applies them. The F-decisions bind only once the Steward records them (noema#363).

Serves REQ-VOI-004, REQ-VOI-005, REQ-VOI-007; PERD §45.4; OD-08 (DEC-2026-10-17); POC-NFR-07. It defines no requirement. The numbers below are the Founder's, not mine.

**Placement.** This file is a working draft in the code repository. The plan the Founder adopts is a governance document and is filed as a new file in the `docs` repository (PERD §64.2 describes the code repository's `docs/` as as-built); this draft is then closed.

## 1. What is recorded, and why

Two separate collections, never mixed:

| Collection | Task | Purpose | Where it runs |
|---|---|---|---|
| Voice subset | VOI-06 (first half, at least 50 sessions), later the rest | Key-slot accuracy and the voice-versus-text gap (VOI-07, VOI-09, VOI-16) | Quiet room, then simulated noise |
| Field sessions | VOI-12, VOI-13. The G5 field gate, as the Founder decided it on noema#349 and as recorded in DEC-2026-11-10-architect-block-f-a129-a136 §3: **240 sessions, 120 in Pidgin and 30 each in en-NG, Yoruba, Hausa and Igbo**; pooled correctness at least 90% with the lower end of its 95% interval at least 80%; each locale at least 80% (24 of 30), reported per locale; a latency claim passes only if the upper 95% bound of the P95 is at most 2.5 s. This replaces the "at least 100" of PERD §45.4 and REQ-VOI-005 for the G5 field plan only | The real-conditions gate G5 (REQ-VOI-005) | Tecno Spark, Airtel 3G, genuine Lagos noise |

Scripts use **synthetic names and amounts only** (AGENT-25). A speaker never speaks about their own bank account or any real person's details.

Raw audio of a real person's voice is handled as in section 5: only in the bench Environment's private store, never in the repository, a fixture, a CI artifact, a log or a trace.

## 2. Speakers

- **At least 30 distinct speakers, and no speaker above 10% of the sessions.** Balanced by gender and by locale mix across en-NG, pcm, yo, ha and ig.
- **Adults only (proposed; the age band was not decided on 8 Nov, so the Founders confirm).** Anyone under 18 is not recruited.
- **The Founders recruit.** Payment, if any, is a small fixed amount, the same for everyone.
- Each speaker's self-described first language and region are recorded as a free label, held apart from the audio and linked only by an opaque speaker reference.
- Native-speaker testers for the five locales (REQ-VOI-007) are recruited the same way. A tester never reviews a session they spoke in.

## 3. Conditions (speaker mix, accents, noise, devices)

- Voice subset: a quiet room, then each simulated condition of VOI-17, with levels set from the dB-meter readings below.
- Field: roadside and market sound levels with a phone dB meter at each session (§45.4 item 2); device model and OS recorded; Airtel 3G with 4G disabled.
- Server-side timestamps for end of speech and Intent Object admission, with NTP on the logging host (§45.4 item 4).
- **Bystanders (proposed rule; a Founder decision).** A roadside or market session can capture people who are not part of the study. The facilitator tells passers-by nothing is kept of them and does not record a conversation they join. At annotation any recording in which a bystander is identifiable or intelligible is cut to exclude that speech or deleted, and the deletion is logged. A session whose purpose is lost by the cut is marked "unusable by bystander" and counted as a failed session in the denominator, never dropped.

## 4. Provider processing

Intron's email of 18 Nov 2026 is the data terms of record for the POC (A377 to A379): retention is set to zero days, the provider does not train on customer audio or transcripts, and no further agreement is a precondition for any work, including audio of real speakers. Live calls may therefore start as soon as the key arrives (the baseline run is noema#767). Masking cannot apply to audio, so nothing spoken may be personal: the script rule of section 1 is what covers it. No custom voice is bought. Each recording keeps an opaque `speaker_ref` and the script's identifier; the register of who recorded what is held with the audio in the private store and is not an input to any model.

## 5. Raw audio: storage, access, retention, withdrawal

Runtime audio is different and not covered here: at runtime raw audio is held in memory for transcription only and discarded (REQ-VOI-004). This section covers only benchmark and field recordings.

- **Storage.** **Raw audio lives only in the bench Environment's private store** and is deleted per this plan (REQ-VOI-004). It has its own credential; no model-provider credential and no core credential can read it. It is never in the repository, a fixture, a CI artifact, a log, a trace or a PR (AGENT-13). It is outside the product and outside the POC runtime (no Noema service reads or writes it). Where the store is created, and its Environment, are the Integration Lead's and the Founder's; this draft names no bucket and no region.
- **Access.** The two Founders only. The Voice Engineer role works from transcripts and annotations, never from raw audio; no agent session holds the audio.
- **Retention.** Audio is kept for the study only and **deleted 90 days after analysis**. Deletion is logged, without keeping the deleted data. Masked transcripts and annotations may outlive the audio.
- **Withdrawal.** A speaker can ask for their recording to be removed at any time before deletion. A withdrawn recording and its derived copies are **deleted within 7 days**; counts already aggregated remain.
- **What proves nothing is kept that should not be.** A **bucket scan** of the private store: every object key carries a `speaker_ref` that appears in the register, and an object without one is deleted and logged. The scan result is kept with the study records. The count of study objects without a `speaker_ref` is zero.

## 6. Annotation

Each session carries the script's intended key slots (amount, counterparty, time), an annotator's reading, and the locale label. Two annotators; disagreement is resolved by a native-speaker tester and recorded. Annotations contain no personal identifier.

## 7. What this plan does not do

- It does not pass G5; the simulation (VOI-17) never replaces the field gate.
- It does not choose a speech provider or write any vendor format.

## 8. What remains

1. The Steward records the F-decisions (noema#363).
2. The Founders confirm: adults only (section 2), the bystander rule (section 3).
3. The private store and its credential are created and its access list (the two Founders) is recorded. Until then no recording of a real person starts.
