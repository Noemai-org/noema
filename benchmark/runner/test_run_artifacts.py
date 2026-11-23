"""BEN-05 / A444: what a run writes for the G1 package besides its results: `requests.jsonl` (the runner's own per-request record, which noema#730 derives the RECORD_ACCESSED count from) and
`reconstructions.json` (the initial reconstruction of every completed case). Written before the run-loop integration and seen failing (the files did not exist). SYNTHETIC."""
import json
import re
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
from benchmark.runner.testdata import ADMIN_TOKEN, USER_TOKEN

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


class RoleSessions:
    def __init__(self) -> None:
        self.asked: list[tuple[str, tuple[str, ...]]] = []

    def token_for(self, principal_id: str, roles: list[str]) -> str:
        self.asked.append((principal_id, tuple(roles)))
        return ADMIN_TOKEN if roles == ["audit"] else USER_TOKEN


class AuditReads(unittest.TestCase):
    """A476 (rulings of 20 Nov): the reconstruction route is audit-only (noema#783), so the runner reads each reconstruction as an AUDIT principal, never with the case principal's own session: a customer
    token is refused on a real core and the case would be an ERROR. The audit principal is one of the runner's own (`usr_` and letters, as the evidence probes use), and the case's conversation still
    runs as the case principal."""

    def test_the_reconstruction_is_read_with_an_audit_session_and_the_conversation_with_the_case_s_own(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as tmp:
            sessions = RoleSessions()
            env = RunEnv(**{**rig.env.__dict__, "sessions": sessions})
            run_suite(env, small(), Path(tmp), commit_sha=None, providers=[], suite_manifest_bytes=b"{}")
            requests = rig.noema.requests
        audit = [(p, r) for p, r in sessions.asked if r == ("audit",)]
        self.assertTrue(audit)
        for principal, _ in audit:
            self.assertRegex(principal, r"^usr_[a-z]{20}$")
        self.assertTrue(any(r == ("customer",) for _, r in sessions.asked))
        reads = [x for x in requests if x[0] == "GET" and x[1].startswith("/v1/reconstructions/")]
        others = [x for x in requests if x not in reads]
        self.assertTrue(reads and others)
        self.assertTrue(all(x[2] == f"Bearer {ADMIN_TOKEN}" for x in reads))
        self.assertTrue(all(x[2] == f"Bearer {USER_TOKEN}" for x in others if x[1].startswith("/v1/conversations") or x[1].startswith("/v1/investigations")))

    def test_each_case_gets_its_own_audit_principal(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as tmp:
            sessions = RoleSessions()
            env = RunEnv(**{**rig.env.__dict__, "sessions": sessions})
            run_suite(env, Suite(suite().root, suite().manifest, [plain_case(), plain_case()], []), Path(tmp), commit_sha=None, providers=[], suite_manifest_bytes=b"{}")
        principals = [p for p, r in sessions.asked if r == ("audit",)]
        self.assertEqual(2, len(principals))
        self.assertEqual(len(principals), len(set(principals)))
        self.assertTrue(all(re.fullmatch(r"usr_[a-z]{20}", p) for p in principals))



if __name__ == "__main__":
    unittest.main()
