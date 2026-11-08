"""Bench-only cost files: each model call's CallRecord.to_dict(), written to $BENCH_OUT/cost/<call_id>.json (REQ-NFR-007).

Faults come from the real harness and the real files, not from mocks (AGENT-12)."""

import json
from pathlib import Path

import pytest

from noema_model.platform.telemetry import (
    COST_EVENT, MAX_CALLS_PER_CORRELATION, MAX_CORRELATION_ID_LENGTH, CostFileRefused, CostFileTelemetry, InMemoryTelemetry, NullTelemetry,
    cost_files_from_environment,
)
from tests.test_harness_call import Out, TEMPLATE, good, request, version  # noqa: F401
from noema_model.harness import Harness
from tests.support import ManualClock, ScriptedProvider, make_config, run
from typing import Any


def harness_with(telemetry: Any) -> Harness:
    return Harness(make_config(providers=("alpha",)), {"alpha": ScriptedProvider("alpha", [good("alpha"), good("alpha")])},
                   ManualClock(), telemetry)


def test_without_bench_out_the_telemetry_is_returned_unchanged(tmp_path: Path) -> None:
    inner = InMemoryTelemetry()
    assert cost_files_from_environment(inner, {}) is inner
    assert cost_files_from_environment(inner, {"BENCH_OUT": ""}) is inner
    assert not list(tmp_path.iterdir())


def test_with_bench_out_a_call_writes_exactly_its_call_record(tmp_path: Path) -> None:
    inner = InMemoryTelemetry()
    telemetry = cost_files_from_environment(inner, {"BENCH_OUT": str(tmp_path)})
    call = run(harness_with(telemetry).generate(request())).call
    files = sorted((tmp_path / "cost").glob("*.json"))
    assert len(files) == 1
    assert json.loads(files[0].read_text()) == call.to_dict()
    assert files[0].name.startswith(request().correlation_id)
    # the other telemetry still sees its events and metrics
    assert inner.total("noema_provider_call_total") >= 1


def test_each_call_gets_its_own_file_and_none_is_overwritten(tmp_path: Path) -> None:
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    harness = harness_with(telemetry)
    run(harness.generate(request()))
    run(harness.generate(request()))
    # a second process with the same correlation id must not overwrite the first one's files
    run(harness_with(CostFileTelemetry(NullTelemetry(), tmp_path)).generate(request()))
    assert len(list((tmp_path / "cost").glob("*.json"))) == 3


@pytest.mark.parametrize("bad", ["../../escape", "a/b", "a b", "a.b", "a\\b", "é", "a\x00b", "a\nb", "", "x" * (MAX_CORRELATION_ID_LENGTH + 1)])
def test_a_correlation_id_outside_the_allowed_characters_is_refused_not_normalised(tmp_path: Path, bad: str) -> None:
    """Decided: only [A-Za-z0-9_-], 1 to MAX_CORRELATION_ID_LENGTH characters. Nothing is rewritten, cut or written."""
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path / "out")
    with pytest.raises(CostFileRefused):
        telemetry.event(COST_EVENT, bad, {"record": "{}"})
    assert not (tmp_path / "escape").exists()
    assert not (tmp_path / "out" / "cost").exists() or not list((tmp_path / "out" / "cost").iterdir())


def test_two_ids_that_would_collide_under_normalisation_cannot_both_be_written(tmp_path: Path) -> None:
    """`a/b`, `a b` and `a.b` would all have become `a_b`. Only the id that is already a file name is accepted."""
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    telemetry.event(COST_EVENT, "a_b", {"record": '{"id": "a_b"}'})
    for other in ("a/b", "a b", "a.b"):
        with pytest.raises(CostFileRefused):
            telemetry.event(COST_EVENT, other, {"record": '{"id": "other"}'})
    assert [p.name for p in (tmp_path / "cost").iterdir()] == ["a_b-001.json"]
    assert json.loads((tmp_path / "cost" / "a_b-001.json").read_text()) == {"id": "a_b"}


