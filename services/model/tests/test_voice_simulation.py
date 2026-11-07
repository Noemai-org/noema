"""Voice simulation library tests (VOI-17, REQ-VOI-007).

Everything here is synthetic: a generated tone stands in for speech and the noise is generated, never recorded, so no
contributor consent is involved (REQ-VOI-004). The library never replaces the field gate (REQ-VOI-005).
"""

import json
import math
import random
import struct
from array import array

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec, Locale
from noema_model.voice.simulation.channel import NetworkProfile, low_quality_mic, simulate_network
from noema_model.voice.simulation.noise import NoiseProfile, mix_at_snr, mix_parts_at_snr, rms, synth_noise
from noema_model.voice.simulation.report import (
    NativeReview,
    ScenarioResult,
    SimulationReport,
    ready_for_field,
    report_json,
)

RATE = 16000


def tone(ms: int = 500, hz: int = 220, amplitude: int = 8000) -> bytes:
    count = RATE * ms // 1000
    return struct.pack(f"<{count}h", *[int(amplitude * math.sin(2 * math.pi * hz * i / RATE)) for i in range(count)])


def samples(data: bytes) -> list[int]:
    out = array("h")
    out.frombytes(data)
    return list(out)


# ---- noise ------------------------------------------------------------------------------------------------------------


@pytest.mark.parametrize("profile", list(NoiseProfile))
def test_noise_is_deterministic_for_a_seed_and_differs_between_seeds(profile: NoiseProfile) -> None:
    first = synth_noise(profile, 4000, RATE, random.Random(7))
    assert first == synth_noise(profile, 4000, RATE, random.Random(7))
    assert first != synth_noise(profile, 4000, RATE, random.Random(8))
    assert len(first) == 4000


@pytest.mark.parametrize("profile", list(NoiseProfile))
def test_noise_is_normalised_to_unit_rms(profile: NoiseProfile) -> None:
    assert rms(synth_noise(profile, 8000, RATE, random.Random(1))) == pytest.approx(1.0, rel=1e-6)


@pytest.mark.parametrize("snr_db", [0, 10, 20])
def test_the_mix_has_the_requested_signal_to_noise_ratio(snr_db: int) -> None:
    parts = mix_parts_at_snr(tone(), NoiseProfile.MARKET, snr_db, RATE, random.Random(3))  # the parts, so a headroom scale cannot skew it
    measured = 20 * math.log10(rms(parts.speech) / rms(parts.noise))
    assert measured == pytest.approx(snr_db, abs=0.5)


def test_the_mix_never_wraps_around_and_keeps_the_length() -> None:
    speech = tone(amplitude=32000)
    mixed = samples(mix_at_snr(speech, NoiseProfile.STREET, -6, RATE, random.Random(3)))
    assert len(mixed) == len(samples(speech))
    assert all(-32768 <= value <= 32767 for value in mixed)


def test_silent_speech_cannot_be_mixed_to_a_ratio() -> None:
    with pytest.raises(ValueError):
        mix_at_snr(b"\x00\x00" * 100, NoiseProfile.STREET, 10, RATE, random.Random(1))


@pytest.mark.parametrize("bad", [b"", b"\x01"])
def test_empty_or_odd_audio_is_refused(bad: bytes) -> None:
    with pytest.raises(ValueError):
        mix_at_snr(bad, NoiseProfile.STREET, 10, RATE, random.Random(1))


# ---- low-quality microphone -----------------------------------------------------------------------------------------


def test_a_low_pass_removes_high_frequencies_and_keeps_low_ones() -> None:
    low = samples(low_quality_mic(tone(hz=200), RATE, cutoff_hz=1000, bits=16, clip_level=32767))
    high = samples(low_quality_mic(tone(hz=6000), RATE, cutoff_hz=1000, bits=16, clip_level=32767))
    assert rms(low) > 0.8 * rms(samples(tone(hz=200)))
    assert rms(high) < 0.3 * rms(samples(tone(hz=6000)))


def test_quantising_to_fewer_bits_leaves_fewer_distinct_levels() -> None:
    coarse = set(samples(low_quality_mic(tone(), RATE, cutoff_hz=7000, bits=6, clip_level=32767)))
    fine = set(samples(low_quality_mic(tone(), RATE, cutoff_hz=7000, bits=16, clip_level=32767)))
    assert len(coarse) <= 2**6 and len(coarse) < len(fine)


def test_clipping_limits_the_peak() -> None:
    clipped = samples(low_quality_mic(tone(amplitude=20000), RATE, cutoff_hz=7000, bits=16, clip_level=5000))
    assert max(abs(v) for v in clipped) <= 5000


@pytest.mark.parametrize("cutoff,bits,clip", [(0, 8, 100), (8000, 8, 100), (1000, 0, 100), (1000, 17, 100), (1000, 8, 0)])
def test_a_microphone_profile_out_of_range_is_refused(cutoff: int, bits: int, clip: int) -> None:
    with pytest.raises(ValueError):
        low_quality_mic(tone(), RATE, cutoff_hz=cutoff, bits=bits, clip_level=clip)


