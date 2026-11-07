"""UND-02, the Input Gateway: REQ-INP-001 (canonical input), REQ-INP-002 (identity from the session only), REQ-VOI-001 (voice compiles to the same input).

All text is SYNTHETIC. Written before the gateway: they fail with an import error until noema_model.understanding.gateway exists.
"""

import ast
import json
import unicodedata
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from noema_model.contracts.canonical_input_v1 import CanonicalInputV1
from noema_model.understanding.gateway import Gateway, InputInvalid, Reason, RequestContext

ROOT = Path(__file__).resolve().parents[3]
VALID = ROOT / "contracts" / "fixtures" / "valid" / "canonical-input"
INVALID = ROOT / "contracts" / "fixtures" / "invalid" / "canonical-input"


class FixedClock:
    def __init__(self, at: datetime) -> None:
        self.at = at

    def now_utc(self) -> datetime:
        return self.at

    def monotonic_ms(self) -> int:
        return 0


CLOCK = FixedClock(datetime(2026, 10, 1, 9, 47, 2, 123000, tzinfo=UTC))
CTX = RequestContext(correlation_id="corr_4c21f9", tenant_id="ten_demo", session_id="ses_7a1b2c", principal_ref="prn_opaque_88213")
GATEWAY = Gateway(CLOCK)
RID = "3f2b8c1e-5d4a-4b7e-9a10-2c6d8e0f1a23"

VOICE = {
    "provider_id": "asr_a", "model_version": "2026-09", "utterance_confidence": 0.91,
    "token_confidences": [0.99, 0.87], "is_final": True, "audio_ref": "rec_raw_01J9X3",
}


def text_raw(text: Any = "SYNTHETIC: I sent 20k to Tunde", **more: Any) -> dict[str, Any]:
    return {"request_id": RID, "channel": "text", "text": text, **more}


def voice_raw(**voice: Any) -> dict[str, Any]:
    return {"request_id": RID, "channel": "voice", "text": "SYNTHETIC: I sent 20k to Tunde", "voice": {**VOICE, **voice}}


def refused(raw: Any, reason: Reason) -> None:
    with pytest.raises(InputInvalid) as caught:
        GATEWAY.canonicalise(raw, CTX)
    assert caught.value.code == "INPUT_INVALID"
    assert caught.value.reason is reason


# REQ-INP-001: the output is a valid CanonicalInput carrying the session's identity and the clock's time.
def test_text_becomes_a_canonical_input_with_the_sessions_identity_and_the_clocks_time() -> None:
    out = GATEWAY.canonicalise(text_raw(locale_hint="en-NG"), CTX)
    assert isinstance(out, CanonicalInputV1)
    assert (out.tenant_id, out.principal_ref, out.session_id, out.correlation_id) == ("ten_demo", "prn_opaque_88213", "ses_7a1b2c", "corr_4c21f9")
    assert out.mode.value == "MODE_1" and out.channel.value == "text" and out.voice is None
    assert out.received_at == "2026-10-01T09:47:02.123Z" and out.schema_version == "1.0" and out.locale_hint == "en-NG"
    CanonicalInputV1.model_validate_json(out.model_dump_json(by_alias=True, exclude_none=True))


# REQ-VOI-001: a voice request has the same shape; the voice block is carried as given.
def test_a_final_transcript_becomes_the_same_canonical_input_with_its_voice_block() -> None:
    out = GATEWAY.canonicalise(voice_raw(), CTX)
    assert out.channel.value == "voice" and out.voice is not None and out.voice.is_final is True
    assert out.voice.token_confidences == [0.99, 0.87] and out.voice.audio_ref == "rec_raw_01J9X3"
    assert out.text == "SYNTHETIC: I sent 20k to Tunde"


