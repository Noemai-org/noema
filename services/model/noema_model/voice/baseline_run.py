"""The baseline-run entry point (VOI-16, REQ-VOI-001, REQ-VOI-004, REQ-NFR-003; rulings A332, A377 to A383, A451; AGENT-13, AGENT-16, INV-21).

`run_baseline` runs a manifest one session at a time through the speech-to-text and text-to-speech adapters (they are arguments), writes the synthetic fixtures it generates to the private
raw-audio store, paces the sessions, stops at the first quota refusal or after two failures in a row, and returns a counts-only report. `main` is the command line: it reads the key ONLY from an
owner-only file (`INTRON_API_KEY_FILE`, the reader of noema#689), loads the settings and the manifest from files in which every value is required and none is defaulted, refuses before any
connection is made when anything is wrong, and writes the report as an owner-only file outside the store. The key, the audio, the transcripts and the paths of the store never appear in its output.

Run it as `python -m noema_model.voice.baseline_run` with the arguments of `main`; the Founder starts it (noema#767) once the key file is in place.
"""

import argparse
import asyncio
import ipaddress
import json
import math
import os
import re
import sys
from collections.abc import Awaitable, Callable, Mapping, Sequence
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from noema_model.platform.clock import SystemClock
from noema_model.providers.speech.base import AudioChunk, AudioCodec, Locale, PipelineMode, ProviderCapabilities, SpeechProvider, StreamRequest, Support
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_connect import websocket_connector
from noema_model.providers.speech.intron_key import KeyFileError, key_reader
from noema_model.providers.speech.intron_stt import Connect, IntronSpeechToText
from noema_model.providers.speech.intron_wire import stt_url
from noema_model.providers.speech.intron_tts import IntronTextToSpeech, Voice
from noema_model.providers.speech.tts import SpeechSynthesizer, SynthesisRequest
from noema_model.voice.baseline import AudioStore, BaselineSettings, ManifestItem, StoreError, build_report, downsample_pcm16, pause_s, plan_budget, word_error_rate
from noema_model.voice.bench import AudioBudget, AudioOrigin, BenchError, SessionResult, Utterance, measure_session
from noema_model.voice.latency import Stage, StageTimeline

_STT_RATE_HZ = 16000
_FRAME_BYTES = 3200  # 100 ms of 16 kHz PCM16 mono: well under the 32,000-byte ceiling of A191
_STOP_AFTER_FAILURES = 2  # the plan: two failures in a row end the run


class RunnerConfigError(Exception):
    """A settings, manifest or argument problem. The message is fixed text and never carries a value, a path or content."""


@dataclass(frozen=True, slots=True)
class WireSettings:
    open_timeout_s: float
    ping_interval_s: float | None
    ping_timeout_s: float | None
    max_message_bytes: int
    tts_poll_s: float
    allowed_hosts: tuple[str, ...]  # A556: the hosts the base address may name; the Founder supplies them in the settings file, so the repository never names the vendor's


_BASELINE_FLOATS = ("stt_final_s", "response_s", "tts_first_s", "session_s", "read_s", "close_s", "hold_s", "min_pause_s", "budget_minutes")
_BASELINE_INTS = ("concurrency", "connections_per_minute", "connections_per_session")
_EXTRA_FLOATS = ("utterance_min_s", "utterance_max_s", "open_timeout_s", "tts_poll_s")
_EXTRA_NULLABLE = ("ping_interval_s", "ping_timeout_s")
_EXTRA_INTS = ("max_message_bytes",)
_HOST = re.compile(r"[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*")  # a plain lowercase host name: no scheme, port, path, wildcard or space


def _load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        raise RunnerConfigError("a configuration file cannot be read as JSON") from None


def _number(value: object) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _plain_host(host: str) -> bool:
    if not _HOST.fullmatch(host) or len(host) > 253:
        return False
    try:
        ipaddress.ip_address(host)
    except ValueError:
        return True
    return False  # an address of numbers is not a host name the Founder supplies


