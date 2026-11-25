# Status · VOI-06 (Voice Engineer) · record the voice subset, first half

Updated 2026-11-11.

## State (updated 2026-11-24)
- **Needs check (AGENTS section 3):** VOI-06 Needs VOI-05, which is not Done in `tasks.csv` (the plan, the store and the zero-retention setting are in review or with the Founder). The devices and SIMs (PRE-05) are not evidenced as received. So no recording starts. What can start without them is done here: the records the recordings will be checked with are brought in line with A379 and plan v1.26 (A449): `voice/study.py`, `voice/retention.py` and `voice/field.py` now key every session and object by the opaque `speaker_ref` the private store uses (`<store>/<speaker_ref>/`), not by a consent reference; the rules are the same (a session or object whose speaker is not in the register, or is withdrawn, is a problem or a violation).
- **Earlier state (11 Nov, before A379):** **Recording has not started and cannot.** VOI-06 needs VOI-05 (the recording plan signed by the Founder, with counsel's approval of the consent text and the restricted bucket created). The plan is `docs/voice/recording-plan-DRAFT.md` (noema#201, aligned on 11 Nov to the decided G5 gate; not signed). Nothing is recorded before it is signed (plan section 8, F3). I have not recorded, requested or stored any audio.
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

# Session 2026-11-24 · VOI-06 (A379 alignment, in the PR that renames the references)

## Session summary
- What changed: `consent_ref` is `speaker_ref`, `ConsentRecord` is `SpeakerRecord`, `n_without_consent` is `n_without_speaker_ref` in `study.py`, `retention.py` and `field.py` and their tests (a rename only: the 231 tests of the four test files are the same tests under the new names and pass). The store of noema#819 and `store.sh scan` already key objects by `speaker_ref`; the code now agrees with it.
- Why: A379 (consent forms are no longer required) and plan v1.26 (A449) amend the consent wording of VOI-06; keeping `consent_ref` in code would say a consent form is required.
- Not changed: the REQ-VOI-004 sentence in `voice/__init__.py` ("keep raw audio without consent") quotes the PERD; the PERD is not edited (an Architect's change). `voice/field.py` is the G5 field-gate analysis: its rule (a session with no reference is refused) is unchanged, only the name.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when VOI-05 is Done and the devices are in hand, the Founders record the sessions; check them with `study.subset_report` and `retention.scan_objects` (with the retention durations as arguments) and attach the result.
