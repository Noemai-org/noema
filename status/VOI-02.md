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
