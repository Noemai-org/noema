"""The baseline-run library (VOI-16, REQ-VOI-001, REQ-VOI-004, REQ-NFR-003; rulings A377 to A383 and A451, AGENT-13, AGENT-16, INV-21).

What the live Intron baseline run is made of. There is no network and no key here: the adapters, the clock and the key are arguments of the runner that calls this library.

- `AudioStore`: the writer of the private raw-audio store (deploy/compose/audio-store, noema#819). Files 0600 and folders 0700 under `<speaker_ref>/`, set explicitly and not left to the umask, each
  file's modification time set to the ANALYSIS date (the store's retention counts from it), nothing written outside the store, an existing file never overwritten, a link never followed.
- `BaselineSettings`: every setting is required and none has a default (AGENT-16); the values are the Architect's (A380) and are passed in.
- `plan_budget`: the audio budget counted in three parts (generated, sent to speech-to-text, spoken), trimmed from the END of the manifest and never by shortening an item.
- `pause_s`: the pause between sessions, the larger of the minimum and what the vendor's published connections-per-minute limit allows.
- `downsample_pcm16`: text-to-speech answers arrive at 48 kHz and speech-to-text takes 16 kHz only (A191), so synthetic fixtures are reduced by a whole-number ratio.
- `word_error_rate`: counted inside the core stand-in against the script, so no transcript is ever kept.
- `build_report`: counts, stages and failures; never audio, transcript text or a key.
"""

import math
import os
import re
import stat
from collections import Counter
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

from noema_model.providers.speech.base import Locale
from noema_model.voice.bench import AudioOrigin, SessionResult, measure_batch
from noema_model.voice.latency import Stage, nearest_rank_percentile

_SPEAKER_REF = re.compile(r"[A-Za-z0-9_-][A-Za-z0-9._-]{0,63}")  # never starts with a dot: the store scan counts such a name as an object without a speaker reference (SR on #824)
_FILE_NAME = re.compile(r"[A-Za-z0-9_-][A-Za-z0-9._-]{0,127}")  # never starts with a dot
_BYTES_PER_SAMPLE = 2


class StoreError(Exception):
    """The store refused the write or is not right. The message is fixed text and never carries a name, a path or content."""


class AudioStore:
    def __init__(self, root: Path, now: Callable[[], datetime]) -> None:
        """`now` is the injected clock (INV-21): an analysis date later than it is refused, because it would extend the retention."""
        self._now = now
        try:
            info = os.lstat(root)
        except OSError:
            raise StoreError("the audio store does not exist") from None
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
            raise StoreError("the audio store must be a real directory, not a link")
        if os.name != "nt":
            if info.st_uid != getattr(os, "geteuid")():
                raise StoreError("the audio store is not owned by the user that runs this")
            if stat.S_IMODE(info.st_mode) != 0o700:
                raise StoreError("the audio store must be mode 0700")
        self._root = root

    def write(self, speaker_ref: str, name: str, data: bytes, analysed_on: datetime) -> None:
        """Write one file under `<store>/<speaker_ref>/`, mode 0600, in a folder of mode 0700, with `analysed_on` as its modification time."""
        if not _SPEAKER_REF.fullmatch(speaker_ref) or speaker_ref in (".", ".."):
            raise StoreError("a speaker reference must be a plain name")
        if not _FILE_NAME.fullmatch(name):
            raise StoreError("a file name must be a plain name")
        if analysed_on.tzinfo is None:
            raise StoreError("the analysis date must carry a time zone")
        if analysed_on > self._now():
            raise StoreError("the analysis date is in the future and would extend the retention")
        folder = self._root / speaker_ref
        try:
            try:
                info = os.lstat(folder)
            except FileNotFoundError:
                os.mkdir(folder, 0o700)
                os.chmod(folder, 0o700)  # not left to the umask
            else:
                if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
                    raise StoreError("the speaker folder must be a real directory, not a link")
                if os.name != "nt" and (info.st_uid != getattr(os, "geteuid")() or stat.S_IMODE(info.st_mode) != 0o700):
                    raise StoreError("the speaker folder must be owned by the user that runs this and be mode 0700")
            flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_BINARY", 0)
            descriptor = os.open(folder / name, flags, 0o600)
        except FileExistsError:
            raise StoreError("the file already exists and is never overwritten") from None
        except OSError:
            raise StoreError("the audio store could not be written") from None
        target = folder / name
        try:
            try:
                if os.name != "nt":
                    os.fchmod(descriptor, 0o600)  # not left to the umask
                written = 0
                while written < len(data):
                    written += os.write(descriptor, data[written:])
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
            stamp = analysed_on.timestamp()
            os.utime(target, (stamp, stamp))  # the retention counts from the analysis date
        except OSError:
            try:
                os.unlink(target)  # a partial file is never kept: it would hold raw audio and block the name for ever (O_EXCL)
            except OSError:
                pass
            raise StoreError("the audio store could not be written") from None


