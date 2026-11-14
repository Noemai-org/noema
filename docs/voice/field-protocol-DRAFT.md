# Field-session protocol and dry-run checklist (VOI-08) · DRAFT

Date: 2026-11-08, updated 2026-11-12 · Owner: the Voice Engineer role is the protocol owner (OQ-08; Founder decision F6 of 8 Nov 2026) · Status: **DRAFT, not signed, not in force**

Serves REQ-VOI-005, REQ-VOI-007, REQ-VOI-004; PERD §45.4 (the protocol of record). This file adds no requirement and changes no number in §45.4. It turns §45.4 into a checklist and lists what is missing. VOI-08 needs PRE-05. The plan version proposed in docs#157 (not merged when this was written) marks PRE-05 Done, but PRE-05's own done_when is "order placed", while VOI-08's text needs devices and SIMs *received*. Receipt is not evidenced anywhere I can read, so no line below is ticked and nothing here is "ready".

## 1. What is fixed by PERD §45.4 (copied, not changed)

1. Device: a Tecno Spark (model and OS recorded) on an Airtel 3G SIM with 4G disabled.
2. Location: genuine Lagos ambient noise; roadside and market sound levels recorded with a phone dB meter.
3. At least 100 sessions across speakers, balanced by gender and locale mix, covering the voice-subset intents, at least 50% in Pidgin.
4. Measurement: time from end of speech (VAD) to Intent Object admitted in core, with NTP-synchronised clocks on the logging host and server-side timestamps for both events.
5. Pass: P95 at most 2.5 s, with the Intent Object correct against the script annotation.
6. Full sessions recorded with consent and stored per REQ-VOI-004.

**Amended for the G5 field plan** by the Founder's decision on noema#349, recorded in DEC-2026-11-10-architect-block-f-a129-a136 §3: item 3 above ("at least 100 sessions") is replaced by **240 measured sessions, 120 in Pidgin and 30 each in en-NG, Yoruba, Hausa and Igbo**; the pooled correctness must be at least 90% with the lower end of its 95% interval at least 80%; each locale must also reach at least 80% (24 of 30), reported per locale; the P95 passes only if the upper 95% bound is at most 2.5 s; the 50% Pidgin rule stays. The text of items 1 to 6 is PERD §45.4 as published and is not edited here.

If the gate fails there is no design-partner demo, and the demo runs in text (SoT §37.5).

## 2. Before any session: the checklist

Each line is tick-with-evidence. A line without evidence is not ticked.

**Devices and network**
- [ ] Tecno Spark received (PRE-05); model, OS version and build recorded.
- [ ] Airtel 3G SIM active, 4G disabled in the device settings; a screenshot of the network settings kept (no SIM number in it).
- [ ] The partner-app overlay build under test is identified by its commit; it sends PCM16 16 kHz (A19).
- [ ] A baseline network reading taken at the site (latency and loss, tool to be chosen); no number is assumed in advance.

**Logging host and clocks**
- [ ] A host that logs both timestamps, synchronised with NTP, with the offset recorded at the start and end of each batch.
- [ ] The end-of-speech event and the Intent Object admission are both stamped server-side (§45.4 item 4). Phone clocks are not used for the measurement.
- [ ] The pipeline stages are timed separately (VOI-16) so a miss can be attributed.

**Noise**
- [ ] A phone dB-meter app chosen and its model recorded; its reading logged at each session location and time. Simulated levels (VOI-17) are set from these readings, not the other way round.
- [ ] Sites chosen: roadside and market at minimum. Site list and permissions: **to be decided by the protocol owner.**

**Script and speakers**
- [ ] The script of voice-subset intents, with synthetic names and amounts only, annotated with the intended key slots (amount, counterparty, time).
- [ ] Speaker list balanced by gender and locale mix, at least half Pidgin overall, each with an opaque speaker reference.
- [ ] Native-speaker testers for en-NG, pcm, yo, ha and ig identified (REQ-VOI-007).

**Consent and storage**
- [ ] The VOI-05 recording plan is signed and the consent text reviewed.
- [ ] Every speaker's consent captured with a `consent_ref` before the first utterance.
- [ ] The restricted store exists and a retention timer is set (VOI-05 section 5).

**Readiness before a measured batch**
- [ ] A simulation report per locale exists and is ready (VOI-17, `ready_for_field`): native review approved, zero actions on a low-confidence key slot.
- [ ] A dry run of 10 sessions (VOI-11) completed and its notes list every problem found and its fix. The dry run is not measured and is not part of the 240.

## 3. A session

1. Check the site, record the dB reading, check the network baseline, check the NTP offset.
2. Obtain consent; record `consent_ref`.
3. The speaker performs the scripted intent on the device. The person running the session does not coach pronunciation.
4. The system's behaviour is recorded as it is: an answer, a clarification, a failover, a fall back to text. Nothing is retried silently, and a failed session is kept and counted, not discarded.
5. After the session: both server timestamps, the Intent Object (or its absence), the annotation comparison, and the dB reading are stored with the session reference.

