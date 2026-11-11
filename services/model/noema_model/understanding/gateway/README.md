# understanding/gateway

**What it does.** The Input Gateway (UND-02): one raw request body and the authenticated context in, one `CanonicalInput` (canonical-input.v2) or an `INPUT_INVALID` refusal out.

**REQs.** REQ-INP-001, REQ-INP-002, REQ-VOI-001. SoT INV-12, INV-02.

**Rules (`gateway.py`).**
- The body may carry only `request_id`, `channel`, `text`, `locale_hint`, `voice` and `conversation_ref`. Identity, tenant, session, correlation, mode and time come from the `RequestContext` (built by core from the session) and the injected `Clock`. A body that names any of them is refused with `UNKNOWN_FIELD`, not ignored, so an attempt is visible.
- Text: bytes are decoded as strict UTF-8; a lone surrogate or a non-text value is `ENCODING`; the result is NFC, trimmed, then 1 to 2,000 characters (`EMPTY_TEXT`, `TEXT_TOO_LONG`, counted after normalisation).
- `channel=voice` needs the voice block and `channel=text` forbids it (`CHANNEL_MISMATCH`). A transcript with `is_final` false is `NOT_FINAL`; `is_final` must be the boolean `true`. Confidences must be real numbers from 0 to 1 (bool, NaN, infinity and strings are `BAD_FIELD`).
- `voice.audio_ref` must be present and `null` (the audio was discarded, REQ-VOI-004). A non-null reference of any shape is `AUDIO_REF_NOT_ALLOWED` until a consent-backed retention path exists (CR docs#137, noema#398); the check that a retained reference has a tenant- and session-bound consent event is core's edge, not here.
- Serialising: dump a voice `CanonicalInput` **without** `exclude_none`. `voice.audio_ref` is a required key whose value is `null`; `exclude_none=True` drops it and the result is no longer a valid canonical-input.v2 document.
- The refusal names a `Reason` and a field and never carries a value, so the text never reaches a log through an error. The gateway logs nothing.
- Same raw body and context give the same bytes; normalising a canonical text again changes nothing (property tests).

**Not here.** The gateway does **not** deduplicate on `request_id`: REQ-INP-003 is the caller's job, per the contract's description (a redelivered body gives the same canonical input again). Deduplication by `request_id` (REQ-INP-003, core), the schema gate on the Intent Object and `intent_hash` (UND-04), and the `INPUT_RECEIVED` Record event with the input hash (core). The `request_id` must already be a lower-case UUID; the gateway does not normalise identifiers.

**Tests.** `pytest tests/test_input_gateway.py` from `services/model/`.
