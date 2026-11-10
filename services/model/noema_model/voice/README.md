# noema_model/voice

**What it does.** Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

**REQs.** REQ-VOI-001, REQ-VOI-002, REQ-VOI-003.

**Status.** `field.py` (VOI-14): `measure` (the numbers, no verdict) and `analyse` (the field gate; it refuses any argument weaker than the Founder's F5 or PERD §45.4, and a floor of zero), with notes when failures or an open upper bound weaken the evidence. `confidence.py` (VOI-03): per-token to per-slot confidence, a missing confidence counts as low. The rest is filled by VOI-02 to VOI-16. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_voice_confidence.py`, `tests/test_voice_field.py`.