## 4. Counting and reporting (VOI-14)

- Sessions are counted by server timestamps. A session is in the 240 only if both timestamps exist; the count per locale is kept, because each locale has its own floor.
- P95 is reported with n, the method and a confidence interval; correctness is reported against the annotation. If the sample is smaller than the plan's, the report says so and does not claim the gate.
- Failed, clarified and fallen-back sessions stay in the denominator.

## 5. Decisions of 8 Nov 2026 and what is still open

**Decided by the Founder:** the F5 gate (Intent Object correctness against the script annotation at least 90%, the lower end of its 95% interval at least 80%, at least 30 sessions per locale; the P95 and Pidgin rules of §45.4 stay; the session plan and per-locale floor were then decided on noema#349, see §1); the recording plan (VOI-05, noema#201: at least 30 speakers, none above 10% of sessions, separate encrypted bucket, 90 days after analysis, withdrawal within 7 days, access for the two Founders, consent text after counsel); samples for providers are synthetic audio only until data terms are confirmed (F4); the Founder orders the Tecno Spark and Airtel 3G SIMs (F6); PCM16 16 kHz from the phone (A19, Opus not adopted: F7).

**Protocol owner's proposals (mine to make under F6; the Founders can change them):**

| # | Item | Proposal | Status |
|---|---|---|---|
| 1 | Sites and permissions in Lagos | One roadside and one market site at minimum, each visited at two times of day; the Founders obtain the permissions; the site list is fixed before the dry run | Open: the Founders obtain permissions |
| 2 | dB-meter app and network-baseline tool | One named phone dB-meter app and one network-test app, both recorded by name and version in the first session log; chosen at the first site visit and then frozen | Open: needs the devices |
| 3 | Who runs sessions | Two Founders-appointed facilitators who do not coach pronunciation; testers for each locale are native speakers who did not speak in the session they review | Open: the Founders recruit (F3) |
| 4 | Session count | Decided by the Founder on noema#349 and recorded in DEC-2026-11-10-architect-block-f-a129-a136 §3: 240 measured sessions (120 Pidgin, 30 each in en-NG, Yoruba, Hausa and Igbo); the dry run of 10 is separate | Decided |
| 5 | Devices and SIMs (PRE-05) | The Founder orders now (F6) | Open |
| 6 | VOI-05 signed; consent text reviewed | Counsel first, then the Founder signs | Open |
| 7 | At least one qualified speech provider (DEBT-14, expiry 2026-11-30) | API documents first, then one pinned synthetic-audio sample per provider (F4) | Open |

## 6. Dry-run checklist status (2026-11-12)

Nothing is ticked: a line is ticked only with evidence, and none of this evidence exists in the repository. The last column is what would tick it and who holds it.

| Line of section 2 | Ticked | Evidence needed | Holder |
|---|---|---|---|
| Tecno Spark received; model, OS, build recorded | no | delivery confirmation and the recorded model, OS and build | Founders (F6) |
| Airtel 3G SIM active, 4G disabled | no | screenshot of the network settings, no SIM number | Founders, protocol owner |
| Overlay build identified by commit, PCM16 16 kHz | no | the commit of the partner-app build (A19) | partner-app owner |
| Baseline network reading at the site | no | the reading and the tool's name and version | protocol owner, at the site |
| Logging host, NTP offset at start and end of each batch | no | host named; offsets logged | Platform with the protocol owner |
| Both timestamps stamped server-side | no | a trace showing both events stamped by the server | VOI-16 owner |
| Stages timed separately (VOI-16) | no | the VOI-16 timing output on a sample | VOI-16 owner |
| dB-meter app chosen and logged | no | app name and version, first reading | protocol owner |
| Sites chosen and permitted | no | site list, permissions | Founders (proposal 1) |
| Script annotated with key slots | no | the script, synthetic names and amounts only | protocol owner with testers |
| Speaker list balanced, at least half Pidgin | no | list of opaque speaker refs | Founders (F3) |
| Native-speaker testers identified | no | five names or refs | Founders |
| VOI-05 plan signed, consent text reviewed | no | counsel's approval and the Founder's signature (noema#201, noema#294) | counsel, Founder |
| Consent captured per speaker | no | register passing `voice/study.py` (noema#451) | protocol owner |
| Restricted store and retention timer | no | the bucket listing passing `voice/retention.py` (noema#485) with the recorded durations | Founders |
| Simulation report per locale ready (VOI-17) | no | `ready_for_field` for each of the five locales, native review approved | VOI-17, testers |
| Dry run of 10 sessions (VOI-11), notes with fixes | no | the dry-run notes | protocol owner (VOI-11) |

VOI-08's done_when ("dry-run checklist complete") is therefore **not met**. This file stays DRAFT until the Founders confirm proposals 1 to 4 of section 5 and sign.
