"""A speech failure degrades to text (VOI-10, REQ-VOI-002, REQ-VOI-004, REQ-INT-006, PERD §35.3 and R-18, AGENT-13; rulings A179 and A249).

A249: there is no replay to a second provider; failover means degrade to text. `transcribe_or_text` makes **one** attempt with **one** provider and keeps nothing: the audio goes
to the provider once, live, and is never buffered or sent anywhere else. If the provider fails, is silent, ends with no final, or ignores its cancellation, the result has no
utterance (a partial is never a result), says to offer text, and carries the failure kind so the caller can record it; nothing the provider says reaches the display after the
call returns; and the live source is closed within `cleanup_s` whatever happened (INV-07, AGENT-13), the way `transcribe_utterance` does it.

A provider that is not qualified, or whose data terms are not confirmed (`retention_terms` is not YES), is never called and never given a chunk (REQ-INT-006, PERD R-18, A180):
the source is closed, the result says `skipped`, and the person is offered text. Which provider to use, and whether a second one exists, is the caller's configuration; a second
provider is a Founder decision at pilot readiness (OD-P-88), and would be a new attempt with fresh audio, never a replay.

The source must support weak references (see `AudioSource`); one that does not is refused with `TypeError` and still closed.
"""

from collections.abc import Callable
from dataclasses import dataclass

from noema_model.platform.telemetry import Telemetry
from noema_model.providers.speech.base import SpeechProvider, StreamRequest, Support
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.endpoint import AudioSource
from noema_model.voice.pipeline import FinalUtterance, cleanup_reporter, release_unused, transcribe_utterance


@dataclass(frozen=True, slots=True)
class DegradeResult:
    utterance: FinalUtterance | None
    offer_text: bool  # True whenever there is no utterance: the person is offered text, never a guess
    failure: ProviderFailure | None  # why there is no utterance (REJECTED when the provider was skipped); None on success
    skipped: bool = False  # True when the provider was not called at all: it is not qualified, or its data terms are not confirmed
    error_type: str | None = None  # the class name (never the message) of an unexpected error that was turned into this fallback; None for a typed provider failure


def eligible(provider: SpeechProvider) -> bool:
    """May this provider hear the audio? Qualified, and its data terms confirmed (REQ-INT-006, PERD R-18). An unverified or refused term is never a yes."""
    capabilities = provider.capabilities
    return capabilities.qualified is True and capabilities.retention_terms is Support.YES


async def transcribe_or_text(
    provider: SpeechProvider,
    request: StreamRequest,
    audio: AudioSource,
    *,
    deadline_s: float,
    on_partial: Callable[[str], None],
    cleanup_s: float | None = None,
    hold_s: float | None = None,
    telemetry: Telemetry | None = None,
    correlation_id: str = "uncorrelated",
) -> DegradeResult:
    """One attempt with one provider. `deadline_s` bounds it and `cleanup_s` (default: `deadline_s`) bounds each cleanup, including the release of the live source. Cleanup that
    happens after the call has returned is reported through `telemetry` on `correlation_id`, as for `transcribe_utterance`."""
    bound_s = deadline_s if cleanup_s is None else cleanup_s
    if not eligible(provider):
        await release_unused(audio, bound_s, cleanup_reporter(telemetry, correlation_id))  # the call owns the source: nothing was allowed to listen, and the microphone is still released
        return DegradeResult(None, True, ProviderFailure.REJECTED, skipped=True)
    try:
        utterance = await transcribe_utterance(
            provider, request, audio, deadline_s=deadline_s, on_partial=on_partial, cleanup_s=cleanup_s, hold_s=hold_s, telemetry=telemetry, correlation_id=correlation_id,
        )
    except ProviderError as error:
        return DegradeResult(None, True, error.kind)
    except TypeError:
        raise  # the caller broke the contract (a source that cannot be weakly referenced): a programming error that must be seen, not degraded
    except Exception as error:  # noqa: BLE001 - an unexpected error must not take the person's answer with it: the text is offered, the class name is kept for the caller to record, the message never (AGENT-13)
        return DegradeResult(None, True, ProviderFailure.UNAVAILABLE, error_type=type(error).__name__)
    return DegradeResult(utterance, False, None)
