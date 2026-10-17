# noema_model/providers

**What it does.** The only place in the model service that talks to a model, speech or text-to-speech provider (GPT, Gemini, Claude; Aethex AI, Indigenius, N-ATLAS) behind neutral interfaces, with failover (BD-16; PERD §36.1). Provider SDKs and credentials live here and nowhere else (INV-29).

**REQs.** REQ-INT-002, REQ-VOI-002.

**Status.** Empty boundary package (PLT-01). Filled by UND-02 and VOI-01. No provider SDK is installed yet. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/` (none for this package yet).
