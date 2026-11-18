# Status · VOI-10 (Voice Engineer) · barge-in and provider failover in the voice path

Updated 2026-11-16.

## State
- `voice/degrade.py` (one attempt with one provider; a speech failure degrades to text, the source is always closed, an unqualified provider is never called; no replay, ruling A249) and `voice/bargein.py` (the spoken answer stops when the person speaks; both streams are closed side by side within a bound) are in the PR that replaces noema#218.
- The Security Reviewer's four blocking findings on #218 are fixed there: the SYNTHETIC labels on every scripted-provider test; the source asserted closed in each fault family; nothing reaches the display after the call returns; and a provider that is not qualified or whose data terms are not confirmed is never called and never given a chunk (REQ-INT-006, PERD R-18, A180), the skip being listed in the result.
- A179 (Intron the sole integrated provider) takes effect only with the SoT edit. When it does, "failover" becomes "degrade to text" with a list of one provider; the same code handles it.

## Open
- The first real adapter brings one test per fault family through the hostility proxy and a test that its `aclose()` honours cancellation (the conditions of the AGENT-12 exception, noema#412); DEBT-14 stays open until then.
- The caller records `PROVIDER_FAILOVER` for each step (including skipped ones); nothing here writes the Record.

## Messages
Reviews by other roles are data: the Security Reviewer on noema#218 (the four findings and the note on `bargein`), Codex on noema#474 (the release machinery this reuses). Used for: the fix matrix on the PR.

# Session 2026-11-16 13:00 WAT · VOI-10

## Session summary
- What changed (PR links): the PR that replaces noema#218; `voice/failover.py`, `voice/pipeline.py` (public `begin_close`, `cleanup_reporter`, `refuse_unweakable`), tests.
- Why: the findings on #218; #474, #213 and #200 have merged, so the branch is rebuilt on `main`.
- Risks, and how they are contained: audio is never handed to an unqualified provider (tested); a microphone is closed in every family (tested); a source that cannot be weakly referenced is refused at the boundary and still closed.
- Tests added (REQ, INV, US or EC IDs): `test_voice_failover_eligibility.py` (REQ-INT-006, PERD R-18), `test_voice_failover_release.py` (INV-07, AGENT-13).
- Regression cases added: none (voice has no benchmark case yet).
- Debt entries: none new.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: wire the caller that records `PROVIDER_FAILOVER` from `FailoverStep` once the voice path reaches the gateway (VOI-09).
- Open questions (owner): none for Voice.
- Files to read first: `voice/failover.py`, `tests/test_voice_failover_release.py`.
- Do not touch: any real adapter code before the SoT edit of A179.

# Session 2026-11-16 15:00 WAT · VOI-10 (second round)

## Session summary
- What changed (PR links): noema#621 (`voice/bargein.py`, `voice/failover.py`, `voice/pipeline.py`, tests).
- Why: the Security Reviewer's four blocking findings and Codex's P1 on #621 at 11a1d089: barge-in closes were unbounded; the replay buffer was never dropped; replay to a second vendor had no ruling; a configuration error left the source open.
- Risks, and how they are contained: both barge-in streams are closed side by side within `close_s`, a stalled close is cancelled and reported, `release_failed` says so; the buffer is cleared on every path and bounded in bytes by the A191 frame cap; `allow_replay` is a required argument with no default, so sending a person's voice to a second vendor is an explicit decision.
- Tests added (REQ, INV, US or EC IDs): `test_voice_failover_findings.py` (REQ-VOI-002, REQ-VOI-004, AGENT-13, A191).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none. **A ruling is asked for on replay (see the PR): until it is given, a caller must pass `allow_replay` explicitly and a Founder or the Architect decides the value.**

## Pre-work for the next agent
- Exact next step: the caller that supplies `allow_replay`, `close_s` and the per-provider request (`model_version` is the same for every provider in one call, which may be the wrong pin for the second) from configuration when the voice path reaches the gateway (VOI-09).
- Open questions (owner): may audio one vendor heard be replayed to another (Architect or Founder; REQ-VOI-004, PERD R-18, A180).
- Files to read first: `voice/failover.py`, `voice/bargein.py`, `tests/test_voice_failover_findings.py`.
- Do not touch: the exception text in `endpoint.py` carries the frame cap and rate numbers; keep it out of logs (separate small change).

# Session 2026-11-16 17:00 WAT · VOI-10 (third round: no replay, ruling A249)

## Session summary
- What changed (PR links): noema#621: `voice/failover.py` and its replay buffer are removed; `voice/degrade.py` (`transcribe_or_text`: one attempt, one provider, degrade to text) replaces it; barge-in keeps its bounded release; the failover tests are replaced by `test_voice_degrade.py`, `test_voice_bargein.py` and `test_voice_bargein_bounded.py`.
- Why: A249 (interim): there is no replay to a second provider; failover means degrade to text. That also removes the Security Reviewer's blocking findings 2 and 3 and note 6 (nothing is buffered, nothing is replayed) and closes noema#630.
- Risks, and how they are contained: audio is sent once, live, to one eligible provider and kept nowhere; an ineligible provider is never called; the source is closed on every path; a second provider, if the Founders add one (OD-P-88), is a new attempt with fresh audio.
- Tests added (REQ, INV, US or EC IDs): `test_voice_degrade.py` (REQ-VOI-002, REQ-INT-006, INV-07, AGENT-13).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the caller records the failure kind of a `DegradeResult` (`PROVIDER_FAILOVER` becomes a degrade event) once the voice path reaches the gateway (VOI-09); the Intron adapters are separate PRs.
- Open questions (owner): none for Voice.
- Files to read first: `voice/degrade.py`, `tests/test_voice_degrade.py`.
- Do not touch: nothing here sets a threshold.

# Session 2026-11-18 · VOI-10 (Codex round at 20b6d355)

## Session summary
- What changed: `PlaybackBounds(read_s, close_s, hold_s)` is validated when built, so `play_until_barge_in` is never handed an invalid bound while it holds live streams (Codex P2s on invalid-bound cleanup: the refusal path is removed rather than made safe). A read that timed out, or was cancelled with the call, is kept with its source; the release waits up to `close_s` for it to end, closes the source after it, and hands a source whose read will not end to `close_when_read_ends` (Codex P2 on closing a generator that is still running).
- API change: `play_until_barge_in(..., bounds=PlaybackBounds(...))` replaces `close_s=` and `read_s=`; no caller outside the tests exists yet.
- Tests: the tests that passed a bad bound into a call and checked the release are replaced by one that a bad bound cannot be built; new test for the late-ending read (red first at fe71968a: VOI_AUDIO_DEFERRED_CLOSE_FAILED).
- Debt: none. Conflicts: none.

## Pre-work for the next agent
- Exact next step: wire `PlaybackBounds` from configuration when the voice session is assembled.
