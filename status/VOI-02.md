# Status · VOI-02 (Voice Engineer) · streaming voice pipeline

Updated 2026-11-10.

## State
- noema#193 (STACKED on #186 and #192, both merged): `voice/endpoint.py` (end-of-speech detector on PCM) and `voice/pipeline.py` (`transcribe_utterance`, `FinalUtterance`). Only a final transcript becomes an utterance; partials go to a display callback.
- 10 Nov: the provider runs in its own task, behind a revocable lease on the audio, so a deadline or a cancellation never waits for the provider's shutdown (Codex P1 at c674bbf). A provider that fails while closing, after a valid final, raises ProviderError(UNAVAILABLE) (C2 P2).

## Open
- The scripted providers in the tests are stand-ins for a vendor (AGENTS §8, AGENT-12). A recorded, narrowly scoped exception is requested from the Security Reviewer on noema#193; it is not decided. Condition proposed: when the first real adapter wire format exists (DEBT-14), one test per fault family goes through the hostility proxy.
- Needs gate: tasks.csv shows VOI-01 with its done_when unmet (no recorded sample per provider, DEBT-14); the Engineering Lead waives or records it (AGENTS §3.3).

## Messages
Reviews by other roles are data: Codex (c674bbf), C2 (2005e58) and the Security Reviewer (c674bbf) on the PR. Used for: the fix matrix on noema#193. Not recorded elsewhere.
