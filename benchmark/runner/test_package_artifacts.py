"""BEN-05 / A444 (rulings of 20 Nov): the runner's oracle client gives per-case write counts (aggregates only), and the dispatch SHA and the run id are stamped into every artifact. Written before
`package_artifacts.py` and seen failing. SYNTHETIC.

`write_counts` sums, per case, the `count` of every effect in the case result's `environment.effects` (what the case system really applied, read from the truth listener by the oracle client:
`benchmark/oracle_client/effects.py`). It reads no oracle label and no expectation, writes counts only, and fails closed: a case whose result has no environment block, a malformed effect or a
duplicate result has no count, never a zero. The stamped files are the ones the G1 package reads (`benchmark/suites/g1_package.py`)."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.runner.package_artifacts import StampError, main, stamp_doc, stamp_probes, write_counts

Json = dict[str, Any]
SHA = "a" * 40
RUN = "g1-20261120"


def result(case_id: str, effects: Any = None, env: bool = True) -> Json:
    return {"run_id": "g1-a", "case_id": case_id, "case_version": 1, "status": "OK", "environment": {"effects": effects if effects is not None else [], "oracle_accesses_by_runtime": 0,
                                                                                                         "model_governance_changes": 0, "monetary_calls": 0} if env else None}


def write_run(root: Path, results: list[Json]) -> Path:
    (root / "results").mkdir()
    for i, r in enumerate(results):
        (root / "results" / f"{i:03d}.json").write_text(json.dumps(r), encoding="utf-8")
    return root


def new_run(tmp: Path, results: list[Json]) -> Path:
    run = tmp / "run"
    run.mkdir()
    return write_run(run, results)


class Counts(unittest.TestCase):
    def test_a_case_with_no_effect_has_a_count_of_zero_and_one_write_has_a_count_of_one(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = write_run(Path(tmp), [result("BEN-CLEAN-0001"), result("BEN-TMO-0001", [{"capability": "fin.cap.create_escalation_case", "target": "fin.transaction:TX1", "count": 1}])])
            self.assertEqual({"BEN-CLEAN-0001": 0, "BEN-TMO-0001": 1}, write_counts(run))

    def test_the_counts_of_several_effects_add_up(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = write_run(Path(tmp), [result("BEN-A-0001", [{"capability": "c1", "target": "t1", "count": 1}, {"capability": "c2", "target": "t2", "count": 2}])])
            self.assertEqual({"BEN-A-0001": 3}, write_counts(run))

    def test_a_result_with_no_environment_block_has_no_count_never_a_zero(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = write_run(Path(tmp), [result("BEN-CLEAN-0001"), result("BEN-TMO-0001", env=False)])
            counts = write_counts(run)
            self.assertEqual({"BEN-CLEAN-0001": 0}, counts)
            self.assertNotIn("BEN-TMO-0001", counts)

    def test_a_malformed_effect_or_count_makes_the_case_uncounted(self) -> None:
        bad: list[Any] = [[{"capability": "c", "target": "t"}], [{"capability": "c", "target": "t", "count": True}], [{"capability": "c", "target": "t", "count": -1}],
                          [{"capability": "c", "target": "t", "count": "1"}], [{"capability": "c", "target": "t", "count": 1.5}], ["x"], [None], "x", {"count": 1}]
        for effects in bad:
            with tempfile.TemporaryDirectory() as tmp:
                run = write_run(Path(tmp), [result("BEN-CLEAN-0001"), result("BEN-TMO-0001", effects)])
                counts = write_counts(run)
                self.assertNotIn("BEN-TMO-0001", counts, str(effects))
                self.assertEqual(0, counts["BEN-CLEAN-0001"])

    def test_a_case_with_two_results_has_no_count(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = write_run(Path(tmp), [result("BEN-TMO-0001", [{"capability": "c", "target": "t", "count": 1}]), result("BEN-TMO-0001", [{"capability": "c", "target": "t", "count": 1}]),
                                        result("BEN-CLEAN-0001")])
            self.assertEqual({"BEN-CLEAN-0001": 0}, write_counts(run))

    def test_an_unreadable_result_file_is_an_error_not_a_skipped_case(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = write_run(Path(tmp), [result("BEN-CLEAN-0001")])
            (run / "results" / "999.json").write_text("not json", encoding="utf-8")
            with self.assertRaises(ValueError):
                write_counts(run)
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):  # no results directory at all
                write_counts(Path(tmp))
        with tempfile.TemporaryDirectory() as tmp:
            run = write_run(Path(tmp), [])
            with self.assertRaises(ValueError):  # an empty run is not a run with zero writes
                write_counts(run)

    def test_a_result_with_no_case_id_is_an_error_because_it_cannot_be_attributed(self) -> None:
        doc: Any
        for doc in ({"environment": {"effects": []}}, {"case_id": "", "environment": {"effects": []}}, {"case_id": 7}, []):
            with tempfile.TemporaryDirectory() as tmp:
                run = write_run(Path(tmp), [result("BEN-CLEAN-0001")])
                (run / "results" / "999.json").write_text(json.dumps(doc), encoding="utf-8")
                with self.assertRaises(ValueError, msg=str(doc)):
                    write_counts(run)

    def test_the_counts_carry_no_oracle_label_or_case_content(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = write_run(Path(tmp), [result("BEN-TMO-0001", [{"capability": "c", "target": "fin.transaction:TX1", "count": 1}])])
            text = json.dumps(stamp_doc("writes", write_counts(run), SHA, RUN))
            self.assertNotIn("fin.transaction", text)
            self.assertNotIn("capability", text)


class Stamps(unittest.TestCase):
    def test_the_stamped_document_has_the_shape_the_package_reads(self) -> None:
        self.assertEqual({"stamp": {"sha": SHA, "run_id": RUN}, "writes": {"BEN-A-0001": 1}}, stamp_doc("writes", {"BEN-A-0001": 1}, SHA, RUN))

    def test_the_sha_and_the_run_id_are_checked(self) -> None:
        for sha, run in ((SHA[:39], RUN), (SHA.upper(), RUN), ("", RUN), (SHA, ""), (SHA, "a b"), (SHA, "x" * 65), (SHA, "a/b"), (SHA, "a\nb")):
            with self.assertRaises(StampError, msg=f"{sha!r} {run!r}"):
                stamp_doc("writes", {}, sha, run)

    def test_the_probes_file_gets_a_top_level_stamp_and_keeps_its_content(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "negative-probes.json"
            path.write_text(json.dumps({"met": True, "cases": [{"case_id": "BEN-A-0001"}]}), encoding="utf-8")
            stamp_probes(path, SHA, RUN)
            doc = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual({"sha": SHA, "run_id": RUN}, doc["stamp"])
            self.assertEqual({"met": True, "cases": [{"case_id": "BEN-A-0001"}]}, {k: v for k, v in doc.items() if k != "stamp"})
            stamp_probes(path, SHA, RUN)  # the same stamp again is fine
            with self.assertRaises(StampError):
                stamp_probes(path, SHA, "g1-other")  # another run's stamp is never overwritten

    def test_a_probes_file_that_is_not_an_object_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "negative-probes.json"
            for text in ("[]", "null", "7", "not json"):
                path.write_text(text, encoding="utf-8")
                with self.assertRaises(ValueError, msg=text):
                    stamp_probes(path, SHA, RUN)
            with self.assertRaises(ValueError):
                stamp_probes(Path(tmp) / "absent.json", SHA, RUN)


class Cli(unittest.TestCase):
    def test_it_writes_the_stamp_file_and_the_stamped_counts_and_exits_zero(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-CLEAN-0001"), result("BEN-TMO-0001", [{"capability": "c", "target": "t", "count": 1}])])
            code = main(["--run", str(run), "--sha", SHA, "--run-id", RUN])
            self.assertEqual(0, code)
            self.assertEqual({"sha": SHA, "run_id": RUN}, json.loads((run / "stamp.json").read_text(encoding="utf-8")))
            doc = json.loads((run / "mock-case-writes.json").read_text(encoding="utf-8"))
            self.assertEqual({"stamp": {"sha": SHA, "run_id": RUN}, "writes": {"BEN-CLEAN-0001": 0, "BEN-TMO-0001": 1}}, doc)

    def test_it_exits_two_and_writes_nothing_on_a_bad_stamp_or_an_unreadable_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-CLEAN-0001")])
            self.assertEqual(2, main(["--run", str(run), "--sha", "short", "--run-id", RUN]))
            self.assertFalse((run / "stamp.json").exists())
            self.assertFalse((run / "mock-case-writes.json").exists())
            self.assertEqual(2, main(["--run", str(Path(tmp) / "absent"), "--sha", SHA, "--run-id", RUN]))

    def test_it_exits_one_when_a_case_could_not_be_counted_but_still_writes_the_rest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-CLEAN-0001"), result("BEN-TMO-0001", env=False)])
            self.assertEqual(1, main(["--run", str(run), "--sha", SHA, "--run-id", RUN]))
            doc = json.loads((run / "mock-case-writes.json").read_text(encoding="utf-8"))
            self.assertEqual({"BEN-CLEAN-0001": 0}, doc["writes"])

    def test_it_stamps_the_probes_file_when_asked(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-CLEAN-0001")])
            probes = Path(tmp) / "negative-probes.json"
            probes.write_text(json.dumps({"met": True, "cases": []}), encoding="utf-8")
            self.assertEqual(0, main(["--run", str(run), "--sha", SHA, "--run-id", RUN, "--probes", str(probes)]))
            self.assertEqual({"sha": SHA, "run_id": RUN}, json.loads(probes.read_text(encoding="utf-8"))["stamp"])


if __name__ == "__main__":
    unittest.main()
