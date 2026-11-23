"""The baseline-run entry point (VOI-16, REQ-VOI-001, REQ-VOI-004, REQ-NFR-003; rulings A332, A377 to A383, A451; AGENT-13, AGENT-16, INV-21).

`run_baseline` runs the manifest one session at a time through the adapters, writes the synthetic fixtures it generates to the private store (files 0600, analysis date as modification time), paces
the sessions, stops at the first quota refusal or after two failures in a row, and writes a counts-only report outside the store. `main` reads the key only from an owner-only file, loads the
settings and the manifest from files (every value required, none defaulted), and refuses before any connection is made when anything is wrong. Adapters, clock, sleep and the connect factory are
scripted stand-ins (the recorded AGENT-12 exception of noema#412); no network is involved. The store tests need POSIX file modes and run on Linux CI.
"""

import asyncio
import json
import os
import stat
from collections.abc import AsyncIterator, Mapping
from datetime import UTC, datetime
from pathlib import Path

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec, FinalTranscript, Locale, PartialTranscript, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_stt import Connect, Connection
from noema_model.providers.speech.tts import SynthesisRequest
from noema_model.voice.baseline import AudioStore, BaselineSettings, ManifestItem
from noema_model.voice.baseline_run import RunnerConfigError, load_manifest, load_settings, main, run_baseline
from noema_model.voice.bench import AudioOrigin
from tests.support import run
from tests.support_voice import CAPS

POSIX = pytest.mark.skipif(os.name == "nt", reason="file modes are POSIX")
KEY = "synthetic-synthetic-synthetic"
GUARD_S = 10.0

SETTINGS_FILE = {
    "stt_final_s": 20.0, "response_s": 1.0, "tts_first_s": 10.0, "session_s": 40.0, "read_s": 2.0, "close_s": 1.0, "hold_s": 5.0, "concurrency": 1, "min_pause_s": 2.0,
    "connections_per_minute": 3, "connections_per_session": 2, "budget_minutes": 20.0, "utterance_min_s": 5.0, "utterance_max_s": 15.0,
    "open_timeout_s": 10.0, "ping_interval_s": None, "ping_timeout_s": None, "max_message_bytes": 4194304, "tts_poll_s": 0.2,
}
WIRE = ("open_timeout_s", "ping_interval_s", "ping_timeout_s", "max_message_bytes", "tts_poll_s")


class Clock:
    def __init__(self) -> None:
        self.now = 0

    def __call__(self) -> int:
        return self.now


def wall() -> datetime:
    return datetime(2026, 11, 21, 12, 0, tzinfo=UTC)


class Stt:
    """Takes 600 ms after the last frame. `script` is a list of outcomes per call: a text, or a ProviderFailure to raise, or "hang"."""

    name = "fake-stt"
    capabilities = CAPS

    def __init__(self, clock: Clock, script: list[object]) -> None:
        self.clock, self.script, self.calls, self.frames_seen = clock, list(script), 0, 0

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        outcome = self.script[self.calls] if self.calls < len(self.script) else "send ten thousand naira"
        self.calls += 1
        async for chunk in audio:
            self.frames_seen += 1
            assert chunk.sample_rate_hz == 16000 and len(chunk.data) <= 32000  # A191: 16 kHz and at most 32,000 bytes a frame
        if outcome == "hang":
            await asyncio.Event().wait()
        if isinstance(outcome, ProviderFailure):
            raise ProviderError(outcome)
        self.clock.now += 600
        yield PartialTranscript(0, "x")
        text = str(outcome)
        yield FinalTranscript(text, tuple(TokenConfidence(w, None) for w in text.split()), None, Locale.PCM, None)


class Tts:
    name = "fake-tts"
    capabilities = CAPS

    def __init__(self, clock: Clock) -> None:
        self.clock, self.requests = clock, []  # type: ignore[var-annotated]

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        self.requests.append(request.text)
        self.clock.now += 500
        yield AudioChunk(0, AudioCodec.PCM_S16LE, 48000, bytes([0, 1]) * 48000 * 6)  # six seconds at 48 kHz


def settings(**over: object) -> BaselineSettings:
    fields = {k: v for k, v in SETTINGS_FILE.items() if k not in ("utterance_min_s", "utterance_max_s", *WIRE)}
    return BaselineSettings(**{**fields, **over})  # type: ignore[arg-type]