def pinned_to(base_url: str, allowed_hosts: Sequence[str]) -> bool:
    """A556: the base address must be `wss://<host>` with a host on the allowlist, exactly: no user information, no port, no path, and a look-alike or a longer host is not the host."""
    parts = urlsplit(base_url)
    host = parts.hostname
    return parts.scheme == "wss" and host is not None and parts.netloc.lower() == host and host in allowed_hosts and parts.path in ("", "/") and not parts.query and not parts.fragment


def load_settings(path: Path) -> tuple[BaselineSettings, tuple[float, float], WireSettings]:
    """The settings file: exactly the keys below, every one required, of the right type. The values are the Architect's (A380) and the bench's wire settings (A332), never defaults in code."""
    document = _load_json(path)
    names = {*_BASELINE_FLOATS, *_BASELINE_INTS, *_EXTRA_FLOATS, *_EXTRA_NULLABLE, *_EXTRA_INTS, "allowed_hosts"}
    if not isinstance(document, dict) or set(document) != names:
        raise RunnerConfigError("the settings file must hold exactly the required settings")
    for name in (*_BASELINE_FLOATS, *_EXTRA_FLOATS):
        if not _number(document[name]):
            raise RunnerConfigError("a setting has the wrong type")
    for name in _EXTRA_NULLABLE:
        if document[name] is not None and not _number(document[name]):
            raise RunnerConfigError("a setting has the wrong type")
    for name in (*_BASELINE_INTS, *_EXTRA_INTS):
        if isinstance(document[name], bool) or not isinstance(document[name], int):
            raise RunnerConfigError("a setting has the wrong type")
    allowed = document["allowed_hosts"]
    if not isinstance(allowed, list) or not allowed or not all(isinstance(host, str) and _plain_host(host) for host in allowed):
        raise RunnerConfigError("allowed_hosts must be a non-empty list of plain lowercase host names")
    try:
        baseline = BaselineSettings(**{name: document[name] for name in (*_BASELINE_FLOATS, *_BASELINE_INTS)})
        wire = WireSettings(**{name: document[name] for name in ("open_timeout_s", "ping_interval_s", "ping_timeout_s", "max_message_bytes", "tts_poll_s")}, allowed_hosts=tuple(allowed))
        low, high = float(document["utterance_min_s"]), float(document["utterance_max_s"])
        if not (0 < low <= high):
            raise ValueError
    except ValueError:
        raise RunnerConfigError("a setting is out of range") from None
    return baseline, (low, high), wire


_MANIFEST_KEYS = {"label", "locale", "origin", "speaker_ref", "audio_s", "answer_s", "script", "file"}


def load_manifest(path: Path) -> list[ManifestItem]:
    document = _load_json(path)
    if not isinstance(document, list) or not document:
        raise RunnerConfigError("the manifest must be a non-empty list")
    items: list[ManifestItem] = []
    for entry in document:
        if not isinstance(entry, dict) or not {"label", "locale", "origin", "speaker_ref", "audio_s", "answer_s", "script"} <= set(entry) or not set(entry) <= _MANIFEST_KEYS:
            raise RunnerConfigError("a manifest item has missing or unknown fields")
        try:
            origin = AudioOrigin(entry["origin"])
            file = entry.get("file")
            if origin is AudioOrigin.TEAM_RECORDING and not isinstance(file, str):
                raise ValueError
            if not isinstance(entry["label"], str) or not isinstance(entry["script"], str) or not _number(entry["audio_s"]) or not _number(entry["answer_s"]):
                raise ValueError
            items.append(ManifestItem(label=entry["label"], locale=Locale(entry["locale"]), origin=origin, speaker_ref=str(entry["speaker_ref"]), audio_s=float(entry["audio_s"]),
                                      answer_s=float(entry["answer_s"]), script=entry["script"], file=file))
        except ValueError:
            raise RunnerConfigError("a manifest item is not valid") from None
    if len({item.label for item in items}) != len(items):
        raise RunnerConfigError("manifest labels must be unique")
    return items


