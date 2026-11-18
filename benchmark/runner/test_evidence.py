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
from benchmark.runner.evidence import NotRun, fault_plan_evidence, main, negative_probes, record_access_evidence

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
    kind = "customer"  # the principal kind of the Record actor the fake edge returns
    served_tenant = "ten_alpha"
    known = {"corr_a1", "corr_b2"}  # the reconstructions that exist; any other correlation is unknown
    owner = {"corr_a1": "usr_synth009", "corr_b2": "usr_synth010"}  # the principal whose action each reconstruction records (never the token's)

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
        if corr not in self.known and mode != "serves-unknown":
            msg = "No such reconstruction." if mode == "reveals-unknown" else "Not found."
            return self._send(404, {"error": {"code": "NOT_FOUND_OR_NOT_AUTHORIZED", "message": msg}})
        if role not in ("ops", "audit") and mode != "serves-wrong-role":
            return self._send(404, {"error": {"code": "NOT_FOUND_OR_NOT_AUTHORIZED", "message": "Not found."}})
        events = [{"event_id": "e1", "tenant_id": "ten_other" if mode == "other-tenant-event" else tenant, "actor": "core-trust" if mode == "no-customer" else f"{FakeEdge.kind}:{self.owner.get(corr, principal)}"}]
        if mode == "other-customer":
            events.append({"event_id": "e2", "tenant_id": tenant, "actor": "customer:usr_someone_else"})
        self._send(200, {"tenant_id": tenant, "correlation_id": corr, "events": [] if mode == "empty" else events})


class Probes(unittest.TestCase):
    def setUp(self) -> None:
        FakeEdge.mode = "edge"
        FakeEdge.kind = "customer"
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

    def test_a_staff_case_is_checked_against_its_own_principal_kind(self) -> None:
        # Codex P2 on noema#591: the generated suite has ops and audit principals (their Record actors are operator:<id> and auditor:<id>), which a customer-only check rejected.
        for role, kind in (("ops", "operator"), ("audit", "auditor")):
            with self.subTest(role):
                FakeEdge.kind = kind
                roles = {c[0]: [role] for c in self.cases}
                self.assertTrue(negative_probes(self.url, lambda tenant, principal, r: f"{tenant}|{principal}|{r}", self.cases, "ten_alpha", "ten_other", roles=roles)["met"])
                self.assertFalse(self.run_probes()["met"], "the same Record does not satisfy a customer case")
                FakeEdge.kind = "customer"
                self.assertFalse(negative_probes(self.url, lambda tenant, principal, r: f"{tenant}|{principal}|{r}", self.cases, "ten_alpha", "ten_other", roles=roles)["met"], "a customer action does not satisfy a staff case")

    def test_a_role_with_no_known_principal_kind_is_not_run(self) -> None:
        with self.assertRaises(NotRun):
            negative_probes(self.url, lambda *a: "x|y|z", self.cases, "ten_alpha", "ten_other", roles={self.cases[0][0]: ["wizard"]})

    def test_each_check_can_fail(self) -> None:
        for mode in ("serves-wrong-role", "serves-foreign-tenant", "unmounted", "other-tenant-event", "no-customer", "other-customer", "empty", "reveals-unknown", "serves-unknown"):
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


class ProbePrincipals(unittest.TestCase):
    """Each probe has a principal of its own, with no digit in it, so the core's access log can say whether the handler ran for that probe alone (SR's first-dispatch condition on noema#436)."""

    def test_every_probe_has_its_own_principal_with_no_digit_and_the_unknown_correlation_is_recorded(self) -> None:
        FakeEdge.mode = "edge"
        server = ThreadingHTTPServer(("127.0.0.1", 0), FakeEdge)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            doc = negative_probes(f"http://127.0.0.1:{server.server_address[1]}", lambda tenant, principal, role: f"{tenant}|{principal}|{role}",
                                  [("A", "usr_synth009", "corr_a1"), ("B", "usr_synth010", "corr_b2")], "ten_alpha", "ten_other")
        finally:
            server.shutdown()
            server.server_close()
        self.assertTrue(doc["met"], doc)
        seen: set[str] = set()
        for row in doc["cases"]:
            ids = row["probe_principals"]
            self.assertEqual(set(ids), {"control", "wrong_role", "foreign_tenant", "unknown_correlation"})
            for who in ids.values():
                self.assertRegex(who, r"^prb[a-z]{20}$")
                self.assertNotIn(who, seen)
                seen.add(who)
            self.assertRegex(row["unknown_correlation_id"], r"^corr_[0-9a-f]{26}$")


def log_line(principal: str, corr: str, outcome: str, tenant: str = "ten_alpha") -> str:
    return json.dumps({"time": "t", "level": "INFO", "msg": "RECORD_ACCESSED", "tenant_id": tenant, "principal": principal, "correlation_id": corr, "outcome": outcome})


def probes_doc(*, control: str = "prbcontrolaaaaaaaaaaa", unknown: str = "prbunknownaaaaaaaaaaa", wrong: str = "prbwrongaaaaaaaaaaaaa", foreign: str = "prbforeignaaaaaaaaaaa") -> Json:
    return {"met": True, "cases": [{"case_id": "A", "principal_id": "usr_synth009", "correlation_id": "corr_a1", "unknown_correlation_id": "corr_" + "b" * 26,
                                    "probe_principals": {"control": control, "wrong_role": wrong, "foreign_tenant": foreign, "unknown_correlation": unknown}}]}