def test_ids_that_differ_only_after_a_long_prefix_do_not_share_a_file(tmp_path: Path) -> None:
    """The old writer cut ids at 128 characters; two long ids then shared a name. Nothing is cut now."""
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    one, two = "x" * 130 + "1", "x" * 130 + "2"
    telemetry.event(COST_EVENT, one, {"record": '{"n": 1}'})
    telemetry.event(COST_EVENT, two, {"record": '{"n": 2}'})
    assert sorted(p.name for p in (tmp_path / "cost").iterdir()) == [f"{one}-001.json", f"{two}-001.json"]


def test_the_longest_allowed_id_gives_a_file_name_the_file_system_accepts(tmp_path: Path) -> None:
    longest = "y" * MAX_CORRELATION_ID_LENGTH
    CostFileTelemetry(NullTelemetry(), tmp_path).event(COST_EVENT, longest, {"record": "{}"})
    assert (tmp_path / "cost" / f"{longest}-001.json").is_file()


def test_an_id_that_is_a_prefix_of_another_never_shares_a_file_name(tmp_path: Path) -> None:
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    for cid in ("a", "a-001", "a-001-001"):
        telemetry.event(COST_EVENT, cid, {"record": json.dumps({"id": cid})})
    names = sorted(p.name for p in (tmp_path / "cost").iterdir())
    assert names == sorted(["a-001.json", "a-001-001.json", "a-001-001-001.json"])
    assert json.loads((tmp_path / "cost" / "a-001.json").read_text()) == {"id": "a"}


def test_many_calls_under_one_correlation_id_take_001_upwards(tmp_path: Path) -> None:
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    for n in range(1, 6):
        telemetry.event(COST_EVENT, "corr_1", {"record": json.dumps({"call": n})})
    names = sorted(p.name for p in (tmp_path / "cost").iterdir())
    assert names == [f"corr_1-{n:03d}.json" for n in range(1, 6)]
    assert [json.loads((tmp_path / "cost" / name).read_text())["call"] for name in names] == [1, 2, 3, 4, 5]


def test_the_1000th_call_for_one_id_is_refused_and_the_field_is_not_widened(tmp_path: Path) -> None:
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    assert MAX_CALLS_PER_CORRELATION == 999
    for n in range(1, 1000):
        telemetry.event(COST_EVENT, "corr_1", {"record": json.dumps({"call": n})})
    before = {p.name: p.read_text() for p in (tmp_path / "cost").iterdir()}
    assert len(before) == 999 and "corr_1-999.json" in before and "corr_1-001.json" in before
    with pytest.raises(CostFileRefused):
        telemetry.event(COST_EVENT, "corr_1", {"record": '{"call": 1000}'})
    assert {p.name: p.read_text() for p in (tmp_path / "cost").iterdir()} == before, "nothing added, nothing changed"
    telemetry.event(COST_EVENT, "corr_2", {"record": "{}"})  # another id is unaffected
    assert (tmp_path / "cost" / "corr_2-001.json").is_file()


def test_a_file_that_exists_is_never_overwritten(tmp_path: Path) -> None:
    cost = tmp_path / "cost"
    cost.mkdir()
    (cost / "corr_1-001.json").write_text("SENTINEL-1")
    (cost / "corr_1-003.json").write_text("SENTINEL-3")
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    telemetry.event(COST_EVENT, "corr_1", {"record": '{"new": 1}'})
    telemetry.event(COST_EVENT, "corr_1", {"record": '{"new": 2}'})
    assert (cost / "corr_1-001.json").read_text() == "SENTINEL-1"
    assert (cost / "corr_1-003.json").read_text() == "SENTINEL-3"
    assert json.loads((cost / "corr_1-002.json").read_text()) == {"new": 1}
    assert json.loads((cost / "corr_1-004.json").read_text()) == {"new": 2}


def test_a_full_set_made_by_another_process_also_refuses_the_next_call(tmp_path: Path) -> None:
    cost = tmp_path / "cost"
    cost.mkdir()
    for n in range(1, 1000):
        (cost / f"corr_9-{n:03d}.json").write_text(f"old-{n}")
    with pytest.raises(CostFileRefused):
        CostFileTelemetry(NullTelemetry(), tmp_path).event(COST_EVENT, "corr_9", {"record": "{}"})
    assert len(list(cost.iterdir())) == 999
    assert (cost / "corr_9-500.json").read_text() == "old-500"


def test_other_events_write_nothing(tmp_path: Path) -> None:
    CostFileTelemetry(NullTelemetry(), tmp_path).event("PROVIDER_FAILOVER", "corr_1", {"route": "x"})
    assert not (tmp_path / "cost").exists()


