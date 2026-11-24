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

# Session 2026-11-24 · VOI-05 (what is left of its done_when)

- **Plan written:** `docs/voice/recording-plan-DRAFT.md` (noema#201, amended to A377 to A379: no consent form, counsel or signature); open for the Founders: adults only, and the bystander rule.
- **Storage path with restricted access and a retention timer:** the private store of noema#819 (`store.sh init|check|purge`, owner-only, never named by a workflow) and `voice/retention.py` (`scan_objects`, `deletions_due`, the durations as arguments). The Founder provisions the encrypted volume and runs `store.sh init`; the retention value (90 days after analysis, 7 days for a removal) goes on the host, never in the repository.
- **The Integrator account's retention is zero days:** only the Founder can set and confirm it in the account (Intron's email says new accounts start at zero retention, configurable); the confirmation is the Founder's, as text with no key shown. Not done.
- **Raw audio is never in the repository:** the leak scans of CI hold; the runner writes only under the private store.
- VOI-05 is therefore **not Done**: the plan is unmerged or in review, the store is not provisioned, and the zero-retention setting is unconfirmed.