GOOD_LOG = "\n".join([log_line("auditor:prbcontrolaaaaaaaaaaa", "corr_a1", "SERVED"), log_line("auditor:prbunknownaaaaaaaaaaa", "corr_" + "b" * 26, "NOT_FOUND_OR_NOT_AUTHORIZED"),
                      "not json at all", log_line("auditor:someone", "corr_zz", "SERVED")])


class RecordAccess(unittest.TestCase):
    """The access log shows the handler ran exactly for the control and the unknown-correlation probe and for no refusal (SR's first-dispatch condition on noema#436, criterion 9)."""

    def evidence(self, log: str, doc: Json | None = None) -> Json:
        return record_access_evidence(doc or probes_doc(), log, "ten_alpha", "ten_other")

    def test_one_line_for_the_control_one_for_the_unknown_and_none_for_the_refusals_is_met(self) -> None:
        doc = self.evidence(GOOD_LOG)
        self.assertTrue(doc["met"], doc["problems"])
        self.assertEqual(doc["cases"][0], {"case_id": "A", "control": 1, "unknown_correlation": 1, "wrong_role": 0, "foreign_tenant": 0})

    def test_a_refusal_that_reached_the_handler_is_not_met(self) -> None:
        for label, extra in {"wrong role": log_line("customer:prbwrongaaaaaaaaaaaaa", "corr_a1", "NOT_FOUND_OR_NOT_AUTHORIZED"),
                             "foreign tenant": log_line("auditor:prbforeignaaaaaaaaaaa", "corr_a1", "SERVED", "ten_other")}.items():
            with self.subTest(label):
                doc = self.evidence(GOOD_LOG + "\n" + extra)
                self.assertFalse(doc["met"])
                self.assertTrue(any("handler" in p for p in doc["problems"]), doc["problems"])

    def test_any_line_of_the_other_tenant_is_not_met_whoever_asked(self) -> None:
        self.assertFalse(self.evidence(GOOD_LOG + "\n" + log_line("auditor:x", "corr_q", "SERVED", "ten_other"))["met"])

    def test_the_control_and_the_unknown_probe_must_each_leave_exactly_one_right_line(self) -> None:
        control, unknown = GOOD_LOG.split("\n")[0], GOOD_LOG.split("\n")[1]
        cases = {"no control line": GOOD_LOG.replace(control, ""), "control twice": GOOD_LOG + "\n" + control, "no unknown line": GOOD_LOG.replace(unknown, ""),
                 "control not served": GOOD_LOG.replace('"outcome": "SERVED", ', '"outcome": "SERVED", ', 1).replace(control, control.replace("SERVED", "INTEGRITY")),
                 "control for another correlation": GOOD_LOG.replace(control, control.replace("corr_a1", "corr_zz")),
                 "control for another tenant": GOOD_LOG.replace(control, control.replace("ten_alpha", "ten_gamma")),
                 "unknown that was served": GOOD_LOG.replace(unknown, unknown.replace("NOT_FOUND_OR_NOT_AUTHORIZED", "SERVED")),
                 "unknown for another correlation": GOOD_LOG.replace(unknown, unknown.replace("b" * 26, "c" * 26))}
        for label, log in cases.items():
            with self.subTest(label):
                self.assertFalse(self.evidence(log)["met"], label)

    def test_a_probe_with_no_principal_recorded_or_no_cases_is_not_met(self) -> None:
        doc = probes_doc()
        del doc["cases"][0]["probe_principals"]
        self.assertFalse(self.evidence(GOOD_LOG, doc)["met"])
        self.assertFalse(self.evidence(GOOD_LOG, {"met": True, "cases": []})["met"])

    def test_a_log_with_no_access_line_at_all_is_not_run(self) -> None:
        for log in ("", "   \n", "not json\n" * 3):
            with self.subTest(log), self.assertRaises(NotRun):
                self.evidence(log)

    def test_a_line_that_names_a_principal_only_in_another_member_does_not_count_as_the_handler_running(self) -> None:
        sneaky = json.dumps({"msg": "something else", "principal": "customer:prbwrongaaaaaaaaaaaaa"})
        self.assertTrue(self.evidence(GOOD_LOG + "\n" + sneaky)["met"])

    def test_the_command_reads_the_files_and_exits_0_1_or_3(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "probes.json").write_text(json.dumps(probes_doc()), encoding="utf-8")
            args = ["record-access", "--probes", str(root / "probes.json"), "--log", str(root / "core.log"), "--tenant-id", "ten_alpha", "--other-tenant-id", "ten_other", "--out", str(root / "out")]
            self.assertEqual(main(args), 3)  # no log file: NOT RUN
            (root / "core.log").write_text(GOOD_LOG + "\n", encoding="utf-8")
            self.assertEqual(main(args), 0)
            self.assertTrue(json.loads((root / "out" / "record-access.json").read_text())["met"])
            (root / "core.log").write_text(GOOD_LOG + "\n" + log_line("customer:prbwrongaaaaaaaaaaaaa", "corr_a1", "SERVED") + "\n", encoding="utf-8")
            self.assertEqual(main(args), 1)
            self.assertEqual(main(args[:-2] + ["--out", str(root / "out"), "--other-tenant-id", "ten_alpha"]), 2)  # the other tenant must differ


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
