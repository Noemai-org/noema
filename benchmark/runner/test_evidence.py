"""The G1 evidence helpers (benchmark/runner/evidence.py): the recorded FaultPlan (SR criterion 6) and the two negative probes per case (criterion 9, noema#436).
The probes talk to a small stand-in of the edge over a real socket, so their request and answer handling is exercised; the edge's own behaviour is pinned by its tests."""
import json
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from types import SimpleNamespace
from typing import Any

from benchmark.runner import evidence
from benchmark.runner.evidence import fault_plan_evidence, main, negative_probes

Json = dict[str, Any]


def case(case_id: str, plan_id: str = "fp_none") -> Json:
    return {"case_id": case_id, "fault_plan": plan_id, "principal": {"principal_id": "usr_synth009", "roles": ["customer"]}}


def plan(source: str = "MOCK_CASE", effects: tuple[str, ...] = ("TIMEOUT_AFTER_COMMIT",), seed: Any = 7) -> Json:
    return {"fault_plan_id": "fp_x", "scenario_id": "S", "seed": seed, "deterministic": True, "source": source, "faults": [{"effect": e} for e in effects]}


def suite(*cases: tuple[Json, Json | None]) -> Any:
    return SimpleNamespace(cases=[SimpleNamespace(case=c, plan=p) for c, p in cases])


class FaultPlans(unittest.TestCase):
    def test_every_declared_plan_is_recorded_with_its_seed_producer_and_hash(self) -> None:
        doc = fault_plan_evidence(suite((case("A", "fp_x"), plan()), (case("B"), None)), None)
        self.assertTrue(doc["met"], doc["problems"])
        a = doc["cases"][0]
        self.assertEqual((a["fault_plan_id"], a["seed"], a["source"], a["effects"]), ("fp_x", 7, "MOCK_CASE", ["TIMEOUT_AFTER_COMMIT"]))
        self.assertRegex(a["plan_sha256"], r"^sha256:[0-9a-f]{64}$")
        self.assertEqual(doc["cases"][1], {"case_id": "B", "fault_plan_id": "fp_none"})

    def test_a_run_with_no_timeout_after_write_plan_is_not_met(self) -> None:
        doc = fault_plan_evidence(suite((case("A", "fp_x"), plan(effects=("HTTP_429",)))), None)
        self.assertFalse(doc["met"])
        self.assertTrue(any("TIMEOUT_AFTER_COMMIT" in p for p in doc["problems"]))

    def test_a_producer_that_is_not_a_real_one_a_missing_plan_and_a_non_integer_seed_are_not_met(self) -> None:
        for label, s in {"test double": suite((case("A", "fp_x"), plan(source="TEST_DOUBLE"))), "no plan loaded": suite((case("A", "fp_x"), None)),
                         "boolean seed": suite((case("A", "fp_x"), plan(seed=True))), "float seed": suite((case("A", "fp_x"), plan(seed=1.5)))}.items():
            with self.subTest(label):
                self.assertFalse(fault_plan_evidence(s, None)["met"])

    def test_only_the_cases_of_the_run_are_read(self) -> None:
        s = suite((case("A", "fp_x"), plan()), (case("B", "fp_y"), plan(source="BAD")))
        self.assertTrue(fault_plan_evidence(s, {"A"})["met"])
        self.assertFalse(fault_plan_evidence(s, {"A", "B"})["met"])


