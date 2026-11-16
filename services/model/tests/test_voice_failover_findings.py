"""The second round of findings on the VOI-10 pull request: barge-in closes are bounded, the replay buffer is dropped on every path, replay to a second provider is an explicit
decision, no early error leaves the source open, and the buffer is bounded in bytes (VOI-10, REQ-VOI-002, REQ-VOI-004, AGENT-13, AGENT-16, ruling A191; the Security Reviewer's blocking
findings 1 to 4 and notes 5 and 6 on noema#621 at 11a1d089, and Codex's P1 on the same head).

Everything is a SYNTHETIC scripted stand-in under the recorded AGENT-12 exception of noema#412 (the SpeechProvider seam; stalls and cancellations are real asyncio waits, not mocks
inside the code under test). A hung close ends by itself after a second or two so that a test that fails cannot hang the loop.
"""

import asyncio
import time
from collections.abc import AsyncIterator, Sequence

import pytest

from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    FinalTranscript,
    Locale,
    PipelineMode,
    ProviderCapabilities,
    SpeechEvent,
    StreamRequest,
    Support,
    TokenConfidence,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice import failover
from noema_model.voice.bargein import BargeInMonitor, play_until_barge_in
from noema_model.voice.endpoint import VadConfig
from noema_model.voice.failover import FailoverResult, transcribe_with_failover
from tests.support import run
from tests.support_voice import Source, chunk

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)
VAD = VadConfig(speech_rms=300, min_speech_ms=40, hangover_ms=100)
GUARD_S = 8.0
CAPS = ProviderCapabilities("fake", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, Support.YES, "v1", True)


def final() -> FinalTranscript:
    return FinalTranscript("send ten", (TokenConfidence("send", 0.95), TokenConfidence("ten", 0.95)), 0.95, Locale.PCM, "m1")


class Fake:
    def __init__(self, name: str, *, reads: int = 1, fails: bool = False, hang: bool = False) -> None:
        self.name = name
        self.capabilities = CAPS
        self._reads, self._fails, self._hang = reads, fails, hang
        self.calls = 0

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        self.calls += 1
        count = 0
        async for _ in audio:
            count += 1
            if count >= self._reads:
                break
        if self._hang:
            await asyncio.Event().wait()
        if self._fails:
            raise ProviderError(ProviderFailure.UNAVAILABLE)
        yield final()


class Handle:
    """An audio handle. `close` is one of: "ok", "hang" (never returns, honours cancellation), "stubborn" (ignores cancellation for 1.5 s, then ends)."""

    def __init__(self, levels: Sequence[int], close: str = "ok") -> None:
        self._levels = iter(enumerate(levels))
        self._close = close
        self.closed = False

    def __aiter__(self) -> "Handle":
        return self

    async def __anext__(self) -> AudioChunk:
        try:
            i, level = next(self._levels)
        except StopIteration:
            raise StopAsyncIteration from None
        return chunk(i, level)

    async def aclose(self) -> None:
        self.closed = True
        if self._close == "hang":
            await asyncio.Event().wait()
        if self._close == "stubborn":
            end = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < end:
                try:
                    await asyncio.sleep(0.05)
                except asyncio.CancelledError:
                    continue


def barge(answer: Handle, microphone: Handle, telemetry: InMemoryTelemetry | None = None, close_s: float = 0.1):  # type: ignore[no-untyped-def]
    return play_until_barge_in(answer, microphone, BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, close_s=close_s, telemetry=telemetry, correlation_id="corr-b")


# ---- Codex P1 and Security Reviewer blocking 1: barge-in closes are bounded and the microphone close is always attempted ------------------------------------------


@pytest.mark.parametrize("which", ["answer", "microphone"])
@pytest.mark.parametrize("mode", ["hang", "stubborn"])
def test_a_close_that_never_finishes_cannot_hold_the_call_and_the_other_stream_is_closed(which: str, mode: str) -> None:
    answer = Handle([1000] * 6, close=mode if which == "answer" else "ok")
    microphone = Handle([5] * 2000, close=mode if which == "microphone" else "ok")
    telemetry = InMemoryTelemetry()
    started = time.monotonic()

    async def call() -> object:
        result = await asyncio.wait_for(barge(answer, microphone, telemetry), timeout=GUARD_S)
        await asyncio.sleep(1.8)  # let a stubborn close end by itself so the loop can be torn down
        return result

    result = run(call())
    assert time.monotonic() - started < 4.0
    assert answer.closed and microphone.closed  # the microphone close is attempted even when the answer's stalls, and the other way round
    assert result.release_failed  # type: ignore[attr-defined]  # a stream that could not be released in time is never a clean success
    assert [f["code"] for n, c, f in telemetry.events if n == "voice_audio_cleanup" and c == "corr-b"] == ["VOI_AUDIO_CLOSE_ABANDONED"]


def test_a_caller_that_cancels_during_barge_in_still_gets_both_streams_closed() -> None:
    answer, microphone = Handle([1000] * 600), Handle([5] * 100000)

    async def call() -> None:
        task = asyncio.ensure_future(barge(answer, microphone, close_s=0.5))
        await asyncio.sleep(0.05)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task

    run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert answer.closed and microphone.closed


def test_a_clean_close_is_not_a_release_failure() -> None:
    answer, microphone = Handle([1000] * 6), Handle([5] * 2000)
    result = run(barge(answer, microphone))
    assert answer.closed and microphone.closed and not result.release_failed


