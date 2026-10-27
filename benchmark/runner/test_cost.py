"""The runner reads provider cost from the model service's bench cost files (REQ-NFR-007; noema#62). SYNTHETIC records.

The file layout is the service's own: `$BENCH_OUT/cost/<correlation_id>-<NNN>.json`, each the JSON of `CallRecord.to_dict()`
(services/model/noema_model/harness/accounting.py), with the correlation id's characters outside A-Z a-z 0-9 _ - replaced by `_` and cut to 128.
The runner does not import services/**: these records are written by hand to that shape. A cost that is not known is null, never zero or a guess."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.runner.cost import CostFiles

Json = dict[str, Any]


def record(total: Any = 1200, *, currency: Any = "USD", complete: Any = True) -> Json:
    """A CallRecord.to_dict() with the fields the runner reads; the rest is present so the shape is the service's."""
    return {"route": "understanding.extract", "template": {"id": "t1", "version": 1, "digest": "sha256:" + "0" * 64}, "input_digest": "sha256:" + "1" * 64,
            "truncated_fields": [], "provider": "alpha", "model_version": "m-1", "latency_ms": 120, "tokens_in": 100, "tokens_out": 20,
            "cost": {"total_micro": total, "currency": currency, "rate_as_of": "2026-10-01", "complete": complete}, "attempts": []}


class Reader(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.costs = CostFiles(self.root / "cost")

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def put(self, name: str, body: Any) -> None:
        directory = self.root / "cost"
        directory.mkdir(exist_ok=True)
        (directory / name).write_text(body if isinstance(body, str) else json.dumps(body), encoding="utf-8")

    def test_no_directory_configured_is_not_available(self) -> None:
        self.assertIsNone(CostFiles(None).cost_usd_micros("corr_1"))

    def test_a_missing_directory_is_not_available(self) -> None:
        self.assertIsNone(self.costs.cost_usd_micros("corr_1"))

    def test_no_file_for_the_correlation_is_not_available_not_zero(self) -> None:
        self.put("corr_2-001.json", record())
        self.assertIsNone(self.costs.cost_usd_micros("corr_1"))

    def test_one_complete_usd_record_is_its_total(self) -> None:
        self.put("corr_1-001.json", record(1200))
        self.assertEqual(self.costs.cost_usd_micros("corr_1"), 1200)

    def test_every_call_of_the_correlation_is_summed(self) -> None:
        self.put("corr_1-001.json", record(1200))
        self.put("corr_1-002.json", record(300))
        self.put("corr_1-003.json", record(0))
        self.assertEqual(self.costs.cost_usd_micros("corr_1"), 1500)

    def test_another_correlation_with_the_same_prefix_is_not_counted(self) -> None:
        self.put("corr_1-001.json", record(1200))
        self.put("corr_10-001.json", record(999))
        self.put("corr_1-001-001.json", record(777))  # the correlation id "corr_1-001"
        self.assertEqual(self.costs.cost_usd_micros("corr_1"), 1200)
        self.assertEqual(self.costs.cost_usd_micros("corr_1-001"), 777)

    def test_the_file_name_follows_the_writers_rule_for_odd_ids(self) -> None:
        self.put("a_b_c-001.json", record(5))
        self.assertEqual(self.costs.cost_usd_micros("a/b c"), 5)
        long_id = "x" * 300
        self.put("x" * 128 + "-001.json", record(6))
        self.assertEqual(self.costs.cost_usd_micros(long_id), 6)
        self.put("call-001.json", record(7))
        self.assertEqual(self.costs.cost_usd_micros(""), 7)

    def test_an_id_cannot_reach_outside_the_directory(self) -> None:
        (self.root / "secret-001.json").write_text(json.dumps(record(1)))
        self.put("corr_1-001.json", record(2))
        self.assertIsNone(self.costs.cost_usd_micros("../secret"))

    def test_an_incomplete_record_makes_the_case_not_available(self) -> None:
        self.put("corr_1-001.json", record(1200))
        self.put("corr_1-002.json", record(300, complete=False))
        self.assertIsNone(self.costs.cost_usd_micros("corr_1"), "a partial sum would mislead")

    def test_an_unknown_total_is_not_available(self) -> None:
        self.put("corr_1-001.json", record(None))
        self.assertIsNone(self.costs.cost_usd_micros("corr_1"))

    def test_a_currency_other_than_usd_is_not_converted(self) -> None:
        """cost_usd_micros is millionths of a US dollar; there is no rate to convert from another currency and none is invented."""
        self.put("corr_1-001.json", record(1200, currency="NGN"))
        self.assertIsNone(self.costs.cost_usd_micros("corr_1"))
        self.put("corr_2-001.json", record(1200, currency=None))
        self.assertIsNone(self.costs.cost_usd_micros("corr_2"))

    def test_anything_that_is_not_a_non_negative_integer_is_not_available(self) -> None:
        for n, bad in enumerate((12.5, "1200", -1, True, [1])):
            self.put(f"c{n}-001.json", record(bad))
            self.assertIsNone(self.costs.cost_usd_micros(f"c{n}"), repr(bad))

    def test_a_malformed_or_wrongly_shaped_file_is_not_available(self) -> None:
        self.put("m1-001.json", "{not json")
        self.put("m2-001.json", "[]")
        self.put("m3-001.json", json.dumps({"cost": []}))
        self.put("m4-001.json", json.dumps({}))
        self.put("m5-001.json", "")
        for cid in ("m1", "m2", "m3", "m4", "m5"):
            self.assertIsNone(self.costs.cost_usd_micros(cid), cid)

    def test_a_float_in_the_file_is_not_read_as_an_integer(self) -> None:
        self.put("f1-001.json", '{"cost": {"total_micro": 1200.0, "currency": "USD", "complete": true}}')
        self.assertIsNone(self.costs.cost_usd_micros("f1"))

    def test_a_directory_named_like_a_record_is_ignored(self) -> None:
        (self.root / "cost").mkdir()
        (self.root / "cost" / "corr_1-001.json").mkdir()
        self.assertIsNone(self.costs.cost_usd_micros("corr_1"))


if __name__ == "__main__":
    unittest.main()
