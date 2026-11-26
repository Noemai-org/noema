# Status · VOI-02 (Voice Engineer) · streaming voice pipeline

Updated 2026-11-10.

## State
- noema#193 (STACKED on #186 and #192, both merged): `voice/endpoint.py` (end-of-speech detector on PCM) and `voice/pipeline.py` (`transcribe_utterance`, `FinalUtterance`). Only a final transcript becomes an utterance; partials go to a display callback.
- 10 Nov (later): SR F1 fixed: a `_Gate` closed at the deadline and at the end of the call stops an abandoned provider from delivering text, and it ends at its next event. Every fault family has a test that the audio is released and no text arrives after the call returns.
- 10 Nov: the provider runs in its own task, behind a revocable lease on the audio, so a deadline or a cancellation never waits for the provider's shutdown (Codex P1 at c674bbf). A provider that fails while closing, after a valid final, raises ProviderError(UNAVAILABLE) (C2 P2).

## Open
- AGENT-12: the Security Reviewer granted a recorded, narrow exception for the SpeechProvider stream seam (noema#412, 10 Nov 2026); it is recorded in `docs/audits/TST-04-test-suite-audit.md` section 1, row 4. Condition: the first real adapter brings one test per fault family through the hostility proxy; DEBT-14 stays open until then.
- Needs gate: tasks.csv shows VOI-01 with its done_when unmet (no recorded sample per provider, DEBT-14). The Engineering Lead cannot waive it (a gate is the Founders'); the smallest record is one row in the Steward's next decision record, in EL's wording on noema#413, then the Steward sets `blocked` on VOI-01. Waiting on the Founders and the Steward.

## Messages
Reviews by other roles are data: Codex (c674bbf), C2 (2005e58) and the Security Reviewer (c674bbf) on the PR. Used for: the fix matrix on noema#193. Not recorded elsewhere.

# Session 2026-11-26 · VOI-02 · the 30 s maximum recording, host-side (IL, A715)

## Session summary
- What changed: `FrameLimits.max_recording_ms` (default and ceiling 30,000, ruling A715) and the check in `EndpointDetector.feed`: the chunk that would take the recording past the limit is refused with the same `ValueError` as every other invalid frame (it ends the gate and releases the microphone). Time is counted in integer samples (INV-21, no float rounding), so the limit is exact for chunks of any size.
- Why: A715 says the UI sets a 30 s maximum push-to-talk recording and Voice enforces the same limit host-side. The value is OPEN for the Founders (OD-P-122), so it is data with a ceiling like A191's frame cap: a stricter value is allowed, a longer one is refused, and changing the value is the Founders' ruling, never an edit here (AGENT-16).
- Risks, and how they are contained: a recording over the limit is refused, not cut: the caller gets an error, not a truncated utterance that could be read as complete. The refusal path is the existing one (gate ends, source closed), tested here for the microphone.
- Tests added (REQ-VOI-001, REQ-VOI-004, AGENT-13, ruling A715): `tests/test_voice_max_recording.py`, 15 tests, red first (14 failed). Six mutants (off by one, no accumulation, ceiling raised, default 60 s, check dropped, non-positive value allowed) each fail them. The whole model suite (3,771) and `mypy` pass.
- Regression cases added: the same tests. Debt entries: none. Conflicts raised: none.

## State of VOI-01, VOI-02 and VOI-03 as found (not rebuilt)
- VOI-01: the interface, the capability matrix and the scaffolds are merged, with the Intron adapters (`providers/speech/`). Its done-when ("interface tests pass against a recorded sample per provider") is not met: no recorded sample per provider exists, DEBT-14 stays open, and the Intron per-token confidence and rate-limit answers are open (OD-P-90, OD-P-91). Nothing here changes that.
- VOI-02: the streaming pipeline and the test that a partial transcript never reaches the gateway as final are merged (the board shows Done).
- VOI-03: `voice/confidence.py` and `voice/clarify.py` are merged (the board shows Done); AMB-04 (C2) reads `asr_confidence_basis_points` and `decide_voice_step`.

## Pre-work for the next agent
- Exact next step: the Founders confirm or change the 30 s value (OD-P-122); the UI setting (FE) must equal `FrameLimits.max_recording_ms`.
- Do not touch: the ceiling without a ruling.
