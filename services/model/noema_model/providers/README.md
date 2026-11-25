# noema_model/providers

**What it does.** The only place in the model service that talks to a model, speech or text-to-speech provider (GPT, Gemini, Claude; Aethex AI, Indigenius, N-ATLAS) behind neutral interfaces, with failover (BD-16; PERD §36.1). Provider SDKs and credentials live here and nowhere else (INV-29).

**REQs.** REQ-INT-002, REQ-VOI-002.

**Status.** `llm/` holds the text-model adapters for the harness (T5): `openai_chat`, `anthropic_messages`, `gemini_generate`, the neutral types in `base.py`, the standard-library `UrllibTransport` (https only, no redirects, size cap, hard deadline) and the kind registry. No vendor SDK is used; each adapter is one HTTP call. `speech/` holds the neutral streaming types, the stream-order check, the capability matrix (`capability_matrix.json`, every cell UNVERIFIED) and three refusing scaffolds for Aethex AI, Indigenius and N-ATLAS (VOI-01); no wire format is written because no vendor document or sample has been read. `openai_decisions` is the decision-model adapter (predicate, choice and score questions; see the harness README for the mapping, the limits and the snapshot gap). The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_providers_contract.py` (fixtures; not run against a live endpoint: no access) and `tests/test_transport.py` (a real local socket).

## OpenAI Responses (`llm/openai_responses.py`)

Kind `openai_responses`: `POST /v1/responses` with `instructions`, `input`, a strict `json_schema` text format, `max_output_tokens`, `store: false` and `reasoning.effort` from the provider configuration (the adapter holds no effort of its own). The reply is one `message` item with one `output_text` or one `refusal`; reasoning items are dropped unread; reasoning tokens are part of `usage.output_tokens`. `incomplete` with `max_output_tokens` is a truncation and with `content_filter` a refusal; any other status, or no message, or more than one, is malformed. Written from the endpoint reference read on 2026-10-27; the fixtures are synthetic: not run against a live endpoint (no access).


## Recorded replay (`llm/replay.py`, A619)

`ReplayTransport` stands where `UrllibTransport` stands and answers a request from a committed, SYNTHETIC recording instead of the network, so the real adapter (`openai_decisions` for a choice and a yes/no gate, or the text adapters) and the real harness run unchanged above it. The key is the SHA-256 of the exact request bytes, as the recorder stored it in the manifest (A261); one byte of difference is a miss. A request with no recording raises `ReplayMiss` (a `ProviderError` of kind REJECTED, code `UNKNOWN_REQUEST`); a recorded failure is evidence and is refused as `EVIDENCE_ONLY` (A320); nothing falls through to a live call and nothing is answered by default. The directory is given at construction and read once: a manifest and one `rec-NNNN.json` per row, regular files only, at most 1 MiB each, strict JSON, labelled SYNTHETIC (AGENT-25), no extra entry. The module imports no network, process, clock or random module and reads no credential header. Not built here: the profile switch and the refusal to start in the demo profile (UND-15's other piece), and the G1 fixtures (reduced from the next passing live run). Tests: `tests/test_replay_transport.py`.
