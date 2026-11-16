# Status · VOI-10 (Voice Engineer) · barge-in and provider failover in the voice path

Updated 2026-11-16.

## State
- `voice/failover.py` (provider failover inside one utterance, full replay to the next provider, a replay-overflow refusal, bounded release of the live source) and `voice/bargein.py` (the spoken answer stops when the person speaks; both streams are closed even when one fails to close) are in the PR that replaces noema#218 (closed at the Founder's instruction; same branch).
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