class _StandIn:
    """The response stage of a vendor baseline: admits the transcript (keeping only its word error rate against the script) and returns a fixed masked sentence."""

    def __init__(self, reference: str | None, answer_text: str) -> None:
        self._reference, self._answer = reference, answer_text
        self.wer: float | None = None

    async def admit(self, text: str) -> None:
        self.wer = None if self._reference is None else word_error_rate(self._reference, text)

    async def answer(self) -> str:
        return self._answer


def _frames(pcm: bytes) -> tuple[AudioChunk, ...]:
    return tuple(AudioChunk(i, AudioCodec.PCM_S16LE, _STT_RATE_HZ, pcm[start:start + _FRAME_BYTES]) for i, start in enumerate(range(0, len(pcm) - len(pcm) % 2, _FRAME_BYTES)))


async def _generate(tts: SpeechSynthesizer, text: str, locale: Locale, model_version: str, deadline_s: float) -> bytes:
    pcm, rate = bytearray(), 0
    async with asyncio.timeout(deadline_s):
        stream = tts.synthesize(SynthesisRequest(text=text, locale=locale, model_version=model_version))
        async for chunk in stream:
            if chunk.codec is not AudioCodec.PCM_S16LE or (rate and chunk.sample_rate_hz != rate):
                raise ProviderError(ProviderFailure.MALFORMED)
            rate = chunk.sample_rate_hz
            pcm += chunk.data
    if not pcm:
        raise ProviderError(ProviderFailure.MALFORMED)
    return downsample_pcm16(bytes(pcm), rate, _STT_RATE_HZ)


def _lost(item: ManifestItem, failure: ProviderFailure, clock_ms: Callable[[], int]) -> SessionResult:
    line = StageTimeline(clock_ms)
    line.mark(Stage.END_OF_SPEECH)  # incomplete by construction: it counts as the worst case
    return SessionResult(item.label, item.origin, line, failure)


