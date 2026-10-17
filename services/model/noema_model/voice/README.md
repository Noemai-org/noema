# noema_model/voice

**What it does.** Speech in and out behind a provider-neutral interface: streaming transcription, per-token confidence and text to speech (SoT §32; PERD §35).

**REQs.** REQ-VOI-001, REQ-VOI-002, REQ-VOI-003.

**Status.** Empty boundary package (PLT-01). Filled by VOI-01 to VOI-16. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/` (none for this package yet).
