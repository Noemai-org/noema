"""The baseline-run library (VOI-16, REQ-VOI-001, REQ-VOI-004, REQ-NFR-003; rulings A377 to A383 and A451, AGENT-13, AGENT-16, INV-21).

What the live Intron baseline run is made of, with no network and no key: the writer of the private raw-audio store (files 0600, folders 0700, each file's modification time set to the analysis date,
nothing written outside the store), the settings (every one required, no default), the audio budget printed before any call and trimmed from the end of the manifest, the pause between sessions
computed from the vendor's published connections-per-minute limit, the resampler for synthetic fixtures (text-to-speech answers at 48 kHz, speech-to-text takes 16 kHz), the word error rate counted
inside the stand-in core so that no transcript is ever kept, and the report (counts, stages and failures; never audio, transcript text or a key). Providers and the clock are scripted stand-ins
(the recorded AGENT-12 exception of noema#412); the store tests need POSIX file modes and are skipped elsewhere (Linux CI runs them).
"""

import asyncio
import json
import os
import stat
import struct
from collections.abc import AsyncIterator
from datetime import UTC, datetime
from pathlib import Path
from typing import cast

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec, FinalTranscript, Locale, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.tts import SynthesisRequest
from noema_model.voice.baseline import (
    AudioStore,
    BaselineSettings,
    ManifestItem,
    StoreError,
    build_report,
    downsample_pcm16,
    pause_s,
    plan_budget,
    word_error_rate,
)
from noema_model.voice.bench import AudioBudget, AudioOrigin, Utterance, measure_session
from tests.support import run
from tests.support_voice import CAPS, REQUEST

POSIX = pytest.mark.skipif(os.name == "nt", reason="file modes are POSIX")
ANALYSED = datetime(2026, 11, 20, 9, 30, tzinfo=UTC)

SETTINGS = dict(stt_final_s=20.0, response_s=1.0, tts_first_s=10.0, session_s=40.0, read_s=2.0, close_s=1.0, hold_s=5.0, concurrency=1, min_pause_s=2.0,
                connections_per_minute=3, connections_per_session=2, budget_minutes=20.0)


def settings(**override: object) -> BaselineSettings:
    return BaselineSettings(**{**SETTINGS, **override})  # type: ignore[arg-type]


# ---- the store writer -------------------------------------------------------------------------------------------------------------------------------------------------


def make_store(tmp_path: Path) -> Path:
    root = tmp_path / "store"
    root.mkdir(mode=0o700)
    root.chmod(0o700)
    return root


@POSIX
def test_a_recording_is_written_under_its_speaker_folder_with_modes_0700_and_0600_and_the_analysis_date_as_its_modification_time(tmp_path: Path) -> None:
    root = make_store(tmp_path)
    AudioStore(root).write("spk-01", "u1.pcm", b"\x00\x01" * 10, ANALYSED)
    folder, file = root / "spk-01", root / "spk-01" / "u1.pcm"
    assert stat.S_IMODE(folder.stat().st_mode) == 0o700 and stat.S_IMODE(file.stat().st_mode) == 0o600
    assert file.read_bytes() == b"\x00\x01" * 10 and file.stat().st_mtime == ANALYSED.timestamp()


@POSIX
def test_the_modes_do_not_depend_on_the_callers_umask(tmp_path: Path) -> None:
    root = make_store(tmp_path)
    old = os.umask(0o000)
    try:
        AudioStore(root).write("spk-02", "u1.pcm", b"abcd", ANALYSED)
    finally:
        os.umask(old)
    assert stat.S_IMODE((root / "spk-02").stat().st_mode) == 0o700 and stat.S_IMODE((root / "spk-02" / "u1.pcm").stat().st_mode) == 0o600


@POSIX
@pytest.mark.parametrize("mode", [0o750, 0o755, 0o770, 0o777, 0o500])
def test_a_store_that_is_not_exactly_mode_0700_is_refused(tmp_path: Path, mode: int) -> None:
    root = tmp_path / "store"
    root.mkdir()
    root.chmod(mode)
    try:
        with pytest.raises(StoreError):
            AudioStore(root)
    finally:
        root.chmod(0o700)


def test_a_store_that_does_not_exist_or_is_a_file_is_refused(tmp_path: Path) -> None:
    (tmp_path / "file").write_text("x")
    for target in (tmp_path / "absent", tmp_path / "file"):
        with pytest.raises(StoreError):
            AudioStore(target)


