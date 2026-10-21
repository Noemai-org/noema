# noema_model/providers

**What it does.** The only place in the model service that talks to a model, speech or text-to-speech provider (GPT, Gemini, Claude; Aethex AI, Indigenius, N-ATLAS) behind neutral interfaces, with failover (BD-16; PERD §36.1). Provider SDKs and credentials live here and nowhere else (INV-29).

**REQs.** REQ-INT-002, REQ-VOI-002.

**Status.** `llm/` holds the text-model adapters for the harness (T5): `openai_chat`, `anthropic_messages`, `gemini_generate`, the neutral types in `base.py`, the standard-library `UrllibTransport` (https only, no redirects, size cap, hard deadline) and the kind registry. No vendor SDK is used; each adapter is one HTTP call. `speech/` is still empty (VOI-01). The OpenAI Decisions adapter is not written yet: see `docs/sessions/`. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_providers_contract.py` (fixtures; not run against a live endpoint: no access) and `tests/test_transport.py` (a real local socket).
