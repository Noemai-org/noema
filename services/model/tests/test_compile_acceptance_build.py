"""UND-16 (noema#546, A198): the live side of the compile-acceptance runner, built from the model service (INV-29: the key is read by the harness's provider adapter, nowhere else).

The real harness, the real pack, lexicon and masking limits and the real pipeline are used; only the wire is a scripted transport (SYNTHETIC) and no socket is opened. Nothing here claims a live reply
parses: the first live run is the evidence for that, and it is the Founder's to dispatch. Written before `benchmark.compile_acceptance.build` exists."""

import json
import sys
from collections.abc import Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from noema_model.providers.llm.base import HttpResponse, ProviderError, ProviderFailure  # noqa: E402

from benchmark.bakeoff.budget import BudgetGuard  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from benchmark.compile_acceptance.build import DryTransport, RecordingTransport, build_backend  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from benchmark.compile_acceptance.golden import GOLDEN  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from benchmark.compile_acceptance.recording import Recorder  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from benchmark.compile_acceptance.runner import CompileFailed  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from tests.support import run  # noqa: E402
from tests.support_swap import deny_sockets  # noqa: E402

KEY = "sk-synthetic-canary-9f3a-not-a-key"
REPLY = json.dumps({"id": "resp_x", "model": "gpt-6-luna-2026-10-01", "output": [], "usage": {"input_tokens": 1_000, "output_tokens": 2_000}}).encode()
RATES = {"input_micro_per_mtok": 100_000, "output_micro_per_mtok": 500_000, "max_output_tokens": 4_000}


class Wire:
    def __init__(self, reply: bytes = REPLY, status: int = 200, raises: Exception | None = None) -> None:
        self.reply, self.status, self.raises = reply, status, raises
        self.sent: list[tuple[str, Mapping[str, str], bytes]] = []

    async def post(self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int) -> HttpResponse:
        self.sent.append((url, headers, body))
        if self.raises is not None:
            raise self.raises
        return HttpResponse(self.status, self.reply)


def recorder(tmp: Path) -> Recorder:
    return Recorder(tmp / "out", run_id="r1", provider="openai", model_id="gpt-6-luna", now=lambda: datetime(2026, 11, 17, tzinfo=UTC))


def transport(inner: Any, guard: BudgetGuard, rec: Recorder) -> RecordingTransport:
    return RecordingTransport(inner, guard, rec, **RATES)


def test_a_call_is_sent_recorded_and_settled_at_the_cost_the_reply_reports(tmp_path: Path) -> None:
    async def go() -> None:
        guard, rec, wire = BudgetGuard(limit_micros=5_000_000), recorder(tmp_path), Wire()
        rec.case_id = "G-A"
        t = transport(wire, guard, rec)
        reply = await t.post("https://h.invalid/v1/responses", {"Authorization": f"Bearer {KEY}"}, b'{"input":"Tunde"}', 30.0, 1_000_000)
        assert (reply.status, reply.body) == (200, REPLY) and len(wire.sent) == 1 and t.calls == 1
        assert guard.spent_micros == 1_000 * 100_000 // 1_000_000 + 2_000 * 500_000 // 1_000_000  # 100 + 1,000 micro-dollars, from the reported usage
        doc = json.loads((tmp_path / "out" / "recordings" / "rec-0001.json").read_text())
        assert (doc["case_id"], doc["status"], doc["cost_micros"], doc["reported_model_id"]) == ("G-A", 200, 1_100, "gpt-6-luna-2026-10-01")
        for path in (tmp_path / "out").rglob("*"):
            if path.is_file():
                assert KEY not in path.read_text() and "Authorization" not in path.read_text()

    run(go())

