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

from benchmark.runner.package_artifacts import StampError, append_probe_requests, main, stamp_doc, stamp_probes, stamp_reconstructions, write_counts

Json = dict[str, Any]
SHA = "a" * 40
RUN = "g1-20261120"


def result(case_id: str, effects: Any = None, env: bool = True) -> Json:
    return {"run_id": "g1-a", "case_id": case_id, "case_version": 1, "status": "OK", "environment": {"effects": effects if effects is not None else [], "oracle_accesses_by_runtime": 0,
                                                                                                         "model_governance_changes": 0, "monetary_calls": 0} if env else None}


def write_run(root: Path, results: list[Json], reconstructions: Any = None) -> Path:
    (root / "reconstructions.json").write_text(json.dumps({} if reconstructions is None else reconstructions), encoding="utf-8")
    (root / "results").mkdir()
    for i, r in enumerate(results):
        (root / "results" / f"{i:03d}.json").write_text(json.dumps(r), encoding="utf-8")
    return root


def new_run(tmp: Path, results: list[Json], reconstructions: Any = None) -> Path:
    run = tmp / "run"
    run.mkdir()
    return write_run(run, results, reconstructions)


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


class Reconstructions(unittest.TestCase):
    """The runner writes `reconstructions.json` as `{case: {"initial": <body>}}`; the package reads `{"stamp": {...}, "cases": {...}}`."""

    def test_the_runners_file_is_wrapped_with_the_stamp_and_keeps_its_content(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [], {"BEN-A-0001": {"initial": {"events": [], "sections": []}}})
            stamp_reconstructions(run, SHA, RUN)
            self.assertEqual({"stamp": {"sha": SHA, "run_id": RUN}, "cases": {"BEN-A-0001": {"initial": {"events": [], "sections": []}}}},
                             json.loads((run / "reconstructions.json").read_text(encoding="utf-8")))

    def test_stamping_twice_with_the_same_stamp_changes_nothing_and_another_stamp_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [], {"BEN-A-0001": {"initial": {}}})
            stamp_reconstructions(run, SHA, RUN)
            first = (run / "reconstructions.json").read_bytes()
            stamp_reconstructions(run, SHA, RUN)
            self.assertEqual(first, (run / "reconstructions.json").read_bytes())
            with self.assertRaises(StampError):
                stamp_reconstructions(run, SHA, "g1-other")

    def test_a_stamped_wrapper_with_an_extra_key_or_no_cases_is_refused_even_with_the_same_stamp(self) -> None:
        stamp = {"sha": SHA, "run_id": RUN}
        for doc in ({"stamp": stamp, "cases": {}, "extra": 1}, {"stamp": stamp}, {"stamp": stamp, "other": {}}):
            with tempfile.TemporaryDirectory() as tmp:
                run = new_run(Path(tmp), [], doc)
                with self.assertRaises(StampError, msg=str(doc)):
                    stamp_reconstructions(run, SHA, RUN)

    def test_a_missing_or_unreadable_or_non_object_file_is_an_error_never_an_empty_stamped_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [])
            (run / "reconstructions.json").unlink()
            with self.assertRaises(ValueError):
                stamp_reconstructions(run, SHA, RUN)
            self.assertFalse((run / "reconstructions.json").exists())
            for text in ("[]", "null", "7", "not json"):
                (run / "reconstructions.json").write_text(text, encoding="utf-8")
                with self.assertRaises(ValueError, msg=text):
                    stamp_reconstructions(run, SHA, RUN)

    def test_a_file_that_has_a_case_called_stamp_is_still_treated_as_the_runners_flat_file_only_when_unstamped(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [], {"stamp": {"initial": {}}})  # a case id cannot be "stamp"; a wrapper must have both keys
            with self.assertRaises(StampError):
                stamp_reconstructions(run, SHA, RUN)


