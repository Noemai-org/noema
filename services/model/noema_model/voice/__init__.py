"""Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

May import: noema_model.platform and noema_model.providers.speech, and nothing else in providers (ruling A9, enforced by tools/lint/pyrules.py).

Must never: let a partial transcript set a key slot, keep raw audio without consent, or name a speech vendor outside providers/ (BD-16).

Created by PLT-01. Built so far: `confidence.py` (VOI-03), `endpoint.py` and `pipeline.py` (VOI-02). See README.md.
"""
