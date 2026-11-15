"""The cost-file writer (REQ-NFR-007, REQ-TST-006, INV-22; Architect's ruling A55 of 8 Nov 2026 on noema#293).

It writes each model call's record to <out>/cost/<correlation_id>-<NNN>.json and changes nothing else. It sits in the benchmark tree and is handed to the model
service from outside; the service reads no benchmark variable. Faults come from real files, not mocks (AGENT-12). All data is SYNTHETIC."""

import json
import tempfile
import unittest
from collections.abc import Mapping
from pathlib import Path

from benchmark.model_cost.cost_files import (
    COST_EVENT, MAX_CALLS_PER_CORRELATION, MAX_CORRELATION_ID_LENGTH, CostFileRefused, CostFileTelemetry,
)


class Recorder:
    """A real, in-memory implementation of the four telemetry methods; it keeps everything it is given."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, tuple[object, ...]]] = []

    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None:
        self.calls.append(("count", (name, tuple(sorted(labels.items())), value)))

    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None:
        self.calls.append(("observe", (name, tuple(sorted(labels.items())), value)))

    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None:
        self.calls.append(("event", (name, correlation_id, tuple(sorted(fields.items())))))

    def span(self, name: str, correlation_id: str, attributes: Mapping[str, object]) -> str:
        self.calls.append(("span", (name, correlation_id)))
        return "span"


def tmp() -> Path:
    holder = tempfile.TemporaryDirectory()
    TestCase.holders.append(holder)
    return Path(holder.name)


class TestCase(unittest.TestCase):
    holders: list[tempfile.TemporaryDirectory[str]] = []

    @classmethod
    def tearDownClass(cls) -> None:
        for holder in cls.holders:
            holder.cleanup()
        cls.holders.clear()

    def test_a_cost_event_writes_exactly_its_record(self) -> None:
        out = tmp()
        telemetry = CostFileTelemetry(Recorder(), out)
        telemetry.event(COST_EVENT, "corr_a", {"record": '{"cost":1}'})
        (written,) = (out / "cost").glob("*.json")
        self.assertEqual(json.loads(written.read_text()), {"cost": 1})
        self.assertTrue(written.name.startswith("corr_a-"))

    def test_everything_is_passed_through_to_the_wrapped_telemetry_unchanged(self) -> None:
        plain, wrapped = Recorder(), Recorder()
        out = tmp()
        for target in (plain, CostFileTelemetry(wrapped, out)):
            target.count("noema_x_total", {"a": "b"}, 2)
            target.observe("noema_y_ms", {"a": "b"}, 7)
            target.event("OTHER_EVENT", "corr_a", {"k": "v"})
            target.event(COST_EVENT, "corr_a", {"record": "{}"})
            target.span("s", "corr_a", {})
        self.assertEqual(wrapped.calls, plain.calls)  # the decisions the service sees do not depend on the writer
        self.assertEqual(len(list((out / "cost").glob("*.json"))), 1)  # only the cost event wrote a file

    def test_each_call_gets_its_own_file_and_none_is_overwritten(self) -> None:
        out = tmp()
        first, second = CostFileTelemetry(Recorder(), out), CostFileTelemetry(Recorder(), out)
        for target in (first, first, second):
            target.event(COST_EVENT, "corr_a", {"record": "{}"})
        self.assertEqual(sorted(p.name for p in (out / "cost").glob("*.json")), ["corr_a-001.json", "corr_a-002.json", "corr_a-003.json"])

    def test_a_correlation_id_outside_the_allowed_characters_is_refused_not_normalised(self) -> None:
        out = tmp()
        for bad in ("../../escape", "a/b", "a b", "a.b", "a\\b", "é", "a\x00b", "a\nb", "", "x" * (MAX_CORRELATION_ID_LENGTH + 1)):
            with self.assertRaises(CostFileRefused, msg=repr(bad)):
                CostFileTelemetry(Recorder(), out).event(COST_EVENT, bad, {"record": "{}"})
        self.assertFalse(list(out.glob("**/*.json")))

    def test_the_thousandth_call_for_one_id_is_refused(self) -> None:
        out = tmp()
        telemetry = CostFileTelemetry(Recorder(), out)
        for _ in range(MAX_CALLS_PER_CORRELATION):
            telemetry.event(COST_EVENT, "corr_a", {"record": "{}"})
        with self.assertRaises(CostFileRefused):
            telemetry.event(COST_EVENT, "corr_a", {"record": "{}"})

    # INV-22: the writer reaches nothing outside <out>/cost, whatever the directory is.
    def test_a_cost_directory_that_is_a_link_is_refused_and_nothing_reaches_the_target(self) -> None:
        base = tmp()
        custody, out = base / "custody", base / "out"
        custody.mkdir()
        out.mkdir()
        (out / "cost").symlink_to(custody, target_is_directory=True)
        with self.assertRaises(CostFileRefused):
            CostFileTelemetry(Recorder(), out).event(COST_EVENT, "corr_inv22", {"record": "{}"})
        self.assertFalse(list(custody.iterdir()))

    def test_an_out_directory_that_is_a_link_is_refused_too(self) -> None:
        base = tmp()
        custody = base / "custody"
        custody.mkdir()
        (base / "out").symlink_to(custody, target_is_directory=True)
        with self.assertRaises(CostFileRefused):
            CostFileTelemetry(Recorder(), base / "out").event(COST_EVENT, "corr_inv22", {"record": "{}"})
        self.assertFalse(list(custody.iterdir()))


if __name__ == "__main__":
    unittest.main()
