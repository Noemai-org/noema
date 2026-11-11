# noema_model/voice

**What it does.** Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

**REQs.** REQ-VOI-001, REQ-VOI-002, REQ-VOI-003.

**Status.** `confidence.py` (VOI-03), `clarify.py` (the clarification decision: confirm the heard value, or offer text after the allowed failed turns; a decision function only, wired by AMB-04), `endpoint.py` and `pipeline.py` (VOI-02): per-slot confidence, end-of-speech detection on PCM, and the stream that turns a provider's events into one FinalUtterance (partials go to the display only). `pipeline.py` runs the provider in its own task: a deadline or a cancellation never waits for the provider's shutdown, and the provider reads the audio through a lease that is revoked at the end of the call. Barge-in, failover and TTS come later (VOI-04, VOI-10). The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_voice_confidence.py`, `tests/test_voice_clarify.py`; `tests/test_voice_pipeline.py`, `test_voice_lifecycle.py`, `test_voice_cleanup.py`, `test_voice_cancellation.py` and `test_voice_release.py` (VOI-02; shared doubles in `tests/support_voice.py`). The providers in these tests are scripted stand-ins for a vendor, a recorded exception to AGENTS §8 (AGENT-12) granted by the Security Reviewer on noema#412 and recorded in `docs/audits/TST-04-test-suite-audit.md` section 1, row 4.