@POSIX
def test_a_store_that_is_a_symbolic_link_is_refused(tmp_path: Path) -> None:
    real = make_store(tmp_path)
    link = tmp_path / "link"
    link.symlink_to(real)
    with pytest.raises(StoreError):
        AudioStore(link)


@pytest.mark.parametrize("ref", ["", ".", "..", "a/b", "../x", "a b", "naira\n", "x" * 65, "é", "/abs"])
def test_a_speaker_reference_that_is_not_a_plain_name_is_refused_and_nothing_is_written(tmp_path: Path, ref: str) -> None:
    root = make_store(tmp_path) if os.name != "nt" else tmp_path
    with pytest.raises(StoreError):
        AudioStore(root).write(ref, "u1.pcm", b"x", ANALYSED)
    assert [p for p in root.rglob("*") if p.is_file()] == []


@pytest.mark.parametrize("name", ["", ".", "..", "a/b", "../x", ".hidden/..", "a b", "x" * 129])
def test_a_file_name_that_is_not_a_plain_name_is_refused(tmp_path: Path, name: str) -> None:
    root = make_store(tmp_path) if os.name != "nt" else tmp_path
    with pytest.raises(StoreError):
        AudioStore(root).write("spk-01", name, b"x", ANALYSED)


@POSIX
def test_an_existing_file_is_never_overwritten(tmp_path: Path) -> None:
    root = make_store(tmp_path)
    store = AudioStore(root)
    store.write("spk-01", "u1.pcm", b"first", ANALYSED)
    with pytest.raises(StoreError):
        store.write("spk-01", "u1.pcm", b"second", ANALYSED)
    assert (root / "spk-01" / "u1.pcm").read_bytes() == b"first"


@POSIX
def test_a_symbolic_link_in_place_of_a_speaker_folder_is_not_followed(tmp_path: Path) -> None:
    root = make_store(tmp_path)
    outside = tmp_path / "outside"
    outside.mkdir()
    (root / "spk-03").symlink_to(outside)
    with pytest.raises(StoreError):
        AudioStore(root).write("spk-03", "u1.pcm", b"x", ANALYSED)
    assert list(outside.iterdir()) == []  # nothing went outside the store


def test_an_analysis_date_without_a_timezone_is_refused(tmp_path: Path) -> None:
    root = make_store(tmp_path) if os.name != "nt" else tmp_path
    with pytest.raises(StoreError):
        AudioStore(root).write("spk-01", "u1.pcm", b"x", datetime(2026, 11, 20, 9, 30))


# ---- the settings, the pause and the budget -------------------------------------------------------------------------------------------------------------------------------


def test_every_setting_is_required() -> None:
    for field in SETTINGS:
        partial = {k: v for k, v in SETTINGS.items() if k != field}
        with pytest.raises(TypeError):
            BaselineSettings(**partial)  # type: ignore[arg-type]


@pytest.mark.parametrize("field", ["stt_final_s", "response_s", "tts_first_s", "session_s", "read_s", "close_s", "hold_s", "min_pause_s", "budget_minutes"])
@pytest.mark.parametrize("bad", [0.0, -1.0, float("nan"), float("inf"), True])
def test_a_bound_that_is_not_positive_and_finite_is_refused(field: str, bad: float) -> None:
    with pytest.raises(ValueError):
        settings(**{field: bad})


@pytest.mark.parametrize("field", ["connections_per_minute", "connections_per_session"])
@pytest.mark.parametrize("bad", [0, -1, 1.5, True])
def test_a_count_that_is_not_a_positive_whole_number_is_refused(field: str, bad: object) -> None:
    with pytest.raises(ValueError):
        settings(**{field: bad})


@pytest.mark.parametrize("bad", [0, 2, 3, -1])
def test_sessions_run_one_at_a_time(bad: int) -> None:
    with pytest.raises(ValueError):
        settings(concurrency=bad)


def test_the_pause_is_the_larger_of_the_minimum_and_what_the_published_connection_limit_allows() -> None:
    assert pause_s(settings()) == 40.0  # two connections a session at three a minute: 60 * 2 / 3
    assert pause_s(settings(connections_per_minute=120)) == 2.0  # the limit would allow a faster pace than the minimum
    assert pause_s(settings(connections_per_minute=60, connections_per_session=1, min_pause_s=5.0)) == 5.0


def item(label: str, audio_s: float = 10.0, answer_s: float = 4.0, origin: AudioOrigin = AudioOrigin.SYNTHETIC) -> ManifestItem:
    return ManifestItem(label=label, locale=Locale.PCM, origin=origin, speaker_ref="synthetic" if origin is AudioOrigin.SYNTHETIC else "spk-01", audio_s=audio_s, answer_s=answer_s,
                        script="send ten thousand naira" if origin is AudioOrigin.TEAM_RECORDING else None)


