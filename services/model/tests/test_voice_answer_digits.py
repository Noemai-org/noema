"""The spoken answer never carries an unmasked run of digits (VOI-04, REQ-VOI-002, REQ-SEC-003; INV-02, AGENT-13; rulings A181 and A246).

The core hands the voice path text it has already masked (A181). This is the last line of defence before the audio: the answer text is checked with the model service's one definition of a
digit run (`platform.redaction.digit_runs`, A246) against the pack's masking limit (never a number of its own here, AGENT-16). A text with a longer run is not sent to any provider and not
spoken: the person gets the text, and the delivery carries a fixed code saying why. SYNTHETIC answers and a scripted synthesizer (AGENT-12 exception, noema#412).
"""

import pytest

from noema_model.platform.masking import MaskingLimits
from noema_model.providers.speech.base import Locale, Support
from noema_model.providers.speech.tts import SynthesisRequest
from noema_model.voice.answer import UNMASKED_DIGITS, DeliveryMode, deliver_answer
from tests.support import MASKING, run
from tests.test_voice_answer import FakeSynth, audio, caps


def deliver(text: str) -> tuple[object, FakeSynth]:
    synth = FakeSynth(caps(Support.YES), audio(2))
    result = run(deliver_answer(synth, SynthesisRequest(text, Locale.PCM, "pinned-1"), deadline_s=5.0, masking=MASKING))
    return result, synth


@pytest.mark.parametrize("text", [
    "We found your transfer to the account ending in 4821.",  # four digits: the limit itself
    "Your balance is 50,000 naira.",  # groups of at most three, and a comma is not a separator
    "It will take 2 to 3 days.",
    "Reference ABC1234 is ready.",
])
def test_text_within_the_masking_limit_is_spoken(text: str) -> None:
    result, synth = deliver(text)
    assert result.mode is DeliveryMode.SPEAK and result.withheld is None and synth.calls == 1  # type: ignore[attr-defined]


@pytest.mark.parametrize("text", [
    "The account number is 0123456789.",
    "Your card 12345 was used.",  # one more than the limit
    "Call 0803 123 4567.",  # spaces join the groups into one run
    "The number is 0803-123-4567.",
    "Digits 12​345 with a zero-width space.",
    "Arabic-Indic digits ١٢٣٤٥.",
    "Full-width １２３４５.",
    "Brackets (1234) (5678).",
])
def test_text_with_a_longer_run_is_withheld_not_sent_to_the_provider_and_the_person_gets_the_text(text: str) -> None:
    result, synth = deliver(text)
    assert result.mode is DeliveryMode.TEXT_ONLY and result.audio == () and result.text == text and result.withheld == UNMASKED_DIGITS  # type: ignore[attr-defined]
    assert synth.calls == 0  # the provider never saw it


def test_the_code_is_a_fixed_string_that_carries_none_of_the_text() -> None:
    assert UNMASKED_DIGITS == "VOI_TTS_UNMASKED_DIGITS"


def test_the_limit_is_the_packs_not_a_number_of_the_voice_path() -> None:
    assert MASKING == MaskingLimits(4)  # the only value masking-limits.v1 allows; the guard reads `max_unmasked_run`, and a different limit would move the boundary
    synth = FakeSynth(caps(Support.YES), audio(1))
    with pytest.raises(TypeError):
        run(deliver_answer(synth, SynthesisRequest("hello there", Locale.PCM, "pinned-1"), deadline_s=5.0))  # type: ignore[call-arg]


def test_a_locale_that_would_be_text_only_anyway_is_not_marked_withheld() -> None:
    synth = FakeSynth(caps(Support.NO), audio(1))
    result = run(deliver_answer(synth, SynthesisRequest("Your card 12345 was used.", Locale.PCM, "pinned-1"), deadline_s=5.0, masking=MASKING))
    assert result.mode is DeliveryMode.TEXT_ONLY and result.withheld is None and synth.calls == 0