def _positive(value: float, what: str) -> float:
    if isinstance(value, bool) or not (math.isfinite(value) and value > 0):
        raise ValueError(f"{what} must be a positive finite number from configuration")
    return value


def _whole(value: int, what: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < 1:
        raise ValueError(f"{what} must be a positive whole number from configuration")
    return value


@dataclass(frozen=True, slots=True)
class BaselineSettings:
    """Every field is required: the values are the Architect's (A380) and come from outside the code (AGENT-16)."""

    stt_final_s: float
    response_s: float
    tts_first_s: float
    session_s: float
    read_s: float
    close_s: float
    hold_s: float
    concurrency: int
    min_pause_s: float
    connections_per_minute: int  # the vendor's published limit on new connections per minute (sourced in docs/voice/intron-docs-notes-DRAFT.md)
    connections_per_session: int  # how many connections one session opens (speech-to-text and text-to-speech)
    budget_minutes: float

    def __post_init__(self) -> None:
        for what in ("stt_final_s", "response_s", "tts_first_s", "session_s", "read_s", "close_s", "hold_s", "min_pause_s", "budget_minutes"):
            _positive(getattr(self, what), what)
        _whole(self.connections_per_minute, "connections_per_minute")
        _whole(self.connections_per_session, "connections_per_session")
        if _whole(self.concurrency, "concurrency") != 1:
            raise ValueError("sessions run one at a time (A380)")


def pause_s(settings: BaselineSettings) -> float:
    """The pause between sessions: the larger of the minimum and the time the published connection limit needs for the connections one session opens."""
    return max(settings.min_pause_s, 60.0 * settings.connections_per_session / settings.connections_per_minute)


@dataclass(frozen=True, slots=True)
class ManifestItem:
    label: str
    locale: Locale
    origin: AudioOrigin
    speaker_ref: str
    audio_s: float  # the utterance's length in seconds (declared in the manifest, the estimate the budget is planned from)
    answer_s: float  # the spoken answer's length in seconds
    script: str | None  # what was said; required for a recording of a team member, so its accuracy can be counted

    def __post_init__(self) -> None:
        _positive(self.audio_s, "audio_s")
        _positive(self.answer_s, "answer_s")
        if self.origin is AudioOrigin.TEAM_RECORDING and not (self.script and self.script.strip()):
            raise ValueError("a recording of a team member needs its script")


@dataclass(frozen=True, slots=True)
class BudgetPlan:
    kept: tuple[ManifestItem, ...]
    dropped: tuple[ManifestItem, ...]
    generation_s: float
    stt_s: float
    spoken_s: float

    @property
    def total_s(self) -> float:
        return self.generation_s + self.stt_s + self.spoken_s


def plan_budget(items: Sequence[ManifestItem], *, budget_minutes: float) -> BudgetPlan:
    """The three parts of the budget (generated by text-to-speech, sent to speech-to-text, spoken as answers) counted together against `budget_minutes`. An item that does not fit is dropped with
    everything after it: the manifest is trimmed from its end, never reordered and never shortened."""
    if not items:
        raise ValueError("the manifest has no items")
    limit = _positive(budget_minutes, "budget_minutes") * 60.0
    generation = stt = spoken = 0.0
    kept: list[ManifestItem] = []
    for index, entry in enumerate(items):
        add_generation = entry.audio_s if entry.origin is AudioOrigin.SYNTHETIC else 0.0
        if generation + add_generation + stt + entry.audio_s + spoken + entry.answer_s > limit + 1e-9:
            return BudgetPlan(tuple(kept), tuple(items[index:]), generation, stt, spoken)
        generation, stt, spoken = generation + add_generation, stt + entry.audio_s, spoken + entry.answer_s
        kept.append(entry)
    return BudgetPlan(tuple(kept), (), generation, stt, spoken)


def downsample_pcm16(data: bytes, src_hz: int, dst_hz: int) -> bytes:
    """Reduce 16-bit little-endian mono PCM by a whole-number ratio, averaging each group of samples. A partial group at the end is dropped. Only a whole-number ratio downwards is supported."""
    for rate in (src_hz, dst_hz):
        if isinstance(rate, bool) or not isinstance(rate, int) or rate < 1:
            raise ValueError("a sample rate must be a positive whole number")
    if len(data) % _BYTES_PER_SAMPLE:
        raise ValueError("PCM16 data must be a whole number of samples")
    if dst_hz > src_hz or src_hz % dst_hz:
        raise ValueError("only a whole-number ratio downwards is supported")
    ratio = src_hz // dst_hz
    if ratio == 1:
        return data
    samples = [int.from_bytes(data[i:i + 2], "little", signed=True) for i in range(0, len(data), 2)]
    out = bytearray()
    for start in range(0, len(samples) - len(samples) % ratio, ratio):
        out += (sum(samples[start:start + ratio]) // ratio).to_bytes(2, "little", signed=True)
    return bytes(out)


def _words(text: str) -> list[str]:
    return re.sub(r"[^\w\s]", " ", text.lower()).split()


def word_error_rate(reference: str, hypothesis: str) -> float:
    """Word edit distance over the number of reference words; case and punctuation are not errors."""
    ref, hyp = _words(reference), _words(hypothesis)
    if not ref:
        raise ValueError("the reference has no words")
    previous = list(range(len(hyp) + 1))
    for i, word in enumerate(ref, start=1):
        current = [i]
        for j, other in enumerate(hyp, start=1):
            current.append(min(previous[j] + 1, current[j - 1] + 1, previous[j - 1] + (word != other)))
        previous = current
    return previous[-1] / len(ref)


def _percentiles(values: list[float]) -> dict[str, int | None]:
    if not values:
        return {"p50": None, "p95": None}
    return {"p50": round(nearest_rank_percentile(values, 0.5)), "p95": round(nearest_rank_percentile(values, 0.95))}


def build_report(results: Sequence[SessionResult], *, wers: Sequence[float], target_ms: int, spent: dict[str, float], budget_s: float, dropped: int) -> dict[str, object]:
    """Counts, stages, failures, budget and accuracy, as plain numbers. Never audio, transcript text or a key; a session is named by nothing here. No verdict is written: the target is printed as a fact."""
    if not results:
        raise ValueError("no sessions")
    summary = measure_batch(results, target_ms=target_ms)
    complete = [r.timeline.durations() for r in results if r.failure is None and r.timeline.complete]
    stages = {
        "stt": _percentiles([float(d[Stage.FINAL_TRANSCRIPT]) for d in complete]),
        "response": _percentiles([float(d[Stage.INTENT_ADMITTED] + d[Stage.ANSWER_TEXT]) for d in complete]),
        "tts": _percentiles([float(d[Stage.FIRST_AUDIO]) for d in complete]),
    }
    total = spent["generation_s"] + spent["stt_s"] + spent["spoken_s"]
    return {
        "sessions": {"total": summary.n_total, "complete": summary.n_complete, "incomplete": summary.n_incomplete},
        "stages_ms": stages,
        "total_p95_ms": round(summary.p95_ms) if math.isfinite(summary.p95_ms) else None,  # None when an incomplete session is the worst case
        "target_ms": target_ms,
        "failures": dict(Counter(r.failure.name for r in results if r.failure is not None)),
        "origins": dict(Counter(r.origin.value for r in results)),
        "dropped_from_manifest": dropped,
        "budget": {"generation_s": spent["generation_s"], "stt_s": spent["stt_s"], "spoken_s": spent["spoken_s"], "total_s": total, "of_s": budget_s},
        "accuracy": {"team_recordings": len(wers), "mean_word_error_rate": round(sum(wers) / len(wers), 4) if wers else None},
    }