def test_the_budget_counts_generation_speech_to_text_and_spoken_answers_and_synthetic_audio_costs_all_three() -> None:
    plan = plan_budget([item("a"), item("b", origin=AudioOrigin.TEAM_RECORDING)], budget_minutes=20.0)
    assert plan.generation_s == 10.0  # only the synthetic one is generated
    assert plan.stt_s == 20.0 and plan.spoken_s == 8.0 and plan.total_s == 38.0
    assert [i.label for i in plan.kept] == ["a", "b"] and plan.dropped == ()


def test_a_manifest_that_does_not_fit_is_trimmed_from_its_end_and_no_utterance_is_shortened() -> None:
    items = [item(f"u{i}") for i in range(20)]  # 24 s each: 480 s in all
    plan = plan_budget(items, budget_minutes=2.0)  # 120 s
    assert [i.label for i in plan.kept] == [f"u{i}" for i in range(5)] and len(plan.dropped) == 15
    assert plan.total_s == 120.0 and all(i.audio_s == 10.0 for i in plan.kept)  # five of them fit exactly; none was cut short
    assert plan.kept + plan.dropped == tuple(items)


def test_an_item_that_alone_exceeds_the_budget_is_dropped_with_everything_after_it() -> None:
    plan = plan_budget([item("a"), item("huge", audio_s=700.0), item("c")], budget_minutes=20.0)
    assert [i.label for i in plan.kept] == ["a"] and [i.label for i in plan.dropped] == ["huge", "c"]  # the manifest is trimmed from the end, never reordered to fit


def test_an_empty_manifest_or_a_bad_item_is_refused() -> None:
    with pytest.raises(ValueError):
        plan_budget([], budget_minutes=20.0)
    for bad in (0.0, -1.0, float("nan")):
        with pytest.raises(ValueError):
            item("x", audio_s=bad)
    with pytest.raises(ValueError):
        ManifestItem(label="x", locale=Locale.PCM, origin=AudioOrigin.TEAM_RECORDING, speaker_ref="spk-01", audio_s=5.0, answer_s=1.0, script=None)  # a recording needs its script to be counted


# ---- the resampler and the word error rate -----------------------------------------------------------------------------------------------------------------------------


def pcm(samples: list[int]) -> bytes:
    return struct.pack(f"<{len(samples)}h", *samples)


def test_downsampling_48_to_16_khz_averages_each_group_of_three_samples() -> None:
    assert downsample_pcm16(pcm([3, 3, 3, 6, 9, 12]), 48000, 16000) == pcm([3, 9])
    assert downsample_pcm16(pcm([32767, 32767, 32767]), 48000, 16000) == pcm([32767])  # no overflow
    assert downsample_pcm16(pcm([-32768, -32768, -32768]), 48000, 16000) == pcm([-32768])
    assert downsample_pcm16(pcm([1, 2, 3, 4]), 48000, 16000) == pcm([2])  # a partial group at the end is dropped, not padded


@pytest.mark.parametrize("src,dst", [(44100, 16000), (16000, 48000), (48000, 0), (0, 16000), (47999, 16000)])
def test_only_a_whole_number_ratio_downwards_is_supported(src: int, dst: int) -> None:
    with pytest.raises(ValueError):
        downsample_pcm16(pcm([1, 2, 3]), src, dst)


def test_the_same_rate_is_returned_unchanged_and_odd_byte_counts_are_refused() -> None:
    assert downsample_pcm16(pcm([1, 2, 3]), 16000, 16000) == pcm([1, 2, 3])
    with pytest.raises(ValueError):
        downsample_pcm16(b"\x00\x01\x02", 48000, 16000)


@pytest.mark.parametrize("reference,hypothesis,rate", [
    ("send ten thousand naira", "send ten thousand naira", 0.0),
    ("Send ten thousand naira.", "send TEN thousand, naira", 0.0),  # case and punctuation are not errors
    ("send ten thousand naira", "send ten naira", 0.25),  # one deletion in four words
    ("send ten thousand naira", "send ten thousand dollars", 0.25),  # one substitution
    ("send ten", "please send ten now", 1.0),  # two insertions in two words
    ("send ten", "", 1.0),
])
def test_the_word_error_rate_is_the_word_edit_distance_over_the_reference_length(reference: str, hypothesis: str, rate: float) -> None:
    assert word_error_rate(reference, hypothesis) == rate