def test_a_reply_that_reports_no_usage_is_settled_at_its_projection(tmp_path: Path) -> None:
    async def go() -> None:
        guard, rec = BudgetGuard(limit_micros=5_000_000), recorder(tmp_path)
        rec.case_id = "G-A"
        await transport(Wire(reply=b'{"model":"m"}'), guard, rec).post("https://h.invalid/p", {}, b"x" * 1_000, 30.0, 1_000_000)
        assert guard.spent_micros == -(-(1_000 * 100_000 + 4_000 * 500_000) // 1_000_000)

    run(go())

def test_a_request_that_would_pass_the_stop_is_refused_before_it_is_sent_and_the_stop_stays(tmp_path: Path) -> None:
    async def go() -> None:
        guard, rec, wire = BudgetGuard(limit_micros=100), recorder(tmp_path), Wire()
        rec.case_id = "G-A"
        t = transport(wire, guard, rec)
        with pytest.raises(ProviderError):
            await t.post("https://h.invalid/p", {}, b"x" * 100, 30.0, 1_000_000)
        assert wire.sent == [] and guard.stopped and t.calls == 0
        with pytest.raises(ProviderError):
            await t.post("https://h.invalid/p", {}, b"", 30.0, 1_000_000)  # cheaper, still refused
        assert wire.sent == []

    run(go())

def test_a_failure_of_the_wire_is_recorded_with_its_name_settled_at_its_projection_and_raised_unchanged(tmp_path: Path) -> None:
    async def go() -> None:
        guard, rec = BudgetGuard(limit_micros=5_000_000), recorder(tmp_path)
        rec.case_id = "G-B"
        t = transport(Wire(raises=ProviderError(ProviderFailure.TIMEOUT)), guard, rec)
        with pytest.raises(ProviderError) as caught:
            await t.post("https://h.invalid/p", {}, b"x" * 10, 30.0, 1_000_000)
        assert caught.value.kind is ProviderFailure.TIMEOUT
        doc = json.loads((tmp_path / "out" / "recordings" / "rec-0001.json").read_text())
        assert (doc["status"], doc["failure"], doc["cost_micros"]) == (None, "TIMEOUT", doc["projected_micros"]) and guard.spent_micros == doc["projected_micros"]

    run(go())

def test_the_dry_transport_keeps_the_first_request_of_each_case_prices_it_and_sends_nothing() -> None:
    async def go() -> None:
        d = DryTransport(**RATES)
        d.case_id = "G-A"
        for size in (1_000, 9_000):  # a later, larger request of the same example does not replace the first
            with pytest.raises(ProviderError):
                await d.post("https://h.invalid/p", {"Authorization": f"Bearer {KEY}"}, b"x" * size, 30.0, 1_000_000)
        d.case_id = "G-B"
        with pytest.raises(ProviderError):
            await d.post("https://h.invalid/p", {}, b"y" * 2_000, 30.0, 1_000_000)
        assert d.first_costs() == [("G-A", -(-(1_000 * 100_000 + 4_000 * 500_000) // 1_000_000)), ("G-B", -(-(2_000 * 100_000 + 4_000 * 500_000) // 1_000_000))]
        assert KEY not in repr(d.__dict__)

    run(go())

ENV = {"OPENAI_API_KEY": KEY}


def args(**over: Any) -> Any:
    from types import SimpleNamespace
    return SimpleNamespace(**{"harness_config": "benchmark/decisions/harness-gpt-6-luna.json", "pack_ref": "fin@1.0.0", **over})


def test_the_dry_backend_builds_the_real_pipeline_and_prices_the_first_request_of_every_example_with_no_socket(tmp_path: Path) -> None:
    async def go() -> None:
        restore = deny_sockets()
        try:
            guard, rec = BudgetGuard(limit_micros=5_000_000, spent_micros=326_693), recorder(tmp_path)
            backend = build_backend(True, {}, guard, rec, args())
            for g in GOLDEN:
                with pytest.raises(CompileFailed):
                    await backend.compile_one(g)
            costs = backend.dry_costs()
            assert [c for c, _ in costs] == [g.case_id for g in GOLDEN] and all(m > 0 for _, m in costs)
            assert backend.calls() == 0 and guard.spent_micros == 326_693
            assert not (tmp_path / "out" / "recordings").exists()
        finally:
            restore()

    run(go())

def test_the_first_request_names_no_digit_run_over_four_and_carries_no_key(tmp_path: Path) -> None:
    async def go() -> None:
        from noema_model.platform.redaction.digit_runs import longest_digit_run
        guard, rec, wire = BudgetGuard(limit_micros=5_000_000), recorder(tmp_path), Wire(status=500, reply=b"{}")
        backend = build_backend(False, ENV, guard, rec, args(), inner=wire)
        with pytest.raises(CompileFailed):
            await backend.compile_one(GOLDEN[0])
        assert wire.sent and backend.calls() >= 1
        for _, headers, body in wire.sent:
            assert longest_digit_run(body.decode()) <= 4
            assert KEY not in body.decode()
        assert any(KEY in v for _, h, _ in wire.sent for v in h.values())  # the key reaches the provider adapter's header, and only there
        doc = json.loads((tmp_path / "out" / "recordings" / "rec-0001.json").read_text())
        assert doc["case_id"] == "G-A" and doc["status"] == 500

    run(go())

def test_a_live_backend_without_the_key_does_not_build(tmp_path: Path) -> None:
    from noema_model.platform.config import ConfigError
    with pytest.raises(ConfigError):
        build_backend(False, {}, BudgetGuard(limit_micros=1), recorder(tmp_path), args(), inner=Wire())


def test_the_pin_is_the_configuration_the_run_used_and_holds_no_key(tmp_path: Path) -> None:
    b = build_backend(False, ENV, BudgetGuard(limit_micros=1), recorder(tmp_path), args(), inner=Wire())
    pin = b.pin()
    assert pin["pinned_model_id"] == "gpt-6-luna" and pin["rate_card"]["input_micro_per_mtok"] == 100_000 and pin["max_output_tokens"] == 4_000
    assert pin["harness_config_sha256"] and pin["pack_ref"] == "fin@1.0.0"
    assert KEY not in json.dumps(pin)