def probe_row(case_id: str, corr: str, control: int | None = 200, wrong: int | None = 404, foreign: int | None = 401, unknown: int | None = 404) -> Json:
    return {"case_id": case_id, "principal_id": "p1", "correlation_id": corr, "control": {"status": control, "ok": True}, "wrong_role": {"status": wrong, "ok": True},
            "foreign_tenant": {"status": foreign, "ok": True}, "unknown_correlation": {"status": unknown, "ok": True}, "ok": True}


def jsonl(path: Path) -> list[Json]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines()]


class ProbeRequests(unittest.TestCase):
    """The evidence probes read the reconstruction route too, so the core's RECORD_ACCESSED log holds their lines. The run's own record must hold the same reads, or noema#730's count check fails a
    correct run: the control read (an allowed role, answered 200) is a `reconstruction` request of the case's correlation; the three refusal checks are `probe` requests, which add nothing."""

    def setup(self, tmp: str, rows: list[Json], existing: list[Json] | None = None) -> tuple[Path, Path]:
        run = Path(tmp)
        (run / "requests.jsonl").write_text("".join(json.dumps(x) + "\n" for x in (existing or [])), encoding="utf-8")
        probes = run / "negative-probes.json"
        probes.write_text(json.dumps({"met": True, "cases": rows}), encoding="utf-8")
        return run, probes

    def test_each_probed_case_adds_its_control_read_as_a_reconstruction_and_three_probes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run, probes = self.setup(tmp, [probe_row("BEN-A-0001", "corr_a")])
            append_probe_requests(run, probes)
            got = jsonl(run / "requests.jsonl")
        self.assertEqual([("reconstruction", 200, "corr_a"), ("probe", 404, "corr_a"), ("probe", 401, "corr_a"), ("probe", 404, "corr_unknown_probe")], [(x["kind"], x["status"], x["correlation_id"]) for x in got])
        for line in got:
            self.assertEqual({"case_id", "correlation_id", "method", "path", "status", "kind"}, set(line))
            self.assertEqual(("BEN-A-0001", "GET", "/v1/reconstructions/{correlation_id}"), (line["case_id"], line["method"], line["path"]))
            self.assertIs(int, type(line["status"]))

    def test_the_lines_are_appended_after_the_runs_own_and_never_replace_them(self) -> None:
        own = {"case_id": "BEN-A-0001", "correlation_id": "corr_a", "method": "POST", "path": "/v1/conversations", "status": 202, "kind": "case"}
        with tempfile.TemporaryDirectory() as tmp:
            run, probes = self.setup(tmp, [probe_row("BEN-A-0001", "corr_a")], [own])
            append_probe_requests(run, probes)
            got = jsonl(run / "requests.jsonl")
        self.assertEqual(own, got[0])
        self.assertEqual(5, len(got))

    def test_running_it_twice_does_not_double_the_probe_lines(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run, probes = self.setup(tmp, [probe_row("BEN-A-0001", "corr_a")])
            append_probe_requests(run, probes)
            first = (run / "requests.jsonl").read_bytes()
            append_probe_requests(run, probes)
            self.assertEqual(first, (run / "requests.jsonl").read_bytes())

    def test_a_probe_with_no_answer_adds_no_line_and_cases_stay_in_the_probe_files_order(self) -> None:
        rows = [probe_row("BEN-B-0001", "corr_b", control=None, wrong=None), probe_row("BEN-A-0001", "corr_a", foreign=None)]
        with tempfile.TemporaryDirectory() as tmp:
            run, probes = self.setup(tmp, rows)
            append_probe_requests(run, probes)
            got = jsonl(run / "requests.jsonl")
        self.assertEqual([("BEN-B-0001", "probe", 401), ("BEN-B-0001", "probe", 404), ("BEN-A-0001", "reconstruction", 200), ("BEN-A-0001", "probe", 404), ("BEN-A-0001", "probe", 404)],
                         [(x["case_id"], x["kind"], x["status"]) for x in got])

    def test_a_missing_request_file_or_an_unreadable_probe_file_is_an_error_and_writes_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = Path(tmp)
            probes = run / "negative-probes.json"
            probes.write_text(json.dumps({"met": True, "cases": [probe_row("BEN-A-0001", "corr_a")]}), encoding="utf-8")
            with self.assertRaises(ValueError):  # no requests.jsonl: the runner did not write its own record
                append_probe_requests(run, probes)
            self.assertFalse((run / "requests.jsonl").exists())
            (run / "requests.jsonl").write_text("", encoding="utf-8")
            for text in ("not json", "[]", "{}", '{"cases": "x"}', '{"cases": [null]}', '{"cases": [{"case_id": "BEN-A-0001"}]}',
                         json.dumps({"cases": [{**probe_row("BEN-A-0001", "corr_a"), "correlation_id": ""}]}),
                         json.dumps({"cases": [{**probe_row("BEN-A-0001", "corr_a"), "control": {"status": True}}]}),
                         json.dumps({"cases": [{**probe_row("BEN-A-0001", "corr_a"), "wrong_role": {"status": "404"}}]})):
                probes.write_text(text, encoding="utf-8")
                with self.assertRaises(ValueError, msg=text[:50]):
                    append_probe_requests(run, probes)
                self.assertEqual("", (run / "requests.jsonl").read_text(encoding="utf-8"), text[:50])

    def test_the_cli_appends_them_when_the_probes_file_is_given(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-A-0001")])
            (run / "requests.jsonl").write_text("", encoding="utf-8")
            probes = Path(tmp) / "negative-probes.json"
            probes.write_text(json.dumps({"met": True, "cases": [probe_row("BEN-A-0001", "corr_a")]}), encoding="utf-8")
            self.assertEqual(0, main(["--run", str(run), "--sha", SHA, "--run-id", RUN, "--probes", str(probes)]))
            self.assertEqual(4, len(jsonl(run / "requests.jsonl")))
            self.assertEqual({"sha": SHA, "run_id": RUN}, json.loads(probes.read_text(encoding="utf-8"))["stamp"])


class Cli(unittest.TestCase):
    def test_it_writes_the_stamp_file_and_the_stamped_counts_and_exits_zero(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-CLEAN-0001"), result("BEN-TMO-0001", [{"capability": "c", "target": "t", "count": 1}])])
            code = main(["--run", str(run), "--sha", SHA, "--run-id", RUN])
            self.assertEqual(0, code)
            self.assertEqual({"sha": SHA, "run_id": RUN}, json.loads((run / "stamp.json").read_text(encoding="utf-8")))
            doc = json.loads((run / "mock-case-writes.json").read_text(encoding="utf-8"))
            self.assertEqual({"stamp": {"sha": SHA, "run_id": RUN}, "writes": {"BEN-CLEAN-0001": 0, "BEN-TMO-0001": 1}}, doc)

    def test_it_stamps_the_reconstructions_file_and_exits_two_before_writing_anything_when_it_is_missing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-CLEAN-0001")], {"BEN-CLEAN-0001": {"initial": {"events": [], "sections": []}}})
            self.assertEqual(0, main(["--run", str(run), "--sha", SHA, "--run-id", RUN]))
            self.assertEqual({"sha": SHA, "run_id": RUN}, json.loads((run / "reconstructions.json").read_text(encoding="utf-8"))["stamp"])
        with tempfile.TemporaryDirectory() as tmp:
            run = new_run(Path(tmp), [result("BEN-CLEAN-0001")])
            (run / "reconstructions.json").unlink()
            self.assertEqual(2, main(["--run", str(run), "--sha", SHA, "--run-id", RUN]))
            self.assertFalse((run / "stamp.json").exists())
            self.assertFalse((run / "mock-case-writes.json").exists())

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
            (run / "requests.jsonl").write_text("", encoding="utf-8")
            probes = Path(tmp) / "negative-probes.json"
            probes.write_text(json.dumps({"met": True, "cases": []}), encoding="utf-8")
            self.assertEqual(0, main(["--run", str(run), "--sha", SHA, "--run-id", RUN, "--probes", str(probes)]))
            self.assertEqual({"sha": SHA, "run_id": RUN}, json.loads(probes.read_text(encoding="utf-8"))["stamp"])


if __name__ == "__main__":
    unittest.main()