def test_a_reference_with_no_words_is_refused() -> None:
    with pytest.raises(ValueError):
        word_error_rate("  ...  ", "anything")


# ---- the report ---------------------------------------------------------------------------------------------------------------------------------------------------------------


class Clock:
    def __init__(self) -> None:
        self.now = 0

    def __call__(self) -> int:
        return self.now


class Stt:
    name = "fake-stt"
    capabilities = CAPS

    def __init__(self, clock: Clock, text: str = "send ten thousand naira", fail: bool = False) -> None:
        self.clock, self.text, self.fail = clock, text, fail

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        async for _ in audio:
            pass
        if self.fail:
            raise ProviderError(ProviderFailure.RATE_LIMITED)
        self.clock.now += 600
        yield FinalTranscript(self.text, tuple(TokenConfidence(w, None) for w in self.text.split()), None, Locale.PCM, None)


class Tts:
    name = "fake-tts"
    capabilities = CAPS

    def __init__(self, clock: Clock) -> None:
        self.clock = clock

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        self.clock.now += 500
        yield AudioChunk(0, AudioCodec.PCM_S16LE, 48000, b"\x00\x01" * 100)


class StandIn:
    """The report is built from sessions that ran through the real harness; the core stand-in keeps only the word error rate."""

    def __init__(self, clock: Clock, reference: str | None) -> None:
        self.clock, self.reference = clock, reference
        self.wer: float | None = None

    async def admit(self, text: str) -> None:
        self.wer = None if self.reference is None else word_error_rate(self.reference, text)
        self.clock.now += 50

    async def answer(self) -> str:
        self.clock.now += 100
        return "We found your transfer."


def frames(seconds: float) -> tuple[AudioChunk, ...]:
    return tuple(AudioChunk(i, AudioCodec.PCM_S16LE, 16000, b"\x10\x00" * 8000) for i in range(round(seconds * 2)))


def one_session(origin: AudioOrigin, text: str, fail: bool = False) -> tuple[object, float | None]:
    clock = Clock()
    core = StandIn(clock, "send ten thousand naira" if origin is AudioOrigin.TEAM_RECORDING else None)
    result = run(asyncio.wait_for(measure_session(stt=Stt(clock, text, fail), tts=Tts(clock), core=core, utterance=Utterance("L", Locale.PCM, frames(8.0), origin), stt_request=REQUEST,
                                                  tts_model_version="v", clock_ms=clock, deadline_s=1.0, core_deadline_s=1.0, tts_deadline_s=1.0, cleanup_s=0.2, bounds_s=(5.0, 15.0),
                                                  budget=AudioBudget(total_s=600.0)), timeout=8.0))
    return result, core.wer


def test_the_report_has_counts_stages_failures_budget_and_accuracy_and_nothing_that_is_audio_text_or_a_key() -> None:
    results = [one_session(AudioOrigin.SYNTHETIC, "send ten thousand naira")[0], one_session(AudioOrigin.SYNTHETIC, "", fail=True)[0]]
    second, wer = one_session(AudioOrigin.TEAM_RECORDING, "send ten naira")
    results.append(second)
    report = build_report(results, wers=[0.25 if wer is None else wer], target_ms=1800, spent={"generation_s": 20.0, "stt_s": 24.0, "spoken_s": 8.0}, budget_s=1200.0, dropped=2)  # type: ignore[arg-type]
    text = json.dumps(report)
    assert report["sessions"] == {"total": 3, "complete": 2, "incomplete": 1} and report["failures"] == {"RATE_LIMITED": 1}
    assert report["origins"] == {"SYNTHETIC": 2, "TEAM_RECORDING": 1} and report["dropped_from_manifest"] == 2
    assert report["budget"] == {"generation_s": 20.0, "stt_s": 24.0, "spoken_s": 8.0, "total_s": 52.0, "of_s": 1200.0}
    assert report["accuracy"] == {"team_recordings": 1, "mean_word_error_rate": 0.25}
    assert report["target_ms"] == 1800 and "verdict" not in text.lower() and "pass" not in text.lower()
    assert "naira" not in text and "send" not in text  # no transcript text
    stages = cast(dict[str, dict[str, int | None]], report["stages_ms"])
    assert set(stages) == {"stt", "response", "tts"} and all(set(v) == {"p50", "p95"} for v in stages.values())


def test_a_report_of_no_sessions_is_refused() -> None:
    with pytest.raises(ValueError):
        build_report([], wers=[], target_ms=1800, spent={"generation_s": 0.0, "stt_s": 0.0, "spoken_s": 0.0}, budget_s=1200.0, dropped=0)
