"""BEN-05 / A444: what a run writes for the G1 package besides its results: `requests.jsonl` (the runner's own per-request record, which noema#730 derives the RECORD_ACCESSED count from) and
`reconstructions.json` (the initial reconstruction of every completed case). Written before the run-loop integration and seen failing (the files did not exist). SYNTHETIC."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.runner.api import PublicApi
from benchmark.runner.env import RunEnv
from benchmark.runner.request_log import FIELDS, RequestRecorder
from benchmark.runner.run import run_suite
from benchmark.runner.suite import Suite
from benchmark.runner.test_run import Rig, pick, plain_case, suite

Json = dict[str, Any]


def small() -> Suite:
    s = suite()
    return Suite(s.root, s.manifest, [plain_case(), pick(lambda x: x.plan is not None and any(f["target"] == "mock-ledger" for f in x.plan["faults"]))], [])


def run_with_recorder(rig: Rig, out: Path) -> RequestRecorder:
    recorder = RequestRecorder()
    env = RunEnv(**{**rig.env.__dict__, "api": PublicApi(rig.env.api.base_url, recorder=recorder)})
    run_suite(env, small(), out, commit_sha=None, providers=[], suite_manifest_bytes=b"{}")
    return recorder


class Artifacts(unittest.TestCase):
    def test_requests_jsonl_holds_the_completed_case_s_requests_with_its_correlation(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            run_with_recorder(rig, out)
            lines = [json.loads(line) for line in (out / "requests.jsonl").read_text(encoding="utf-8").splitlines()]
            correlations = json.loads((out / "correlations.json").read_text(encoding="utf-8"))
        self.assertTrue(lines)
        completed = plain_case().case["case_id"]
        self.assertEqual({completed}, {x["case_id"] for x in lines})  # the other case errored before any request: it records nothing
        for line in lines:
            self.assertEqual(set(FIELDS), set(line))
            self.assertEqual(correlations[completed], line["correlation_id"])
            self.assertIs(int, type(line["status"]))
            self.assertIn(line["kind"], ("case", "reconstruction"))
        self.assertEqual(1, sum(1 for x in lines if x["kind"] == "reconstruction" and x["status"] == 200))
        self.assertEqual(["/v1/conversations"], [x["path"] for x in lines if x["method"] == "POST"][:1])
        self.assertTrue(all("{" in x["path"] or x["path"] == "/v1/conversations" for x in lines))  # templates only

    def test_no_header_body_token_or_concrete_identifier_reaches_the_file(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            run_with_recorder(rig, out)
            text = (out / "requests.jsonl").read_text(encoding="utf-8")
        for forbidden in ("Bearer", "Authorization", "request_id", "inv_0001", "conv_0001", "text"):
            self.assertNotIn(forbidden, text, forbidden)

    def test_reconstructions_json_has_the_initial_body_of_each_completed_case_only(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            run_with_recorder(rig, out)
            doc = json.loads((out / "reconstructions.json").read_text(encoding="utf-8"))
            results = {p.stem: json.loads(p.read_text(encoding="utf-8")) for p in (out / "results").glob("*.json")}
        completed = [c for c, r in results.items() if r["status"] == "COMPLETED"]
        self.assertEqual(sorted(completed), sorted(doc))
        for case in completed:
            self.assertEqual(["initial"], list(doc[case]))
            self.assertIn("sections", doc[case]["initial"])
            self.assertIn("events", doc[case]["initial"])

    def test_a_run_with_no_recorder_writes_no_request_file_but_still_the_reconstructions(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            run_suite(rig.env, small(), out, commit_sha=None, providers=[], suite_manifest_bytes=b"{}")
            self.assertFalse((out / "requests.jsonl").exists())
            self.assertTrue((out / "reconstructions.json").is_file())


if __name__ == "__main__":
    unittest.main()