# ---- 3G jitter and loss ----------------------------------------------------------------------------------------------


def chunks(count: int = 200) -> list[AudioChunk]:
    return [AudioChunk(i, AudioCodec.PCM_S16LE, RATE, b"\x01\x00" * 320) for i in range(count)]


def test_the_network_is_deterministic_for_a_seed() -> None:
    profile = NetworkProfile(jitter_ms_max=80, loss_rate=0.1, frame_ms=20)
    assert simulate_network(chunks(), profile, random.Random(5)) == simulate_network(chunks(), profile, random.Random(5))


def test_a_perfect_network_delivers_everything_in_order_on_time() -> None:
    arrived = simulate_network(chunks(10), NetworkProfile(jitter_ms_max=0, loss_rate=0.0, frame_ms=20), random.Random(1))
    assert [a.chunk.sequence for a in arrived] == list(range(10))
    assert [a.arrival_ms for a in arrived] == [20 * i for i in range(10)]


def test_loss_drops_roughly_the_requested_share_and_marks_them_lost() -> None:
    arrived = simulate_network(chunks(2000), NetworkProfile(jitter_ms_max=0, loss_rate=0.2, frame_ms=20), random.Random(2))
    lost = [a for a in arrived if a.lost]
    assert len(arrived) == 2000
    assert 0.15 < len(lost) / 2000 < 0.25
    assert all(a.chunk.data == b"" for a in lost)  # a lost frame carries no audio
    assert {a.chunk.sequence for a in lost}.isdisjoint({a.chunk.sequence for a in arrived if not a.lost})


def test_jitter_can_reorder_arrivals_but_never_makes_them_earlier_than_sent() -> None:
    arrived = simulate_network(chunks(300), NetworkProfile(jitter_ms_max=200, loss_rate=0.0, frame_ms=20), random.Random(4))
    assert [a.chunk.sequence for a in arrived] != sorted(a.chunk.sequence for a in arrived)
    assert all(a.arrival_ms >= 20 * a.chunk.sequence for a in arrived)
    assert [a.arrival_ms for a in arrived] == sorted(a.arrival_ms for a in arrived)


@pytest.mark.parametrize("jitter,loss,frame", [(-1, 0.0, 20), (0, -0.1, 20), (0, 1.1, 20), (0, 0.0, 0), (0, math.nan, 20)])
def test_a_network_profile_out_of_range_is_refused(jitter: int, loss: float, frame: int) -> None:
    with pytest.raises(ValueError):
        NetworkProfile(jitter_ms_max=jitter, loss_rate=loss, frame_ms=frame)


# ---- the per-locale report -------------------------------------------------------------------------------------------


def result(name: str = "market-10db", utterances: int = 20, correct: int = 19, clarified: int = 1, acted_low: int = 0) -> ScenarioResult:
    return ScenarioResult(name, utterances, correct, clarified, acted_low)


def review(signed: bool = True) -> NativeReview | None:
    return NativeReview(reviewer_ref="tester-01", locale=Locale.PCM, approved=signed) if signed else None


def report(**over: object) -> SimulationReport:
    base: dict[str, object] = dict(locale=Locale.PCM, seed=20261106, results=(result(),), native_review=review())
    return SimulationReport(**{**base, **over})  # type: ignore[arg-type]


def test_a_report_with_a_native_review_and_no_low_confidence_action_is_ready() -> None:
    assert ready_for_field(report())


def test_a_report_without_a_native_review_is_not_ready() -> None:
    assert not ready_for_field(report(native_review=None))


def test_a_review_for_another_locale_or_not_approved_does_not_count() -> None:
    assert not ready_for_field(report(native_review=NativeReview("t", Locale.YO, True)))
    assert not ready_for_field(report(native_review=NativeReview("t", Locale.PCM, False)))


def test_acting_on_a_low_confidence_key_slot_even_once_blocks_the_report() -> None:
    assert not ready_for_field(report(results=(result(), result("street", acted_low=1))))


def test_a_report_with_no_scenarios_is_not_ready() -> None:
    assert not ready_for_field(report(results=()))


@pytest.mark.parametrize("args", [(0, 0, 0, 0), (5, 6, 0, 0), (5, 3, 3, 0), (5, 0, 0, 6), (-1, 0, 0, 0)])
def test_impossible_scenario_counts_are_refused(args: tuple[int, int, int, int]) -> None:
    with pytest.raises(ValueError):
        ScenarioResult("x", *args)


def test_the_report_json_is_stable_and_carries_no_audio_or_transcripts() -> None:
    first = report_json(report())
    assert first == report_json(report())
    parsed = json.loads(first)
    assert parsed["locale"] == "pcm" and parsed["seed"] == 20261106
    assert parsed["synthetic"] is True
    assert set(parsed["results"][0]) == {"scenario", "utterances", "key_slots_correct", "clarified", "acted_on_low_confidence"}
