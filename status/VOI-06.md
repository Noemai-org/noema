# Status · VOI-06 (Voice Engineer) · record the voice subset, first half

Updated 2026-11-11.

## State
- **Recording has not started and cannot.** VOI-06 needs VOI-05 (the recording plan signed by the Founder, with counsel's approval of the consent text and the restricted bucket created). The plan is `docs/voice/recording-plan-DRAFT.md` (noema#201, aligned on 11 Nov to the decided G5 gate; not signed). Nothing is recorded before it is signed (plan section 8, F3). I have not recorded, requested or stored any audio.
- noema#451 (this status file's PR): `voice/study.py`, the checks that run on the records once recordings exist. It holds records only, never audio (AGENT-13): the consent register, the annotations (two distinct annotators, a locale of the five, the three key slots, a native-speaker tester's resolution of any disagreement) and the subset report (at least 50 sessions, no session with a problem, failed and bystander-unusable sessions stay in the denominator, a withdrawn consent leaves no session behind). Seven mutations of it were each caught by a test.

## Open
- VOI-05 signature, counsel's approval, the bucket and its credential (Founders). The register and the annotations are produced by the Founders' recording process; the Voice Engineer works from transcripts and annotations only.
- Which file format carries a session's annotation is not decided; `study.py` takes the records as Python values and reads no file. The first batch of real records will show the format.

## Pre-work for the next agent
- Exact next step: when VOI-05 is signed and the first sessions are annotated, load the register and the annotations, run `subset_report(sessions, register, min_sessions=50)`, and attach its output to the VOI-06 evidence. Do not weaken `min_sessions`.
- Files to read first: `docs/voice/recording-plan-DRAFT.md` (sections 5 and 6), `services/model/noema_model/voice/study.py`.
- Do not touch: raw audio, the study bucket, the consent register's contents.

## Messages
None sent or received about this task other than the Architect's instruction of 11 Nov to start VOI-06 and VOI-07 (data, not a decision record; used for: this change and the blocker report).
