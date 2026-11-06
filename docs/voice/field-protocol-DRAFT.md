# Field-session protocol and dry-run checklist (VOI-08) · DRAFT

Date: 2026-11-06 · Owner: the field protocol owner (OQ-08, DEC-2026-10-17 OD-P-21) · Drafted by: Voice Engineer role · Status: **DRAFT, not signed, not in force**

Serves REQ-VOI-005, REQ-VOI-007, REQ-VOI-004; PERD §45.4 (the protocol of record). This file adds no requirement and changes no number in §45.4. It turns §45.4 into a checklist and lists what is missing. VOI-08 needs PRE-05 (devices and SIMs received), which is not Done, so nothing here is "ready".

## 1. What is fixed by PERD §45.4 (copied, not changed)

1. Device: a Tecno Spark (model and OS recorded) on an Airtel 3G SIM with 4G disabled.
2. Location: genuine Lagos ambient noise; roadside and market sound levels recorded with a phone dB meter.
3. At least 100 sessions across speakers, balanced by gender and locale mix, covering the voice-subset intents, at least 50% in Pidgin.
4. Measurement: time from end of speech (VAD) to Intent Object admitted in core, with NTP-synchronised clocks on the logging host and server-side timestamps for both events.
5. Pass: P95 at most 2.5 s, with the Intent Object correct against the script annotation.
6. Full sessions recorded with consent and stored per REQ-VOI-004.

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
- [ ] A dry run of 10 sessions (VOI-11) completed and its notes list every problem found and its fix. The dry run is not measured and is not part of the 100.

## 3. A session

1. Check the site, record the dB reading, check the network baseline, check the NTP offset.
2. Obtain consent; record `consent_ref`.
3. The speaker performs the scripted intent on the device. The person running the session does not coach pronunciation.
4. The system's behaviour is recorded as it is: an answer, a clarification, a failover, a fall back to text. Nothing is retried silently, and a failed session is kept and counted, not discarded.
5. After the session: both server timestamps, the Intent Object (or its absence), the annotation comparison, and the dB reading are stored with the session reference.

## 4. Counting and reporting (VOI-14)

- Sessions are counted by server timestamps. A session is in the 100 only if both timestamps exist.
- P95 is reported with n, the method and a confidence interval; correctness is reported against the annotation. If the sample is smaller than the plan's, the report says so and does not claim the gate.
- Failed, clarified and fallen-back sessions stay in the denominator.

## 5. Open items (not decided here)

| # | Item | Needs |
|---|---|---|
| 1 | Sites and permissions in Lagos | Protocol owner |
| 2 | dB-meter app and network-baseline tool | Protocol owner |
| 3 | Who runs sessions, and the testers' recruitment | Founders |
| 4 | PRE-05 devices and SIMs | Founders |
| 5 | VOI-05 signed; consent text reviewed | Founder; legal review |
| 6 | At least one qualified speech provider (DEBT-14) | Founders |