# ---- Security Reviewer blocking 2: the replay buffer is dropped on every path -----------------------------------------------------------------------------------------


@pytest.fixture
def buffers(monkeypatch: pytest.MonkeyPatch) -> list[failover._ReplayBuffer]:
    made: list[failover._ReplayBuffer] = []

    class Recording(failover._ReplayBuffer):
        def __init__(self, *args: object, **kwargs: object) -> None:
            super().__init__(*args, **kwargs)  # type: ignore[arg-type]
            made.append(self)

    monkeypatch.setattr(failover, "_ReplayBuffer", Recording)
    return made


def go(providers: Sequence[Fake], source: Source, *, max_buffered: int = 100, allow_replay: bool = True) -> FailoverResult:
    async def call() -> FailoverResult:
        return await transcribe_with_failover(providers, REQUEST, source, deadline_s=0.3, cleanup_s=0.1, on_partial=lambda _: None, max_buffered_chunks=max_buffered,  # type: ignore[arg-type]
                                              allow_replay=allow_replay)

    return run(asyncio.wait_for(call(), timeout=GUARD_S))


def mic(n: int = 8) -> Source:
    return Source([chunk(i, 500) for i in range(n)])


def test_the_audio_is_dropped_after_success(buffers: list[failover._ReplayBuffer]) -> None:
    go([Fake("a", reads=3)], mic())
    assert buffers[0].held_chunks == 0 and buffers[0].held_bytes == 0


def test_the_audio_is_dropped_after_every_provider_fails(buffers: list[failover._ReplayBuffer]) -> None:
    go([Fake("a", reads=3, fails=True), Fake("b", reads=3, fails=True)], mic())
    assert buffers[0].held_chunks == 0 and buffers[0].held_bytes == 0


def test_the_audio_is_dropped_after_an_overflow(buffers: list[failover._ReplayBuffer]) -> None:
    result = go([Fake("a", reads=8, fails=True), Fake("b")], mic(8), max_buffered=2)
    assert result.replay_overflow
    assert buffers[0].held_chunks == 0 and buffers[0].held_bytes == 0


def test_the_audio_is_dropped_when_the_caller_cancels(buffers: list[failover._ReplayBuffer]) -> None:
    source = mic()

    async def call() -> None:
        task = asyncio.ensure_future(transcribe_with_failover([Fake("a", reads=3, hang=True)], REQUEST, source, deadline_s=5.0, cleanup_s=0.1, on_partial=lambda _: None,  # type: ignore[arg-type]
                                                              max_buffered_chunks=100, allow_replay=True))
        await asyncio.sleep(0.1)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task

    run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert source.closed
    assert buffers[0].held_chunks == 0 and buffers[0].held_bytes == 0  # the bounded release ran although the caller was cancelled (Security Reviewer's note 5)


# ---- Security Reviewer blocking 3: replay to a second vendor is an explicit decision, never a default ---------------------------------------------------------------


def test_replay_to_a_second_provider_is_refused_unless_it_is_allowed() -> None:
    first, second = Fake("a", reads=3, fails=True), Fake("b")
    source = mic()
    result = go([first, second], source, allow_replay=False)
    assert first.calls == 1 and second.calls == 0  # the audio the first vendor heard is not sent to another
    assert result.utterance is None and result.offer_text and not result.replay_overflow
    assert source.closed


def test_with_replay_allowed_the_second_provider_hears_the_whole_utterance() -> None:
    second = Fake("b")
    result = go([Fake("a", reads=3, fails=True), second], mic(), allow_replay=True)
    assert result.utterance is not None and result.utterance.provider_name == "b"


def test_allowing_replay_has_no_default() -> None:
    async def call() -> FailoverResult:
        return await transcribe_with_failover([Fake("a")], REQUEST, mic(), deadline_s=0.3, on_partial=lambda _: None, max_buffered_chunks=10)  # type: ignore[arg-type,call-arg]

    with pytest.raises(TypeError, match="allow_replay"):
        run(call())


# ---- Security Reviewer blocking 4: an early error never leaves the source open --------------------------------------------------------------------------------------


@pytest.mark.parametrize("providers,limit,match", [([], 10, "providers"), ([Fake("a")], 0, "buffer")])
def test_a_configuration_error_still_closes_the_source(providers: list[Fake], limit: int, match: str) -> None:
    source = mic()
    with pytest.raises(ValueError, match=match):
        go(providers, source, max_buffered=limit)
    assert source.closed


# ---- Security Reviewer note 6 and ruling A191: the buffer is bounded in bytes and a frame outside the limits is refused -----------------------------------------------


def test_a_chunk_over_the_frame_cap_is_refused_and_the_source_is_closed(buffers: list[failover._ReplayBuffer]) -> None:
    big = AudioChunk(0, AudioCodec.PCM_S16LE, 16000, b"\x10\x00" * 16_001)  # 32,002 bytes
    source = Source([big])
    with pytest.raises(ValueError, match="frame"):
        go([Fake("a")], source)
    assert source.closed and buffers[0].held_bytes == 0


def test_the_buffer_holds_at_most_the_chunks_it_was_given_room_for_in_bytes(buffers: list[failover._ReplayBuffer]) -> None:
    source = mic(8)
    result = go([Fake("a", reads=8, fails=True), Fake("b")], source, max_buffered=3)
    assert result.replay_overflow  # three chunks of 640 bytes fit; the fourth does not, so the second provider is not handed a partial utterance
