# noema_model/voice

**What it does.** Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

**REQs.** REQ-VOI-001, REQ-VOI-002, REQ-VOI-003.

**Status.** `confidence.py` (VOI-03), `endpoint.py` and `pipeline.py` (VOI-02): per-slot confidence, end-of-speech detection on PCM, and the stream that turns a provider's events into one FinalUtterance (partials go to the display only). `pipeline.py` runs the provider in its own task: a deadline or a cancellation never waits for the provider's shutdown, and the provider reads the audio through a lease that is revoked at the end of the call. Barge-in, failover and TTS come later (VOI-04, VOI-10). The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_voice_confidence.py` (VOI-03); `tests/test_voice_pipeline.py`, `test_voice_lifecycle.py`, `test_voice_cleanup.py` and `test_voice_cancellation.py` (VOI-02; shared doubles in `tests/support_voice.py`). The providers in these tests are scripted stand-ins for a vendor, a recorded exception to AGENTS §8 that is requested on noema#193 and not yet decided.
