# noema_model/voice

**What it does.** Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

**REQs.** REQ-VOI-001, REQ-VOI-002, REQ-VOI-003.

**Status.** `confidence.py` (VOI-03), `endpoint.py` and `pipeline.py` (VOI-02): per-slot confidence, end-of-speech detection on PCM, and the stream that turns a provider's events into one FinalUtterance (partials go to the display only). Barge-in, failover and TTS come later (VOI-04, VOI-10). The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_voice_confidence.py`.
