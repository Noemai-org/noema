"""Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

May import: noema_model.platform and noema_model.providers.

Must never: let a partial transcript set a key slot, keep raw audio without consent, or name a speech vendor outside providers/ (BD-16).

An empty boundary package created by PLT-01; nothing is built here yet.
"""