async def run_baseline(
    *,
    items: Sequence[ManifestItem],
    stt: SpeechProvider,
    tts: SpeechSynthesizer,
    settings: BaselineSettings,
    bounds_s: tuple[float, float],
    store: AudioStore,
    clock_ms: Callable[[], int],
    wall: Callable[[], datetime],
    sleep: Callable[[float], Awaitable[None]],
    tts_model_version: str,
    on_progress: Callable[[dict[str, object]], None],
    answer_text: str,
    targets_ms: int,
) -> dict[str, object]:
    """Run the manifest and return the report. `on_progress` receives the report so far after EVERY session, and the final report at the end, so a failure part-way never discards what was measured
    (A568); `ended` says why the run stopped: complete, budget_exhausted, rate_limited, two_failures_in_a_row, generation_or_store_failed or store_read_failed. The budget is charged with what was
    actually generated and sent, never with what the manifest declared (A568)."""
    plan = plan_budget(items, budget_minutes=settings.budget_minutes)
    budget = AudioBudget(total_s=settings.budget_minutes * 60.0)
    results: list[SessionResult] = []
    wers: list[float] = []
    generation_s = stt_s = spoken_s = 0.0
    failures_in_a_row = 0
    ran = skipped = 0
    low, high = bounds_s

    def snapshot(state: str) -> dict[str, object]:
        spent = {"generation_s": generation_s, "stt_s": stt_s, "spoken_s": spoken_s}
        if results:
            report = build_report(results, wers=wers, target_ms=targets_ms, spent=spent, budget_s=settings.budget_minutes * 60.0, dropped=len(plan.dropped))
        else:
            report = {"sessions": {"total": 0, "complete": 0, "incomplete": 0}, "dropped_from_manifest": len(plan.dropped),
                      "budget": {**spent, "total_s": generation_s + stt_s + spoken_s, "of_s": settings.budget_minutes * 60.0}}
        report["not_run"] = len(plan.kept) - ran - skipped
        report["skipped_out_of_bounds"] = skipped
        report["ended"] = state
        return report

    ended = "complete"
    for index, item in enumerate(plan.kept):
        if index:
            await sleep(pause_s(settings))
        if item.origin is AudioOrigin.SYNTHETIC:
            assert item.script is not None
            try:
                pcm = await _generate(tts, item.script, item.locale, tts_model_version, settings.session_s)
                store.write(item.speaker_ref, f"{item.label}.pcm", pcm, wall())
            except (ProviderError, TimeoutError, StoreError):
                ended = "generation_or_store_failed"  # the plan stops at the first one
                break
            generated_s = len(pcm) / 2 / _STT_RATE_HZ
            generation_s += generated_s
            try:
                budget.charge(generated_s)  # what the vendor generated, not what the manifest declared
            except BenchError:
                ended = "budget_exhausted"
                break
        else:
            assert item.file is not None
            try:
                pcm = store.read(item.speaker_ref, item.file)
            except StoreError:
                ended = "store_read_failed"  # the report so far is kept, never lost
                break
        utterance = Utterance(label=item.label, locale=item.locale, frames=_frames(pcm), origin=item.origin)
        if not (low <= utterance.duration_s <= high):
            skipped += 1  # the measurement condition is the Founder's (A241): audio outside it is never sent and is counted, and the run goes on
            continue
        if budget.spent_s + utterance.duration_s + item.answer_s > budget.total_s:
            ended = "budget_exhausted"  # the audio to send and the answer to speak must both fit before anything is sent
            break
        budget.charge(item.answer_s)  # the spoken answer is TTS audio generated for this session (the manifest's length: only its first audio is read)
        core = _StandIn(item.script if item.origin is AudioOrigin.TEAM_RECORDING else None, answer_text)
        try:
            async with asyncio.timeout(settings.session_s):
                result = await measure_session(stt=stt, tts=tts, core=core, utterance=utterance, stt_request=_request(item.locale), tts_model_version=tts_model_version, clock_ms=clock_ms,
                                               deadline_s=settings.stt_final_s, core_deadline_s=settings.response_s, tts_deadline_s=settings.tts_first_s, cleanup_s=settings.close_s,
                                               bounds_s=bounds_s, budget=budget)  # charges the audio sent to speech-to-text
        except TimeoutError:
            result = _lost(item, ProviderFailure.TIMEOUT, clock_ms)
        ran += 1
        results.append(result)
        stt_s += utterance.duration_s
        spoken_s += item.answer_s
        if core.wer is not None and result.failure is None:
            wers.append(core.wer)
        failures_in_a_row = failures_in_a_row + 1 if result.failure is not None else 0
        on_progress(snapshot("in_progress"))
        if result.failure is ProviderFailure.RATE_LIMITED:
            ended = "rate_limited"
            break
        if failures_in_a_row >= _STOP_AFTER_FAILURES:
            ended = "two_failures_in_a_row"
            break
    final = snapshot(ended)
    on_progress(final)
    return final


def _request(locale: Locale) -> StreamRequest:
    return StreamRequest(model_version="not reported", locale_hint=locale)  # Intron reports no model version (A182 asks for one in the technical session)


