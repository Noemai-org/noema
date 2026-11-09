# noema_model/voice

**What it does.** Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

**REQs.** REQ-VOI-001, REQ-VOI-002, REQ-VOI-003.

**Status.** `confidence.py` (VOI-03): per-token to per-slot confidence, a missing confidence counts as low. The rest is filled by VOI-02 to VOI-16. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_voice_confidence.py`.