def synthetic(label: str, seconds: float = 6.0) -> ManifestItem:
    return ManifestItem(label=label, locale=Locale.PCM, origin=AudioOrigin.SYNTHETIC, speaker_ref="synthetic", audio_s=seconds, answer_s=2.0, script="send ten thousand naira")


def make_store(tmp_path: Path) -> tuple[Path, AudioStore]:
    root = tmp_path / "store"
    root.mkdir(mode=0o700)
    root.chmod(0o700)
    return root, AudioStore(root, wall)


async def fake_sleep(log: list[float], seconds: float) -> None:
    log.append(seconds)


def go(tmp_path: Path, items: list[ManifestItem], script: list[object], **over: object) -> tuple[dict[str, object], Stt, Tts, list[float], Path]:
    root, store = make_store(tmp_path) if os.name != "nt" else (tmp_path, AudioStore(tmp_path, wall))
    clock, pauses = Clock(), []  # type: ignore[var-annotated]
    stt, tts = Stt(clock, script), Tts(clock)

    async def call() -> dict[str, object]:
        return await run_baseline(items=items, stt=stt, tts=tts, settings=settings(**over), bounds_s=(5.0, 15.0), store=store, clock_ms=clock, wall=wall,
                                  sleep=lambda s: fake_sleep(pauses, s), tts_model_version="v1", answer_text="We found your transfer.", targets_ms=1800)

    report = run(asyncio.wait_for(call(), timeout=GUARD_S))
    return report, stt, tts, pauses, root


@POSIX
def test_a_synthetic_item_is_generated_stored_at_16_khz_with_the_modes_and_the_analysis_date_then_measured(tmp_path: Path) -> None:
    report, stt, tts, pauses, root = go(tmp_path, [synthetic("u1")], [])
    stored = root / "synthetic" / "u1.pcm"
    assert tts.requests[0] == "send ten thousand naira"  # the script is what is generated
    assert stored.stat().st_size == 6 * 32000  # six seconds at 48 kHz reduced to 16 kHz: 96,000 samples of 2 bytes
    assert stat.S_IMODE(stored.stat().st_mode) == 0o600 and stat.S_IMODE(stored.parent.stat().st_mode) == 0o700 and stored.stat().st_mtime == wall().timestamp()
    assert report["sessions"] == {"total": 1, "complete": 1, "incomplete": 0} and stt.calls == 1


def test_sessions_are_paced_between_each_other_and_not_after_the_last(tmp_path: Path) -> None:
    _, _, _, pauses, _ = go(tmp_path, [synthetic("u1"), synthetic("u2"), synthetic("u3")], [])
    assert pauses == [40.0, 40.0]  # two connections a session at three a minute: 60 * 2 / 3


def test_a_quota_refusal_ends_the_run_and_the_rest_of_the_manifest_is_not_sent(tmp_path: Path) -> None:
    report, stt, _, _, _ = go(tmp_path, [synthetic("u1"), synthetic("u2"), synthetic("u3")], ["send ten thousand naira", ProviderFailure.RATE_LIMITED])
    assert stt.calls == 2 and report["failures"] == {"RATE_LIMITED": 1} and report["sessions"] == {"total": 2, "complete": 1, "incomplete": 1}
    assert report["not_run"] == 1


def test_two_failures_in_a_row_end_the_run(tmp_path: Path) -> None:
    report, stt, _, _, _ = go(tmp_path, [synthetic(f"u{i}") for i in range(5)], [ProviderFailure.UNAVAILABLE, ProviderFailure.UNAVAILABLE])
    assert stt.calls == 2 and report["not_run"] == 3


def test_one_failure_between_successes_does_not_end_the_run(tmp_path: Path) -> None:
    report, stt, _, _, _ = go(tmp_path, [synthetic(f"u{i}") for i in range(4)], ["ok go", ProviderFailure.UNAVAILABLE, "ok go", "ok go"])
    assert stt.calls == 4 and report["not_run"] == 0 and report["failures"] == {"UNAVAILABLE": 1}


