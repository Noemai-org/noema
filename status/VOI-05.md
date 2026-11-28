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

# Session 2026-11-24 · VOI-05 (the Founder's confirmations of 24 Nov)

- **Confirmed by the Founder in this session (counts once this pull request is merged by a non-author Founder, AGENTS section 2, item 3):** adults only and the bystander rule of `docs/voice/recording-plan-DRAFT.md`; that the Integrator account's retention is zero days (the Founder's statement; no screenshot or key is stored in the repository: the Founder may add a one-line comment on noema#294 naming the setting and the date); that Voice starts the preparation work of VOI-04 to VOI-07 before their Needs are Done, with no recording and no Done claim.
- **Still not done:** the private store is not evidenced as provisioned (`store.sh init` and `check` on the bench host, by a Founder), the devices and SIMs (PRE-05) are not evidenced as received, and `tasks.csv` still shows VOI-01 and VOI-05 In progress (the Steward moves status from merged evidence).
- VOI-05 is **not Done** until the store and the retention evidence exist.

# Session 2026-11-26 · VOI-05 (the recording devices; A716)

- **Confirmed by the Founder (26 Nov 2026), recorded here as ruling A716 directs:** the recording devices are confirmed. This is the Founder's statement as relayed in A716 (`DEC-2026-11-26-architect-rulings-a704-a720`); no device list, serial number or SIM detail is stored in the repository. It will not be asked again.
- **Still not done:** the private store has no evidence of `store.sh init` and `store.sh check` on the bench host (a Founder runs them; VOI-06 starts after it), and VOI-05 stays **not Done** until the store evidence exists. `tasks.csv` is the Steward's.
- Nothing in this change records audio, names a device or a person, or touches the store.