class ReportFile:
    """The report file, created once, owner-only and never over an existing file, and rewritten whole after every session (A568): the new content is written in place and only then is the file cut
    to its length, so the file is never empty and a longer earlier version leaves nothing behind."""

    def __init__(self, descriptor: int, path: Path) -> None:
        self._descriptor = descriptor
        self._path = path

    @classmethod
    def create(cls, path: Path) -> "ReportFile":
        try:
            return cls(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_BINARY", 0) | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0), 0o600), path)
        except OSError:
            raise RunnerConfigError("the report cannot be created (an existing report is never overwritten)") from None

    def write(self, report: dict[str, object]) -> None:
        data = json.dumps(report, indent=2, sort_keys=True).encode("utf-8")
        written = 0
        while written < len(data):
            written += os.pwrite(self._descriptor, data[written:], written)
        os.ftruncate(self._descriptor, len(data))
        os.fsync(self._descriptor)

    def discard_if_empty(self) -> None:
        """A587: a run that failed before any session leaves an empty file that would refuse the rerun, so it is removed: only if it is still empty and only if the path still names THIS file (the
        same device and inode), never a report with anything in it and never somebody else's file."""
        try:
            held = os.fstat(self._descriptor)
            if held.st_size != 0:
                return
            present = os.lstat(self._path)
            if (present.st_dev, present.st_ino) == (held.st_dev, held.st_ino):
                os.unlink(self._path)
        except OSError:
            pass  # the file is gone or cannot be removed: the Founder deletes it, and nothing non-empty was touched

    def close(self) -> None:
        os.close(self._descriptor)


_ARGUMENTS = ("settings", "manifest", "store", "report", "base-url", "voices", "tts-model-version", "answer-text", "target-ms")


def _fail(message: str) -> int:
    print(f"baseline-run: {message}", file=sys.stderr)
    return 2


def _load_voices(path: Path) -> dict[Locale, Voice]:
    document = _load_json(path)
    if not isinstance(document, dict) or not document:
        raise RunnerConfigError("the voices file must be a non-empty object")
    voices: dict[Locale, Voice] = {}
    try:
        for locale, entry in document.items():
            if not isinstance(entry, dict) or set(entry) != {"language", "accent", "gender"}:
                raise ValueError
            voices[Locale(locale)] = Voice(str(entry["language"]), str(entry["accent"]), str(entry["gender"]))
    except ValueError:
        raise RunnerConfigError("a voice is not valid") from None
    return voices


def _verdict(raw: object) -> str:
    """accepted, rejected or inconclusive, from the first message the server sent. Only an authentication error is a rejection: anything unknown is not."""
    try:
        kind = json.loads(raw)["message_type"] if isinstance(raw, str) else None
    except (ValueError, KeyError, TypeError, RecursionError):
        return "inconclusive"
    if kind == "SESSION_CREATED":
        return "accepted"
    if kind == "AUTHENTICATION_ERROR":
        return "rejected"
    return "inconclusive"


async def _probe(connect: Connect, url: str, key: str, open_timeout_s: float, close_s: float) -> str:
    connection = None
    try:
        async with asyncio.timeout(open_timeout_s):
            connection = await connect(url, {"Authorization": f"Bearer {key}"})
            return _verdict(await connection.recv())
    except ProviderError as error:
        return "rejected" if error.kind is ProviderFailure.REJECTED else "inconclusive"  # the handshake refusal of 401 and 403 is the vendor refusing the key
    except (TimeoutError, OSError):
        return "inconclusive"  # a dropped or silent connection says nothing about the key
    finally:
        if connection is not None:
            try:
                async with asyncio.timeout(close_s):
                    await connection.close()
            except (TimeoutError, OSError):
                pass  # the answer is already known; a close that fails or stalls changes nothing about it


def _check_key(argv: Sequence[str], environ: Mapping[str, str], make_connector: Callable[..., Connect]) -> int:
    """`--check-key` (A487, accepted in A523): opens one speech-to-text connection with the key, sends nothing (no audio), closes, and prints one word: accepted, rejected or inconclusive.
    Exit 0 accepted, 1 rejected or inconclusive, 2 refused before any connection. The Founder runs it; the key, the path and the address are never printed."""
    parser = argparse.ArgumentParser(prog="baseline-run", add_help=False, exit_on_error=False)
    parser.add_argument("--check-key", action="store_true", required=True)
    for name in ("settings", "base-url"):
        parser.add_argument(f"--{name}", required=True)
    try:
        args = vars(parser.parse_args(list(argv)))
    except (argparse.ArgumentError, SystemExit):
        return _fail("--check-key needs --settings and --base-url and nothing else")
    key = key_reader(environ)
    try:
        secret = key()
    except KeyFileError:
        return _fail("the Intron key file is missing or not safe (INTRON_API_KEY_FILE, owner-only)")
    try:
        settings, _, wire = load_settings(Path(args["settings"]))
        if not pinned_to(args["base_url"], wire.allowed_hosts):
            return _fail("the base address is not on the allowlist of the settings file")
        url = stt_url(args["base_url"], Locale.EN_NG)
    except (RunnerConfigError, ValueError):
        return _fail("the settings file or the base address is not valid")
    connect = make_connector(open_timeout_s=wire.open_timeout_s, close_timeout_s=settings.close_s, ping_interval_s=wire.ping_interval_s, ping_timeout_s=wire.ping_timeout_s,
                             max_message_bytes=wire.max_message_bytes)
    word = asyncio.run(_probe(connect, url, secret, wire.open_timeout_s, settings.close_s))
    print(word)
    return 0 if word == "accepted" else 1


def main(
    argv: Sequence[str],
    environ: Mapping[str, str],
    *,
    make_connector: Callable[..., Connect] = websocket_connector,
    wall: Callable[[], datetime] | None = None,
    clock_ms: Callable[[], int] | None = None,
    sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
) -> int:
    """Exit 0 ran (see the report), 2 refused before any connection (`--check-key` is described at `_check_key`). Nothing the key, the audio or a transcript is ever printed."""
    if "--check-key" in argv:
        return _check_key(argv, environ, make_connector)
    parser = argparse.ArgumentParser(prog="baseline-run", add_help=False, exit_on_error=False)
    for name in _ARGUMENTS:
        parser.add_argument(f"--{name}", required=True)
    try:
        args = vars(parser.parse_args(list(argv)))
    except (argparse.ArgumentError, SystemExit):
        return _fail("every argument is required and none has a default")
    system = SystemClock()  # the one clock module (INV-21); tests pass their own
    wall_now = wall if wall is not None else system.now_utc
    clock = clock_ms if clock_ms is not None else system.monotonic_ms
    key = key_reader(environ)
    try:
        key()  # read once now so that a missing or unsafe key file refuses before anything is built or connected
    except KeyFileError:
        return _fail("the Intron key file is missing or not safe (INTRON_API_KEY_FILE, owner-only)")
    try:
        settings, bounds, wire = load_settings(Path(args["settings"]))
        items = load_manifest(Path(args["manifest"]))
        voices = _load_voices(Path(args["voices"]))
        if not pinned_to(args["base_url"], wire.allowed_hosts):
            raise RunnerConfigError("the base address is not on the allowlist of the settings file")
        store_root, report_path = Path(args["store"]), Path(args["report"])
        if not str(args["target_ms"]).isdigit() or int(args["target_ms"]) < 1:
            raise RunnerConfigError("target-ms must be a positive whole number")
        if report_path.resolve() == store_root.resolve() or store_root.resolve() in report_path.resolve().parents:
            raise RunnerConfigError("the report must not be written inside the audio store")
        store = AudioStore(store_root, wall_now)
    except (RunnerConfigError, StoreError) as error:
        return _fail(str(error))
    try:
        report_file = ReportFile.create(report_path)  # before any connection: an existing report refuses the run, so no credit is spent for a report that cannot be kept
    except RunnerConfigError as error:
        return _fail(str(error))
    try:
        try:
            connect = make_connector(open_timeout_s=wire.open_timeout_s, close_timeout_s=settings.close_s, ping_interval_s=wire.ping_interval_s, ping_timeout_s=wire.ping_timeout_s,
                                     max_message_bytes=wire.max_message_bytes)
            capabilities = ProviderCapabilities("intron", PipelineMode.TRANSCRIBE_ONLY, Support.YES, tuple((locale, Support.YES) for locale in voices), tuple((locale, Support.YES) for locale in voices),
                                                None, Support.YES, None, False)
            stt = IntronSpeechToText(capabilities=capabilities, connect=connect, base_url=args["base_url"], api_key=key)
            tts = IntronTextToSpeech(capabilities=capabilities, connect=connect, base_url=args["base_url"], api_key=key, voices=voices, poll_s=wire.tts_poll_s)
            asyncio.run(run_baseline(items=items, stt=stt, tts=tts, settings=settings, bounds_s=bounds, store=store, clock_ms=clock, wall=wall_now, sleep=sleep,
                                     tts_model_version=args["tts_model_version"], on_progress=report_file.write, answer_text=args["answer_text"], targets_ms=int(args["target_ms"])))
        except (BenchError, StoreError):
            return _fail("the run was refused (the report written so far is kept)")
        except OSError:
            return _fail("the report could not be written (the run stopped)")
    finally:
        report_file.discard_if_empty()  # a run that failed before any session leaves no empty report to block the rerun (A587)
        report_file.close()
    print("baseline-run: done; the report was written")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:], os.environ))