def test_a_session_that_outlasts_the_whole_session_deadline_is_an_incomplete_timeout_and_the_run_goes_on(tmp_path: Path) -> None:
    report, stt, _, _, _ = go(tmp_path, [synthetic("u1"), synthetic("u2")], ["hang", "send ten thousand naira"], session_s=0.3, stt_final_s=5.0)
    assert report["failures"] == {"TIMEOUT": 1} and stt.calls == 2


def test_a_manifest_that_does_not_fit_the_budget_is_trimmed_from_its_end_before_anything_is_sent(tmp_path: Path) -> None:
    report, stt, tts, _, _ = go(tmp_path, [synthetic(f"u{i}", seconds=10.0) for i in range(10)], [], budget_minutes=2.0)  # 24 s each: five fit
    assert stt.calls == 5 and report["dropped_from_manifest"] == 5 and len(tts.requests) == 10  # five fixtures generated and five answers spoken


@POSIX
def test_a_team_recording_is_read_from_the_store_and_its_accuracy_is_counted_without_keeping_the_transcript(tmp_path: Path) -> None:
    root, store = make_store(tmp_path)
    store.write("spk-01", "r1.pcm", b"\x10\x00" * 16000 * 6, wall())  # six seconds of 16 kHz audio
    item = ManifestItem(label="r1", locale=Locale.PCM, origin=AudioOrigin.TEAM_RECORDING, speaker_ref="spk-01", audio_s=6.0, answer_s=2.0, script="send ten thousand naira", file="r1.pcm")
    clock, pauses = Clock(), []  # type: ignore[var-annotated]
    stt, tts = Stt(clock, ["send ten naira"]), Tts(clock)
    report = run(asyncio.wait_for(run_baseline(items=[item], stt=stt, tts=tts, settings=settings(), bounds_s=(5.0, 15.0), store=store, clock_ms=clock, wall=wall,
                                               sleep=lambda s: fake_sleep(pauses, s), tts_model_version="v1", answer_text="We found your transfer.", targets_ms=1800), timeout=GUARD_S))
    assert report["accuracy"] == {"team_recordings": 1, "mean_word_error_rate": 0.25} and tts.requests == ["We found your transfer."]  # nothing generated for a recording
    assert "naira" not in json.dumps(report) and report["origins"] == {"TEAM_RECORDING": 1}


# ---- the files and main -----------------------------------------------------------------------------------------------------------------------------------------------


def write_json(path: Path, value: object) -> Path:
    path.write_text(json.dumps(value))
    return path


def test_the_settings_file_must_hold_every_value_and_nothing_else_and_of_the_right_type(tmp_path: Path) -> None:
    good = write_json(tmp_path / "s.json", SETTINGS_FILE)
    loaded, bounds, wire = load_settings(good)
    assert loaded.stt_final_s == 20.0 and loaded.concurrency == 1 and bounds == (5.0, 15.0)
    assert wire.open_timeout_s == 10.0 and wire.ping_interval_s is None and wire.max_message_bytes == 4194304 and wire.tts_poll_s == 0.2
    for field in SETTINGS_FILE:
        partial = {k: v for k, v in SETTINGS_FILE.items() if k != field}
        with pytest.raises(RunnerConfigError):
            load_settings(write_json(tmp_path / "p.json", partial))
    with pytest.raises(RunnerConfigError):
        load_settings(write_json(tmp_path / "x.json", {**SETTINGS_FILE, "surprise": 1}))
    with pytest.raises(RunnerConfigError):
        load_settings(write_json(tmp_path / "t.json", {**SETTINGS_FILE, "stt_final_s": "20"}))
    with pytest.raises(RunnerConfigError):
        load_settings(tmp_path / "absent.json")
    (tmp_path / "bad.json").write_text("{not json")
    with pytest.raises(RunnerConfigError):
        load_settings(tmp_path / "bad.json")
    with pytest.raises(RunnerConfigError):
        load_settings(write_json(tmp_path / "n.json", {**SETTINGS_FILE, "concurrency": 2}))