class FakeEdge(BaseHTTPRequestHandler):
    """Answers GET /v1/reconstructions/{id} the way the edge does: 401 SESSION_INVALID for a tenant it does not serve, 404 NOT_FOUND_OR_NOT_AUTHORIZED for a role the route does not
    list, and the reconstruction otherwise. Tokens here are 'tenant|principal|role'. `mode` bends it to prove each check can fail."""

    mode = "edge"
    served_tenant = "ten_alpha"

    def log_message(self, *args: Any) -> None:
        pass

    def _send(self, status: int, body: Json) -> None:
        raw = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/v1/anything":
            return self._send(404, {"error": {"code": "NOT_FOUND_OR_NOT_AUTHORIZED", "message": "Not found."}})
        corr = self.path.rsplit("/", 1)[-1]
        tenant, principal, role = self.headers.get("Authorization", "Bearer ||").split(" ", 1)[1].split("|")
        mode = type(self).mode
        if mode == "unmounted":
            return self._send(404, {"error": {"code": "NOT_FOUND_OR_NOT_AUTHORIZED", "message": "Not found."}})
        if tenant != self.served_tenant and mode != "serves-foreign-tenant":
            return self._send(401, {"error": {"code": "SESSION_INVALID", "message": "The session is not valid."}})
        if role not in ("ops", "audit") and mode != "serves-wrong-role":
            return self._send(404, {"error": {"code": "NOT_FOUND_OR_NOT_AUTHORIZED", "message": "Not found."}})
        events = [{"event_id": "e1", "tenant_id": "ten_other" if mode == "other-tenant-event" else tenant, "actor": "core-trust" if mode == "no-customer" else f"customer:{principal}"}]
        if mode == "other-customer":
            events.append({"event_id": "e2", "tenant_id": tenant, "actor": "customer:usr_someone_else"})
        self._send(200, {"tenant_id": tenant, "correlation_id": corr, "events": [] if mode == "empty" else events})


class Probes(unittest.TestCase):
    def setUp(self) -> None:
        FakeEdge.mode = "edge"
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), FakeEdge)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.cases = [("BEN-US-0001", "usr_synth009", "corr_a1"), ("BEN-US-0002", "usr_synth010", "corr_b2")]

    def tearDown(self) -> None:
        if not getattr(self, "stopped", False):
            self.server.shutdown()
            self.server.server_close()

    def run_probes(self) -> Json:
        return negative_probes(self.url, lambda tenant, principal, role: f"{tenant}|{principal}|{role}", self.cases, "ten_alpha", "ten_other")

    def test_a_correct_edge_meets_all_three_checks_for_every_case(self) -> None:
        doc = self.run_probes()
        self.assertTrue(doc["met"], doc)
        for row in doc["cases"]:
            self.assertEqual((row["control"]["status"], row["wrong_role"]["status"], row["wrong_role"]["code"], row["foreign_tenant"]["status"], row["foreign_tenant"]["code"]),
                             (200, 404, "NOT_FOUND_OR_NOT_AUTHORIZED", 401, "SESSION_INVALID"))

    def test_each_check_can_fail(self) -> None:
        for mode in ("serves-wrong-role", "serves-foreign-tenant", "unmounted", "other-tenant-event", "no-customer", "other-customer", "empty"):
            with self.subTest(mode):
                FakeEdge.mode = mode
                self.assertFalse(self.run_probes()["met"], mode)

    def test_an_unmounted_route_is_not_read_as_two_refusals(self) -> None:
        # Every request gets 404: the wrong-role probe "passes" by accident, the foreign-tenant probe cannot, and the control, which is why it exists, fails.
        FakeEdge.mode = "unmounted"
        row = self.run_probes()["cases"][0]
        self.assertFalse(row["control"]["ok"])
        self.assertTrue(row["wrong_role"]["ok"])

    def test_no_cases_is_not_met(self) -> None:
        self.assertFalse(negative_probes(self.url, lambda *a: "x|y|z", [], "ten_alpha", "ten_other")["met"])

    def test_no_answer_is_not_met_and_not_a_crash(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.stopped = True
        self.assertFalse(self.run_probes()["met"])


class Cli(unittest.TestCase):
    def test_probes_refuse_the_same_tenant_twice_and_a_run_with_nothing_is_not_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            base = ["probes", "--suite", tmp, "--run", tmp, "--out", tmp, "--api-url", "http://127.0.0.1:1", "--idp-binary", "x", "--idp-key", "y", "--idp-kid", "k", "--idp-audience", "a"]
            self.assertEqual(main(base + ["--tenant-id", "ten_a", "--other-tenant-id", "ten_a"]), 2)
            self.assertEqual(main(base + ["--tenant-id", "ten_a", "--other-tenant-id", "ten_b"]), 2)  # no manifest: refused, never a pass

    def test_a_run_directory_without_results_is_not_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertRaises(evidence.NotRun, evidence._run_cases, Path(tmp))


if __name__ == "__main__":
    unittest.main()