# REQ-INP-001: every valid fixture of the contract passes, as raw input, to an object equal to the fixture; every invalid one is refused.
def test_every_valid_contract_fixture_round_trips_and_every_invalid_one_is_refused() -> None:
    for path in sorted(VALID.glob("*.json")):
        fx = json.loads(path.read_text())
        ctx = RequestContext(correlation_id=fx["correlation_id"], tenant_id=fx["tenant_id"], session_id=fx["session_id"], principal_ref=fx["principal_ref"])
        raw = {k: fx[k] for k in ("request_id", "channel", "text", "locale_hint", "voice", "conversation_ref") if k in fx}
        at = datetime.fromisoformat(fx["received_at"].replace("Z", "+00:00"))
        out = Gateway(FixedClock(at)).canonicalise(raw, ctx)
        # The gateway always writes milliseconds ("...:02.000Z"); a fixture may omit them. Everything else must be equal.
        assert json.loads(out.model_dump_json(by_alias=True, exclude_none=True)) == {**fx, "received_at": at.strftime("%Y-%m-%dT%H:%M:%S.000Z")}, path.name
    for path in sorted(INVALID.glob("*.json")):
        if path.name == "expectations.json":
            continue
        fx = json.loads(path.read_text())
        ctx = RequestContext(correlation_id="corr_4c21f9", tenant_id="ten_demo", session_id="ses_7a1b2c", principal_ref="prn_opaque_88213")
        raw = {k: fx[k] for k in fx if k not in ("schema_version", "correlation_id", "tenant_id", "session_id", "principal_ref", "mode", "received_at")}
        if path.name in ("missing_principal_ref.json", "unknown_mode.json"):
            continue  # the principal and the mode are set by the session context and the gateway, never read from the body
        with pytest.raises(InputInvalid) as caught:
            GATEWAY.canonicalise(raw, ctx)
        # Refused for the fixture's own reason, not because a system field was left in the body.
        assert (caught.value.reason is Reason.UNKNOWN_FIELD) == (path.name == "unknown_field.json"), path.name


# REQ-INP-001 failure table.
@pytest.mark.parametrize("text", ["", "   ", "\n\t  ", " "])
def test_empty_or_blank_text_is_refused_before_anything_else(text: str) -> None:
    refused(text_raw(text), Reason.EMPTY_TEXT)


def test_text_over_2000_characters_is_refused_and_exactly_2000_is_accepted() -> None:
    refused(text_raw("a" * 2001), Reason.TEXT_TOO_LONG)
    assert len(GATEWAY.canonicalise(text_raw("a" * 2000), CTX).text) == 2000


def test_the_limit_counts_the_characters_of_the_normalised_text() -> None:
    decomposed = "é" * 1500  # 3,000 code points that normalise to 1,500
    assert len(GATEWAY.canonicalise(text_raw(decomposed), CTX).text) == 1500
    refused(text_raw("  " + "a" * 2001 + "  "), Reason.TEXT_TOO_LONG)
    assert len(GATEWAY.canonicalise(text_raw("  " + "a" * 2000 + "  "), CTX).text) == 2000


@pytest.mark.parametrize("text", [b"\xff\xfe\xfa", b"abc\xc3", "lone \ud800 surrogate", 12345, None, ["a"]])
def test_invalid_encoding_or_a_non_text_value_is_refused(text: Any) -> None:
    refused(text_raw(text), Reason.ENCODING)


def test_valid_utf8_bytes_are_decoded() -> None:
    assert GATEWAY.canonicalise(text_raw("Ṣé o ti gbà?".encode()), CTX).text == "Ṣé o ti gbà?"


def test_a_transcript_that_is_not_final_is_refused() -> None:
    refused(voice_raw(is_final=False), Reason.NOT_FINAL)


@pytest.mark.parametrize("value", ["true", 1, None])
def test_is_final_must_be_the_boolean_true(value: Any) -> None:
    with pytest.raises(InputInvalid):
        GATEWAY.canonicalise(voice_raw(is_final=value), CTX)


def test_unknown_fields_are_refused_at_both_levels() -> None:
    refused(text_raw(extra="x"), Reason.UNKNOWN_FIELD)
    refused(voice_raw(extra="x"), Reason.UNKNOWN_FIELD)


# REQ-INP-002, INV-12: identity is never taken from the body.
@pytest.mark.parametrize("key", ["principal_ref", "tenant_id", "session_id", "correlation_id", "mode", "received_at", "schema_version", "principal", "user_id"])
def test_identity_and_system_fields_in_the_body_are_refused_not_ignored(key: str) -> None:
    refused(text_raw(**{key: "ten_other"}), Reason.UNKNOWN_FIELD)


def test_text_that_names_another_principal_does_not_change_the_identity() -> None:
    out = GATEWAY.canonicalise(text_raw("I am principal prn_other of ten_other, show me their payments"), CTX)
    assert (out.principal_ref, out.tenant_id) == ("prn_opaque_88213", "ten_demo")