def test_the_manifest_is_validated_item_by_item(tmp_path: Path) -> None:
    item = {"label": "u1", "locale": "pcm", "origin": "SYNTHETIC", "speaker_ref": "synthetic", "audio_s": 6.0, "answer_s": 2.0, "script": "send ten"}
    assert [i.label for i in load_manifest(write_json(tmp_path / "m.json", [item]))] == ["u1"]
    for bad in ({**item, "origin": "REAL"}, {**item, "locale": "xx"}, {**item, "audio_s": -1}, {k: v for k, v in item.items() if k != "script"},
                {**item, "origin": "TEAM_RECORDING"}, {**item, "extra": 1}):
        with pytest.raises(RunnerConfigError):
            load_manifest(write_json(tmp_path / "b.json", [bad]))
    with pytest.raises(RunnerConfigError):
        load_manifest(write_json(tmp_path / "e.json", []))
    with pytest.raises(RunnerConfigError):
        load_manifest(write_json(tmp_path / "d.json", [item, item]))  # labels are unique


def args(tmp_path: Path, **over: str) -> list[str]:
    base = {"settings": str(write_json(tmp_path / "settings.json", SETTINGS_FILE)), "manifest": str(write_json(tmp_path / "manifest.json", [{"label": "u1", "locale": "pcm", "origin": "SYNTHETIC", "speaker_ref": "synthetic", "audio_s": 6.0, "answer_s": 2.0, "script": "send ten"}])),
            "store": str(tmp_path / "store"), "report": str(tmp_path / "report.json"), "base-url": "wss://example.invalid", "voices": str(write_json(tmp_path / "voices.json", {"pcm": {"language": "pcm", "accent": "pidgin", "gender": "female"}})),
            "tts-model-version": "v1", "answer-text": "We found your transfer.", "target-ms": "1800", **over}
    return [f"--{k}={v}" for k, v in base.items()]


class Spy:
    """A connect factory that records that it was asked for a connector, and refuses to connect: the refusals under test must happen before any connection."""

    def __init__(self) -> None:
        self.built = 0

    def __call__(self, **_: object) -> Connect:
        self.built += 1

        async def connect(url: str, headers: Mapping[str, str]) -> Connection:
            raise AssertionError("no connection may be made")

        return connect


def run_main(argv: list[str], environ: dict[str, str], spy: Spy, capsys: pytest.CaptureFixture[str]) -> tuple[int, str]:
    code = main(argv, environ, make_connector=spy, wall=wall, clock_ms=Clock(), sleep=lambda s: fake_sleep([], s))
    out = capsys.readouterr()
    return code, out.out + out.err


