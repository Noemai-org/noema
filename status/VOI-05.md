# Status · VOI-05 (Voice Engineer) · voice subset recording plan

Updated 2026-11-12.

## State
- The plan is `docs/voice/recording-plan-DRAFT.md` (noema#201): aligned on 11 Nov to the decided G5 field gate (docs `DEC-2026-11-10-architect-block-f-a129-a136` §3), not signed. Counsel has not approved the consent text and the Founder has not signed; nothing is recorded before then.
- noema#485: `voice/retention.py`, the checks behind "storage path has restricted access and a retention timer": `scan_objects` (every study object carries a consent ref that is in the register; a withdrawn consent leaves no object; the count without a consent ref is reported and must be zero) and `deletions_due` (audio is due a set time after analysis and a set time after a withdrawal; the earlier applies; times are timezone-aware; overdue is reported). It holds no audio and reads no bucket; it deletes nothing.

## Open
- The two durations are the Founders' decisions (the plan says 90 days after analysis and 7 days after a withdrawal, from decision F3 relayed on noema#294) and are **not yet in a committed decision record**, so the module has no default for either: the caller passes them. The Steward should record them (AGENTS section 2, item 3).
- Counsel's approval, the Founder's signature, the bucket and its credential (Founders; noema#294).
- The format of the listing the caller gives `scan_objects` is not decided; the module takes `StudyObject` values and reads no file.

## Pre-work for the next agent
- Exact next step: after the plan is signed and the bucket exists, a Founder lists the bucket into `StudyObject` records, runs `scan_objects` and `deletions_due` with the recorded durations, and attaches the result to the VOI-05 evidence. Do not give the durations a default.
- Files to read first: `docs/voice/recording-plan-DRAFT.md` (section 5), `services/model/noema_model/voice/retention.py`.
- Do not touch: the bucket, the register's contents, raw audio.

## Messages
Architect's instruction of 12 Nov to take the next two unblocked tasks (data; used for: this PR and `status/VOI-08.md`).
