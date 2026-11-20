"""UND-16: the acceptance run's pool, at its configured size, covers the pipeline's concurrent fan-out (REQ-INT-002, REQ-TST-010; INV-16, INV-02).

Run 44521380842 refused the fifth of the five calls the pipeline starts together (the quiet injection screen) because the pool held four workers; the screen was silently empty. Here the real
pipeline and the real harness run against a scripted model (SYNTHETIC), each call holding a worker of the real `BoundedBlockingPool` for a moment as a blocking HTTP call would, and the pool
refuses as `UrllibTransport` does (`ProviderError(UNAVAILABLE, local=True)`). No socket, no key."""

import sys
import threading
import time
from datetime import UTC, datetime
from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from noema_model.harness import AttemptOutcome  # noqa: E402
from noema_model.platform.blocking import BoundedBlockingPool, PoolSaturated  # noqa: E402
from noema_model.providers.llm.base import ProviderError, ProviderFailure, ProviderRequest, ProviderResponse  # noqa: E402
from noema_model.understanding.pipeline import Outcome, run_pipeline  # noqa: E402
from noema_model.understanding.pipeline.types import PipelineResult  # noqa: E402
from benchmark.bakeoff.budget import BudgetGuard  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from benchmark.compile_acceptance.build import POOL_SIZE, build_backend  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from benchmark.compile_acceptance.recording import Recorder  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from tests.support import run  # noqa: E402
from tests.support_pipeline import G_A, GOOD_MENTIONS, FakeSource, ScriptedModel, candidate, harness_for, make_input, no_errors, normaliser, pipeline_config  # noqa: E402


class PooledModel:
    """The scripted model behind a real bounded pool: each call holds one worker for a moment, and a full pool refuses the call at once, as the real transport does."""

    name = "alpha"

    def __init__(self, pool: BoundedBlockingPool) -> None:
        self._inner, self._pool = ScriptedModel(answers=G_A, mentions=GOOD_MENTIONS), pool
        self._lock, self.in_flight, self.most_in_flight, self.refused = threading.Lock(), 0, 0, 0

    def _hold(self) -> None:
        with self._lock:
            self.in_flight += 1
            self.most_in_flight = max(self.most_in_flight, self.in_flight)
        time.sleep(0.05)
        with self._lock:
            self.in_flight -= 1

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        try:
            await self._pool.run(self._hold)
        except PoolSaturated:
            self.refused += 1
            raise ProviderError(ProviderFailure.UNAVAILABLE, local=True) from None
        return await self._inner.complete(request)


def pipeline_at(workers: int) -> tuple[PooledModel, PipelineResult]:
    pool = BoundedBlockingPool(workers, name="test-llm")
    model = PooledModel(pool)
    harness, _ = harness_for(model)  # type: ignore[arg-type]
    try:
        result = run(run_pipeline(make_input(), pipeline_config(), harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    finally:
        pool.close()
    return model, result


def test_at_the_acceptance_pool_size_no_call_of_the_fan_out_is_refused() -> None:
    """The size the acceptance run uses: five calls in flight at once, none refused, the screen answered."""
    model, result = pipeline_at(POOL_SIZE)
    assert model.most_in_flight == 5 and model.refused == 0
    assert result.outcome is Outcome.PROPOSED and "utterance" in result.injection_signals
    assert all(c.attempts[0].outcome is AttemptOutcome.OK for c in result.calls)


def test_at_the_old_size_of_four_the_fifth_call_is_refused_and_the_screen_is_silently_empty() -> None:
    """The control: the run's finding reproduced, so the test above can fail for the right reason."""
    model, result = pipeline_at(4)
    assert model.refused >= 1 and model.most_in_flight == 4
    refused = [c for c in result.calls if c.attempts[0].outcome is AttemptOutcome.UNAVAILABLE]
    assert len(refused) >= 1
    assert "utterance" not in result.injection_signals


def test_the_live_backend_builds_its_transport_with_the_acceptance_pool_size(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """The size the run uses is the size the wire transport is built with (nothing sends: the transport is a recorder of its own arguments)."""
    from noema_model.providers.llm import transport as wire

    built: list[dict[str, Any]] = []

    class Capture:
        def __init__(self, **kwargs: Any) -> None:
            built.append(kwargs)

    monkeypatch.setattr(wire, "UrllibTransport", Capture)
    rec = Recorder(tmp_path / "out", run_id="r1", provider="openai", model_id="gpt-6-luna", now=lambda: datetime(2026, 11, 19, tzinfo=UTC))
    args = SimpleNamespace(harness_config="benchmark/decisions/harness-gpt-6-luna.json", pack_ref="fin@1.0.0")
    build_backend(False, {"OPENAI_API_KEY": "sk-synthetic-canary-9f3a-not-a-key"}, BudgetGuard(limit_micros=1), rec, args)
    assert built == [{"pool_size": POOL_SIZE}]