def test_without_the_key_file_variable_the_runner_refuses_before_any_connection(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    code, text = run_main(args(tmp_path), {}, Spy(), capsys)
    assert code == 2 and "key" in text.lower() and KEY not in text


@POSIX
def test_a_key_file_that_is_not_owner_only_is_refused_and_the_key_is_never_printed(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    key = tmp_path / "key"
    key.write_text(KEY)
    key.chmod(0o644)
    spy = Spy()
    code, text = run_main(args(tmp_path), {"INTRON_API_KEY_FILE": str(key)}, spy, capsys)
    assert code == 2 and KEY not in text and str(key) not in text


@POSIX
@pytest.mark.parametrize("missing", ["settings", "manifest", "store", "report", "base-url", "voices", "tts-model-version", "answer-text", "target-ms"])
def test_every_argument_is_required_and_has_no_default(tmp_path: Path, capsys: pytest.CaptureFixture[str], missing: str) -> None:
    key = tmp_path / "key"
    key.write_text(KEY)
    key.chmod(0o600)
    argv = [a for a in args(tmp_path) if not a.startswith(f"--{missing}=")]
    code, _ = run_main(argv, {"INTRON_API_KEY_FILE": str(key)}, Spy(), capsys)
    assert code == 2


@POSIX
def test_a_report_path_inside_the_store_is_refused_so_the_report_can_never_sit_among_raw_audio(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    key = tmp_path / "key"
    key.write_text(KEY)
    key.chmod(0o600)
    (tmp_path / "store").mkdir(mode=0o700)
    (tmp_path / "store").chmod(0o700)
    spy = Spy()
    code, _ = run_main(args(tmp_path, report=str(tmp_path / "store" / "report.json")), {"INTRON_API_KEY_FILE": str(key)}, spy, capsys)
    assert code == 2 and spy.built == 0


def test_the_shipped_settings_file_loads_and_holds_the_values_of_a380() -> None:
    path = Path(__file__).resolve().parents[1] / "bench" / "baseline-settings.json"
    loaded, bounds, wire = load_settings(path)
    assert (loaded.stt_final_s, loaded.response_s, loaded.tts_first_s, loaded.session_s) == (20.0, 1.0, 10.0, 40.0)
    assert (loaded.read_s, loaded.close_s, loaded.hold_s, loaded.concurrency, loaded.min_pause_s, loaded.budget_minutes) == (2.0, 1.0, 5.0, 1, 2.0, 20.0)
    assert bounds == (5.0, 15.0) and (loaded.connections_per_minute, loaded.connections_per_session) == (3, 2) and wire.ping_interval_s is None


# ---- an item whose audio is outside the bounds is skipped and counted, never sent, and the run goes on ---------------------------------------------------------------------------


class ShortThenLongTts(Tts):
    """The first generation is 3 seconds (below the bounds), the rest 6 seconds."""

    def __init__(self, clock: Clock) -> None:
        super().__init__(clock)
        self.generations = 0

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        if request.text == "send ten thousand naira":
            self.generations += 1
            seconds = 3 if self.generations == 1 else 6
            self.requests.append(request.text)
            yield AudioChunk(0, AudioCodec.PCM_S16LE, 48000, bytes([0, 1]) * 48000 * seconds)
        else:
            async for chunk in super().synthesize(request):
                yield chunk


@POSIX
def test_an_item_whose_generated_audio_is_outside_the_bounds_is_skipped_and_counted_and_the_next_item_still_runs(tmp_path: Path) -> None:
    root, store = make_store(tmp_path)
    clock, pauses = Clock(), []  # type: ignore[var-annotated]
    stt, tts = Stt(clock, []), ShortThenLongTts(clock)
    report = run(asyncio.wait_for(run_baseline(items=[synthetic("u1"), synthetic("u2")], stt=stt, tts=tts, settings=settings(), bounds_s=(5.0, 15.0), store=store, clock_ms=clock, wall=wall,
                                               sleep=lambda s: fake_sleep(pauses, s), tts_model_version="v1", answer_text="We found your transfer.", targets_ms=1800), timeout=GUARD_S))
    assert report["skipped_out_of_bounds"] == 1 and stt.calls == 1 and report["sessions"] == {"total": 1, "complete": 1, "incomplete": 0}


# ---- --check-key (A487): one connection, no audio, prints only accepted, rejected or inconclusive ---------------------------------------------------------------------------------


class Greeter:
    """A scripted connection: replies with `reply` on the first receive. Records what was sent (it must be nothing) and that it was closed."""

    def __init__(self, reply: str | None) -> None:
        self.reply, self.sent, self.closed = reply, [], False  # type: ignore[var-annotated]

    async def send(self, message: str) -> None:
        self.sent.append(message)

    async def recv(self) -> str:
        if self.reply is None:
            raise ConnectionError("dropped")
        return self.reply

    async def close(self) -> None:
        self.closed = True


def check_key(tmp_path: Path, reply: str | None, capsys: pytest.CaptureFixture[str], environ: dict[str, str] | None = None) -> tuple[int, str, Greeter, list[tuple[str, dict[str, str]]]]:
    key = tmp_path / "key"
    key.write_text(KEY)
    key.chmod(0o600)
    greeter, seen = Greeter(reply), []  # type: ignore[var-annotated]

    def make(**_: object) -> Connect:
        async def connect(url: str, headers: Mapping[str, str]) -> Connection:
            seen.append((url, dict(headers)))
            return greeter  # type: ignore[return-value]

        return connect

    argv = ["--check-key", f"--settings={write_json(tmp_path / 'settings.json', SETTINGS_FILE)}", "--base-url=wss://example.invalid"]
    code = main(argv, environ if environ is not None else {"INTRON_API_KEY_FILE": str(key)}, make_connector=make, wall=wall, clock_ms=Clock(), sleep=lambda s: fake_sleep([], s))
    out = capsys.readouterr()
    return code, out.out + out.err, greeter, seen


def test_a_key_the_vendor_accepts_prints_accepted_sends_no_audio_and_leaves_no_key_in_the_output(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    code, text, greeter, seen = check_key(tmp_path, json.dumps({"message_type": "SESSION_CREATED", "session_id": "s", "credit_balance": 10}), capsys)
    assert code == 0 and text.strip() == "accepted" and greeter.sent == [] and greeter.closed and KEY not in text
    assert seen[0][1] == {"Authorization": f"Bearer {KEY}"} and seen[0][0].startswith("wss://example.invalid/")


def test_a_key_the_vendor_refuses_prints_rejected(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    code, text, greeter, _ = check_key(tmp_path, json.dumps({"message_type": "AUTHENTICATION_ERROR", "message": "permission denied"}), capsys)
    assert code == 1 and text.strip() == "rejected" and greeter.sent == [] and greeter.closed and KEY not in text


@pytest.mark.parametrize("reply", [None, "not json", json.dumps({"message_type": "QUOTA_EXCEEDED"}), json.dumps({"message_type": "SOMETHING_NEW"})])
def test_anything_else_is_inconclusive_never_rejected(tmp_path: Path, capsys: pytest.CaptureFixture[str], reply: str | None) -> None:
    code, text, greeter, _ = check_key(tmp_path, reply, capsys)
    assert code == 1 and text.strip() == "inconclusive" and greeter.sent == [] and KEY not in text


def test_check_key_without_the_key_file_refuses_before_any_connection(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    code, text, greeter, seen = check_key(tmp_path, "{}", capsys, environ={})
    assert code == 2 and seen == [] and greeter.sent == [] and KEY not in text


# ---- the proposed manifest and voices files -------------------------------------------------------------------------------------------------------------------------------------


def test_the_proposed_manifest_loads_fits_the_budget_and_covers_the_five_locales() -> None:
    from noema_model.voice.baseline import plan_budget
    from noema_model.voice.baseline_run import _load_voices

    bench = Path(__file__).resolve().parents[1] / "bench"
    items = load_manifest(bench / "manifest.proposed.json")
    voices = _load_voices(bench / "voices.proposed.json")
    assert {i.locale for i in items} == {Locale.EN_NG, Locale.PCM, Locale.YO, Locale.HA, Locale.IG} and set(voices) == {i.locale for i in items}
    assert all(i.origin is AudioOrigin.SYNTHETIC and i.script for i in items)  # proposed: synthetic only, no team recording is named here
    assert 5.0 <= min(i.audio_s for i in items) and max(i.audio_s for i in items) <= 15.0
    plan = plan_budget(items, budget_minutes=20.0)
    assert plan.dropped == () and plan.total_s <= 20 * 60 * 0.6  # leaves room: at most 60 percent of the 20 audio-minutes, the rest for recordings and reruns
    assert voices[Locale.PCM].language == "pcm" and voices[Locale.PCM].accent == "pidgin" and voices[Locale.EN_NG].language == "en" and voices[Locale.EN_NG].accent == "yoruba"
    for locale in (Locale.YO, Locale.HA, Locale.IG):
        assert voices[locale].language == locale.value


@pytest.mark.parametrize(("failure", "word"), [(ProviderFailure.REJECTED, "rejected"), (ProviderFailure.UNAVAILABLE, "inconclusive"), (ProviderFailure.RATE_LIMITED, "inconclusive")])
def test_a_refusal_at_the_handshake_is_rejected_only_when_the_vendor_refused_the_key(tmp_path: Path, capsys: pytest.CaptureFixture[str], failure: ProviderFailure, word: str) -> None:
    key = tmp_path / "key"
    key.write_text(KEY)
    key.chmod(0o600)

    def make(**_: object) -> Connect:
        async def connect(url: str, headers: Mapping[str, str]) -> Connection:
            raise ProviderError(failure)

        return connect

    argv = ["--check-key", f"--settings={write_json(tmp_path / 'settings.json', SETTINGS_FILE)}", "--base-url=wss://example.invalid"]
    code = main(argv, {"INTRON_API_KEY_FILE": str(key)}, make_connector=make, wall=wall, clock_ms=Clock(), sleep=lambda s: fake_sleep([], s))
    out = capsys.readouterr()
    assert out.out.strip() == word and code == (1) and KEY not in out.out + out.err
