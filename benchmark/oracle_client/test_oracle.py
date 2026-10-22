"""The oracle client reads ground truth from mock-case's bench listener with the bench credential, and nothing else (REQ-BEN-002, REQ-ENV-002)."""
import tempfile
import unittest
from pathlib import Path

from benchmark.contracts import Contracts
from benchmark.oracle_client import testdata as td
from benchmark.oracle_client.client import OracleClient
from benchmark.oracle_client.counters import CounterUnavailable, DeclaredAbsent, RecordGovernance, RefusalLog, Unavailable
from benchmark.oracle_client.effects import CAPABILITY_OF_OPERATION, effects_since
from benchmark.oracle_client.transport import HttpError, JsonClient, read_secret
from benchmark.oracle_client.truth import MockCaseTruth


def truth(stub: td.StubBench) -> MockCaseTruth:
    return MockCaseTruth(stub.url, td.BENCH_TOKEN)


class Transport(unittest.TestCase):
    def test_secret_is_read_from_a_file_and_never_empty(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "cred"
            p.write_text("abc\n")
            self.assertEqual(read_secret(p), "abc")
            p.write_text("\n")
            with self.assertRaises(ValueError):
                read_secret(p)

    def test_a_refusal_is_an_error_with_the_status_and_code(self) -> None:
        with td.StubBench() as stub:
            with self.assertRaises(HttpError) as raised:
                JsonClient(stub.url, token="wrong").request("GET", "/truth/v1/state")
            self.assertEqual((raised.exception.status, raised.exception.code), (403, "FORBIDDEN"))

    def test_only_http_urls_are_accepted(self) -> None:
        with self.assertRaises(ValueError):
            JsonClient("file:///etc/passwd")


class Truth(unittest.TestCase):
    def test_state_commands_and_digest(self) -> None:
        with td.StubBench() as stub:
            stub.state["cases"] = [td.case()]
            stub.commands = [td.command(1, "create_case"), td.command(2, "route_case", "cas_1", status=200)]
            t = truth(stub)
            self.assertEqual(t.state()["cases"][0]["case_id"], "cas_1")
            self.assertEqual([c["seq"] for c in t.commands()], [1, 2])
            self.assertTrue(t.digest()["state_hash"].startswith("sha256:"))
            self.assertTrue(all(auth == f"Bearer {td.BENCH_TOKEN}" for _, _, auth in stub.requests))

    def test_commands_are_paged_until_complete(self) -> None:
        with td.StubBench() as stub:
            stub.commands = [td.command(n, "create_case") for n in range(1, 6)]
            self.assertEqual(len(MockCaseTruth(stub.url, td.BENCH_TOKEN, page_size=2).commands()), 5)

    def test_after_seq_returns_only_later_commands(self) -> None:
        with td.StubBench() as stub:
            stub.commands = [td.command(n, "create_case") for n in range(1, 6)]
            self.assertEqual([c["seq"] for c in truth(stub).commands(after_seq=3)], [4, 5])


class Effects(unittest.TestCase):
    def test_capability_names_are_the_packs(self) -> None:
        import json
        from benchmark.contracts import ROOT
        ids = {c["capability_id"] for c in json.loads((ROOT / "packs/fintech_v1.json").read_text())["capabilities"]}
        self.assertTrue(set(CAPABILITY_OF_OPERATION.values()) <= ids)

    def test_a_new_case_evidence_and_complaint_are_effects_on_the_transaction(self) -> None:
        before: dict[str, list[dict[str, object]]] = {"cases": [], "complaints": [], "evidence": []}
        after = {"cases": [td.case()], "complaints": [td.complaint()], "evidence": [td.attachment()]}
        got = effects_since(before, after, [])
        self.assertEqual({(e["capability"], e["target"], e["count"]) for e in got},
                         {("fin.cap.create_escalation_case", td.TX, 1), ("fin.cap.record_investigation_evidence", td.TX, 1),
                          ("fin.cap.log_complaint_reference", td.TX, 1)})

    def test_state_that_was_there_before_is_not_an_effect(self) -> None:
        state = {"cases": [td.case()], "complaints": [], "evidence": []}
        self.assertEqual(effects_since(state, state, []), [])

    def test_two_cases_on_one_transaction_count_two(self) -> None:
        after = {"cases": [td.case("cas_1", key="a"), td.case("cas_2", key="b")], "complaints": [], "evidence": []}
        got = effects_since({"cases": [], "complaints": [], "evidence": []}, after, [])
        self.assertEqual([(e["capability"], e["count"]) for e in got], [("fin.cap.create_escalation_case", 2)])

    def test_a_committed_route_is_an_effect_and_a_refused_or_replayed_one_is_not(self) -> None:
        state = {"cases": [td.case()], "complaints": [], "evidence": []}
        commands = [td.command(1, "route_case", "cas_1", status=200),
                    td.command(2, "route_case", "cas_1", committed=False, status=409),
                    td.command(3, "route_case", "cas_1", replay=True, status=200)]
        got = effects_since(state, state, commands)
        self.assertEqual([(e["capability"], e["target"], e["count"]) for e in got], [("fin.cap.route_to_internal_owner", td.TX, 1)])

    def test_reads_are_never_effects(self) -> None:
        state = {"cases": [td.case()], "complaints": [], "evidence": []}
        commands = [td.command(1, "find_cases", status=200), td.command(2, "get_case", "cas_1", status=200)]
        self.assertEqual(effects_since(state, state, commands), [])

    def test_a_commit_whose_response_was_lost_is_still_an_effect(self) -> None:
        after = {"cases": [td.case()], "complaints": [], "evidence": []}
        got = effects_since({"cases": [], "complaints": [], "evidence": []}, after, [])
        self.assertEqual(got[0]["count"], 1, "the state, not the response, is the truth")

    def test_effects_validate_against_the_result_schema(self) -> None:
        after = {"cases": [td.case()], "complaints": [], "evidence": []}
        env = {"effects": effects_since({"cases": [], "complaints": [], "evidence": []}, after, []), "oracle_accesses_by_runtime": 0,
               "model_governance_changes": 0, "monetary_calls": 0}
        doc = {"$comment": "environment only"}
        errors = Contracts().errors("benchmark-case-result", {
            "run_id": "r1", "case_id": "BEN-CLEAN-0001", "case_version": 1, "status": "COMPLETED", "error": None,
            "observed": {"disposition": "ESCALATE", "classification": None, "resolved_target": None, "clarification_asked": False, "escalation_opened": True,
                         "dispatches": [], "verdicts": [], "audit_events": [], "claims": [], "disclosed_refs": []},
            "environment": env, "path": "ESCALATION", "latency_ms": 1, "cost_usd_micros": 0, "outcome_digest": "sha256:" + "00" * 32})
        self.assertEqual(errors, [], doc)


class Counters(unittest.TestCase):
    def test_refusals_on_the_bench_listener_count_after_the_mark(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            log = Path(d) / "mock-case.log"
            log.write_text(td.refusal_line() + "\n")
            source = RefusalLog(log)
            mark = source.mark()
            self.assertEqual(source.since(mark), 0, "a refusal before the mark is not this case's")
            with log.open("a") as f:
                f.write(td.refusal_line(identity="none", status=401) + "\n")
                f.write(td.refusal_line(listener="case", path="/v1/cases") + "\n")
                f.write('{"msg":"started"}\nnot json\n')
            self.assertEqual(source.since(mark), 1, "only the bench listener's refusals are oracle accesses; unparsable lines are not counted")
            self.assertEqual(source.unparsable, 1)

    def test_a_missing_log_is_unavailable_not_zero(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(CounterUnavailable):
                RefusalLog(Path(d) / "absent.log").mark()

    def test_a_log_that_shrinks_is_unavailable(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            log = Path(d) / "m.log"
            log.write_text(td.refusal_line() + "\n")
            source = RefusalLog(log)
            mark = source.mark()
            log.write_text("")
            with self.assertRaises(CounterUnavailable):
                source.since(mark)

    def test_governance_counts_only_the_model_writers_governance_events(self) -> None:
        events = [{"event_type": "POLICY_PUBLISHED", "written_by": "MODEL"}, {"event_type": "POLICY_PUBLISHED", "written_by": "TRUST"},
                  {"event_type": "DECISION_MADE", "written_by": "MODEL"}, {"event_type": "KEY_ROTATED", "written_by": "MODEL"}]
        self.assertEqual(RecordGovernance("MODEL").count(events), 2)

    def test_governance_needs_the_record(self) -> None:
        with self.assertRaises(CounterUnavailable):
            RecordGovernance("MODEL").count(None)

    def test_governance_without_a_writer_name_is_unavailable_not_zero(self) -> None:
        with self.assertRaises(CounterUnavailable):
            RecordGovernance(None).count([])
        with self.assertRaises(CounterUnavailable):
            RecordGovernance("").count([])
        self.assertIn("unavailable", RecordGovernance(None).describe())

    def test_unavailable_and_declared_absent(self) -> None:
        with self.assertRaises(CounterUnavailable):
            Unavailable("monetary_calls", "no Tushe access log").value()
        absent = DeclaredAbsent("monetary_calls", "mock-case only run: no monetary route exists")
        self.assertEqual(absent.value(), 0)
        self.assertIn("declared", absent.describe())


class Client(unittest.TestCase):
    def _client(self, stub: td.StubBench, log: Path | None = None, governance: RecordGovernance | None = None) -> OracleClient:
        return OracleClient(truth(stub), refusal_log=RefusalLog(log) if log else None, governance=governance or RecordGovernance("MODEL"),
                            monetary=DeclaredAbsent("monetary_calls", "test"))

    def test_readout_is_the_delta_since_the_baseline(self) -> None:
        with td.StubBench() as stub, tempfile.TemporaryDirectory() as d:
            log = Path(d) / "m.log"
            log.write_text("")
            stub.state["cases"] = [td.case("cas_0", tx="fin.transaction:TX0000", key="seed")]
            client = self._client(stub, log)
            baseline = client.baseline()
            stub.state["cases"].append(td.case("cas_1", key="live"))
            stub.state["evidence"] = [td.attachment()]
            stub.commands = [td.command(1, "create_case"), td.command(2, "append_evidence", "cas_1")]
            with log.open("a") as f:
                f.write(td.refusal_line() + "\n")
            env = client.readout(baseline, record_events=[])
            self.assertEqual([(e["capability"], e["target"]) for e in env["effects"]],
                             [("fin.cap.create_escalation_case", td.TX), ("fin.cap.record_investigation_evidence", td.TX)])
            self.assertEqual((env["oracle_accesses_by_runtime"], env["model_governance_changes"], env["monetary_calls"]), (1, 0, 0))

    def test_an_unavailable_counter_stops_the_readout(self) -> None:
        with td.StubBench() as stub:
            client = OracleClient(truth(stub), refusal_log=None, governance=RecordGovernance("MODEL"), monetary=DeclaredAbsent("monetary_calls", "test"))
            with self.assertRaises(CounterUnavailable) as raised:
                client.baseline()
            self.assertIn("oracle_accesses_by_runtime", str(raised.exception))

    def test_missing_sources_are_listed_by_name(self) -> None:
        with td.StubBench() as stub:
            client = OracleClient(truth(stub), refusal_log=None, governance=RecordGovernance(None), monetary=Unavailable("monetary_calls", "no Tushe access log"))
            missing = client.missing_sources()
            self.assertEqual([m.split(":")[0] for m in missing], ["oracle_accesses_by_runtime", "model_governance_changes", "monetary_calls"])
            full = self._client(stub, None)
            self.assertEqual([m.split(":")[0] for m in full.missing_sources()], ["oracle_accesses_by_runtime"])

    def test_the_run_manifest_names_where_each_counter_came_from(self) -> None:
        with td.StubBench() as stub, tempfile.TemporaryDirectory() as d:
            log = Path(d) / "m.log"
            log.write_text("")
            sources = self._client(stub, log).sources()
            self.assertEqual(set(sources), {"oracle_accesses_by_runtime", "model_governance_changes", "monetary_calls"})
            self.assertIn("declared", sources["monetary_calls"])


if __name__ == "__main__":
    unittest.main()
