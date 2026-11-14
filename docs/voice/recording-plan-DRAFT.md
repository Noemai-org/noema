# Voice subset recording plan (VOI-05) · DRAFT with the Founder's decisions of 8 Nov 2026 applied and the reviewers' findings answered

Date: 2026-11-09 · Owner: the Founder (signs) · Drafted by: Voice Engineer role · Status: **DRAFT. The Founder decided the open choices on 8 Nov 2026 (decision F3). The plan is signed only after counsel approves the consent text. Nothing is recorded before then. The F-decisions bind only once the Steward records them (noema#363); the Founder signs after that record exists and this plan then cites it.**

Serves REQ-VOI-004, REQ-VOI-005, REQ-VOI-007; PERD §45.4; OD-08 (DEC-2026-10-17); POC-NFR-07. It defines no requirement. The numbers below are the Founder's, not mine. Nothing here is legal advice; counsel reviews the consent text and the points in section 4.

**Placement.** This file is a working draft in the code repository. The signed plan is a governance document and is filed as a new signed file in the `docs` repository (PERD §64.2 describes the code repository's `docs/` as as-built); this draft is then closed.

## 1. What is recorded, and why

Two separate collections, never mixed:

| Collection | Task | Purpose | Where it runs |
|---|---|---|---|
| Voice subset | VOI-06 (first half, at least 50 sessions), later the rest | Key-slot accuracy and the voice-versus-text gap (VOI-07, VOI-09, VOI-16) | Quiet room, then simulated noise |
| Field sessions | VOI-12, VOI-13. The G5 field gate, as the Founder decided it on noema#349 and as recorded in DEC-2026-11-10-architect-block-f-a129-a136 §3: **240 sessions, 120 in Pidgin and 30 each in en-NG, Yoruba, Hausa and Igbo**; pooled correctness at least 90% with the lower end of its 95% interval at least 80%; each locale at least 80% (24 of 30), reported per locale; a latency claim passes only if the upper 95% bound of the P95 is at most 2.5 s. This replaces the "at least 100" of PERD §45.4 and REQ-VOI-005 for the G5 field plan only | The real-conditions gate G5 (REQ-VOI-005) | Tecno Spark, Airtel 3G, genuine Lagos noise |

Scripts use **synthetic names and amounts only** (AGENT-25). A speaker never speaks about their own bank account or any real person's details.

**A recording of a real person's voice is personal data**, even though the script is synthetic. The legal review (section 4) covers it.

## 2. Speakers (decided)

- **At least 30 distinct speakers, and no speaker above 10% of the sessions.** Balanced by gender and by locale mix across en-NG, pcm, yo, ha and ig.
- **Adults only (proposed; the age band was not decided on 8 Nov, so the Founders confirm).** Anyone under 18 is not recruited; if the Founders want a guardian rule instead, counsel writes it.
- **The Founders recruit.** Payment, if any, is a small fixed amount, the same for everyone.
- Each speaker's self-described first language and region are recorded as a free label, held apart from the audio and linked only by an opaque speaker reference.
- Native-speaker testers for the five locales (REQ-VOI-007) are recruited the same way. A tester never reviews a session they spoke in.

## 3. Conditions

- Voice subset: a quiet room, then each simulated condition of VOI-17, with levels set from the dB-meter readings below.
- Field: roadside and market sound levels with a phone dB meter at each session (§45.4 item 2); device model and OS recorded; Airtel 3G with 4G disabled.
- Server-side timestamps for end of speech and Intent Object admission, with NTP on the logging host (§45.4 item 4).
- **Bystanders (proposed rule; a Founder decision).** A roadside or market session can capture people who did not consent. The facilitator tells passers-by nothing is kept of them and does not record a conversation they join. At annotation any recording in which a bystander is identifiable or intelligible is cut to exclude that speech or deleted, and the deletion is logged. A session whose purpose is lost by the cut is marked "unusable by bystander" and counted as a failed session in the denominator, never dropped. Counsel confirms the rule.

## 4. Consent and legal review (decided: counsel first)

**Legal review of the consent text comes first. Nothing is recorded before the text is approved by counsel and the plan is signed.** Counsel's list is: the consent wording in each of the five locales; the lawful basis for processing a voice recording; where the data is stored (the **region of the AWS bucket is to be named by the Founders** before signing, and counsel confirms it is acceptable for speakers in Nigeria); the age rule in section 2; the bystander rule in section 3; and whether a speaker's audio may be sent to a speech provider (below).

Before the first utterance, in the speaker's own language, a person reads or plays and the speaker agrees to:

1. what is recorded (audio, transcript, device and noise readings);
2. why (to test and measure the voice channel, nothing else);
3. who can listen (the two Founders only);
4. **whether and when the audio is sent to a speech provider for transcription, which provider processes it, and that field sessions with a real provider start only after that provider's data terms are confirmed;**
5. how long it is kept and when it is deleted (section 5);
6. that they can stop at any time and withdraw a recording, and what happens then;
7. that no real account or personal information should be spoken.

The consent is recorded as evidence: an opaque `consent_ref`, the consent-text version, the language it was given in, the date, and who obtained it. Audio without a `consent_ref` is never stored. The consent wording itself is counsel's, not drafted here.

**Provider processing (decided F4, extended here for the Founders to confirm).** The Founder sends the API documents first; then one pinned sample per provider is approved, **synthetic audio only**, once the data terms are confirmed (REQ-INT-006 for the version pinning; the data-terms sources are OD-04, OD-05 and R-18). No live calls with partner data. **Open Founder decision:** may contributor audio reach a provider before the OD-08 partner retention schedule exists? Recommendation: no. Field sessions that need a live provider wait until at least one provider's `retention_terms` is confirmed in the capability matrix; until then only synthetic samples are sent. Masking cannot apply to audio, so the consent line above is what covers it.

## 5. Raw audio: storage, access, retention, withdrawal (decided)

Runtime audio is different and not covered here: at runtime raw audio is held in memory for transcription only and discarded (REQ-VOI-004). This section covers only consented benchmark and field recordings.

- **Storage.** One separate encrypted bucket in the Founder's AWS account (changed from the self-hosted store on 8 Nov 2026), **without Object Lock, so deletion works**. It has its own credential, **readable by the two Founders only**; no model-provider credential and no core credential can read it. The bucket is outside the product and outside the POC runtime (no Noema service reads or writes it). It is a datastore, so the AGENT-14 note is the entry the Steward records with the AWS anchor decision (noema#366); no new service is added. It is a different bucket from the anchor store and must not be created in the Compliance-locked anchor account or use the `anchor-store` Environment or either anchor key (docs#111). Audio is never in the repository, a fixture, a log, a trace or a PR (AGENT-13).
- **Access.** The two Founders only. The Voice Engineer role works from transcripts and annotations, never from raw audio; no agent session holds the audio.
- **Retention.** Audio is kept for the study only and **deleted 90 days after analysis**. Deletion is logged, without keeping the deleted data. Masked transcripts and annotations may outlive the audio.
- **Withdrawal.** A speaker can withdraw at any time before deletion. A withdrawn recording and its derived copies are **deleted within 7 days**; counts already aggregated remain.
- **What proves "no audio without consent".** REQ-VOI-004's `AUDIO_RETAINED{consent_ref}` event covers runtime audio kept after a dispute. For the study bucket the evidence is a **consent register** (one row per `consent_ref`) and a **bucket scan**: every object key carries a `consent_ref` that appears in the register, and an object without one is deleted and logged. The scan result is kept with the study records. The count of study objects without a consent reference is zero.

## 6. Annotation

Each session carries the script's intended key slots (amount, counterparty, time), an annotator's reading, and the locale label. Two annotators; disagreement is resolved by a native-speaker tester and recorded. Annotations contain no personal identifier.

## 7. What this plan does not do

- It does not pass G5; the simulation (VOI-17) never replaces the field gate.
- It does not choose a speech provider or write any vendor format.
- It does not draft the consent wording or give legal advice.

## 8. What remains before signing

1. The Steward records the F-decisions (noema#363); this plan then cites that record.
2. noema#349 is decided and recorded (DEC-2026-11-10-architect-block-f-a129-a136 §3: 240 sessions, pooled gate, per-locale floor, latency on the upper bound); §1 is aligned to it.
3. The Founders confirm: adults only (section 2), the bystander rule (section 3), the bucket region (section 4), and the provider-audio rule (section 4).
4. Counsel approves the consent text and the points in section 4.
5. The Founder signs the plan as a new signed file in the `docs` repository.
6. The bucket and its credential are created and the access list (the two Founders) is recorded. Until then no recording starts.