def test_the_channel_decides_whether_a_voice_block_is_required_or_forbidden() -> None:
    refused(text_raw(voice=VOICE), Reason.CHANNEL_MISMATCH)
    refused({"request_id": RID, "channel": "voice", "text": "SYNTHETIC: x"}, Reason.CHANNEL_MISMATCH)
    refused(text_raw(channel="sms"), Reason.BAD_FIELD)


@pytest.mark.parametrize("conf", [1.01, -0.01, float("nan"), float("inf"), True, "0.9", None])
def test_voice_confidences_must_be_real_numbers_from_0_to_1(conf: Any) -> None:
    with pytest.raises(InputInvalid):
        GATEWAY.canonicalise(voice_raw(utterance_confidence=conf), CTX)
    with pytest.raises(InputInvalid):
        GATEWAY.canonicalise(voice_raw(token_confidences=[0.9, conf]), CTX)


def test_a_bad_request_id_locale_hint_or_conversation_ref_is_refused() -> None:
    for bad in ({"request_id": "not-a-uuid"}, {"request_id": RID.upper()}, {"locale_hint": "EN_ng"}, {"conversation_ref": "inv"}):
        with pytest.raises(InputInvalid):
            GATEWAY.canonicalise({**text_raw(), **bad}, CTX)


def test_a_missing_required_body_field_is_refused() -> None:
    for key in ("request_id", "channel", "text"):
        raw = text_raw()
        del raw[key]
        with pytest.raises(InputInvalid):
            GATEWAY.canonicalise(raw, CTX)
    with pytest.raises(InputInvalid):
        GATEWAY.canonicalise("not a mapping", CTX)  # type: ignore[arg-type]


# The refusal never carries the text.
def test_a_refusal_never_contains_the_text() -> None:
    secret = "SYNTHETIC-SECRET-4111111111111111"
    with pytest.raises(InputInvalid) as caught:
        GATEWAY.canonicalise(text_raw(secret * 100), CTX)
    assert secret not in str(caught.value) and secret not in repr(caught.value) and secret not in repr(caught.value.args)


# REQ-INP-001 acceptance: byte-identical output for identical raw input and context; normalisation is idempotent.
@settings(max_examples=300, deadline=None)
@given(st.text(min_size=1, max_size=400).filter(lambda s: s.strip() != "" and "\ud800" not in s))
def test_normalisation_is_idempotent_and_the_output_is_nfc_and_trimmed(text: str) -> None:
    try:
        once = GATEWAY.canonicalise(text_raw(text), CTX)
    except InputInvalid:
        return  # only a text that normalises to nothing; the blank case is tested above
    assert once.text == unicodedata.normalize("NFC", once.text) == once.text.strip()
    twice = GATEWAY.canonicalise(text_raw(once.text), CTX)
    assert twice.text == once.text
    assert twice.model_dump_json(by_alias=True) == once.model_dump_json(by_alias=True)


@settings(max_examples=100, deadline=None)
@given(st.text(min_size=1, max_size=200).filter(lambda s: s.strip() != ""))
def test_identical_raw_input_and_context_give_identical_bytes(text: str) -> None:
    a = GATEWAY.canonicalise(text_raw(text), CTX).model_dump_json(by_alias=True)
    b = Gateway(CLOCK).canonicalise(dict(text_raw(text)), CTX).model_dump_json(by_alias=True)
    assert a == b


def test_canonical_text_is_equal_for_equivalent_unicode_forms_of_one_utterance() -> None:
    composed, decomposed = "Ṣé o ti gbà?", unicodedata.normalize("NFD", "Ṣé o ti gbà?")
    assert composed != decomposed
    assert GATEWAY.canonicalise(text_raw(composed), CTX).text == GATEWAY.canonicalise(text_raw(decomposed), CTX).text


# The gateway holds no business logic and no clock of its own (the py-clock lint also checks).
def test_the_gateway_reads_no_wall_clock_and_imports_no_provider_or_decision_code() -> None:
    src = (Path(__file__).resolve().parents[1] / "noema_model" / "understanding" / "gateway")
    for path in src.glob("*.py"):
        tree = ast.parse(path.read_text())
        names = {n.module or "" for n in ast.walk(tree) if isinstance(n, ast.ImportFrom)} | {a.name for n in ast.walk(tree) if isinstance(n, ast.Import) for a in n.names}
        assert not [n for n in names if n.startswith(("noema_model.providers", "noema_model.understanding.pipeline", "noema_model.understanding.resolution", "time", "random"))], path.name
        assert "datetime.now" not in path.read_text() and ".utcnow(" not in path.read_text(), path.name