def test_a_write_failure_is_loud_not_silent(tmp_path: Path) -> None:
    blocked = tmp_path / "file"
    blocked.write_text("x")
    with pytest.raises(OSError):
        CostFileTelemetry(NullTelemetry(), blocked).event(COST_EVENT, "corr_1", {"record": "{}"})


def test_events_and_spans_still_reach_the_wrapped_telemetry(tmp_path: Path) -> None:
    inner = InMemoryTelemetry()
    telemetry = CostFileTelemetry(inner, tmp_path)
    telemetry.event("PROVIDER_FAILOVER", "corr_1", {"route": "x"})
    telemetry.event(COST_EVENT, "corr_1", {"record": "{}"})
    with telemetry.span("harness.call", "corr_1", {"route": "x"}):
        pass
    assert [e[0] for e in inner.events] == ["PROVIDER_FAILOVER", COST_EVENT]
    assert inner.spans == [("harness.call", "corr_1", {"route": "x"})]


def test_the_file_holds_the_record_text_unchanged(tmp_path: Path) -> None:
    CostFileTelemetry(NullTelemetry(), tmp_path).event(COST_EVENT, "corr_1", {"record": '{"a":1}'})
    assert (tmp_path / "cost" / "corr_1-001.json").read_text() == '{"a":1}'


# Architect's ruling of 8 Nov 2026 on noema#293 (item 2): BENCH_OUT is accepted as an observability-only exception for the POC on two conditions, tested here.
# REQ-TST-006, REQ-NFR-007, INV-22.

def test_decisions_are_identical_with_and_without_bench_out(tmp_path: Path) -> None:
    """The cost files add output and nothing else: the call a model request gets back, and everything the wrapped telemetry sees, are the same."""
    plain_inner = InMemoryTelemetry()
    plain = run(harness_with(cost_files_from_environment(plain_inner, {})).generate(request())).call
    wrapped_inner = InMemoryTelemetry()
    wrapped = run(harness_with(cost_files_from_environment(wrapped_inner, {"BENCH_OUT": str(tmp_path)})).generate(request())).call
    assert wrapped.to_dict() == plain.to_dict()
    assert wrapped_inner.counts == plain_inner.counts
    assert wrapped_inner.observations == plain_inner.observations
    assert wrapped_inner.events == plain_inner.events
    assert wrapped_inner.spans == plain_inner.spans
    assert not list(tmp_path.glob("**/*.tmp"))
    assert [p.name for p in (tmp_path / "cost").iterdir()] and not any(p.is_dir() for p in (tmp_path / "cost").iterdir())


def test_cost_files_are_written_only_under_bench_out_cost_and_never_through_a_link(tmp_path: Path) -> None:
    """A path that leaves $BENCH_OUT/cost is refused, so the writer can never reach held-out custody (INV-22), even if the directory is a link into it."""
    custody = tmp_path / "custody"
    custody.mkdir()
    out = tmp_path / "out"
    out.mkdir()
    (out / "cost").symlink_to(custody, target_is_directory=True)
    telemetry = CostFileTelemetry(InMemoryTelemetry(), out)
    with pytest.raises(CostFileRefused):
        telemetry.event(COST_EVENT, "corr_inv22", {"record": "{}"})
    assert not list(custody.iterdir())
    for hostile in ("../corr", "a/b", "..", "/etc/passwd", "corr\x00x"):
        with pytest.raises(CostFileRefused):
            CostFileTelemetry(InMemoryTelemetry(), tmp_path / "ok").event(COST_EVENT, hostile, {"record": "{}"})
    assert not (tmp_path / "corr").exists()


def test_the_bench_compose_override_mounts_benchmark_output_and_no_held_out_custody() -> None:
    text = (Path(__file__).resolve().parents[3] / "deploy" / "compose" / "bench-model.yml").read_text()
    live = [line for line in text.splitlines() if line.strip() and not line.lstrip().startswith("#")]
    volumes = [line for line in live if line.lstrip().startswith("- ") and ":" in line]
    assert volumes == ["      - ${NOEMA_BENCH_OUT:?set by make bench-model-up}:/bench-out"]
    assert not any("held" in line.lower() for line in live)
