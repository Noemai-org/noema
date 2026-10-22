"""One case, one code path (REQ-BEN-002): reset, fixture, fault plan, baseline, inputs through the public API, clocks, the Record, the oracle's readout."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts
from benchmark.oracle_client import testdata as otd
from benchmark.oracle_client.client import OracleClient
from benchmark.oracle_client.counters import DeclaredAbsent, RecordGovernance, RefusalLog
from benchmark.oracle_client.truth import MockCaseTruth
from benchmark.runner.api import PublicApi
from benchmark.runner.control import MockCaseControl
from benchmark.runner.env import EnvironmentUnavailable, RunEnv, UnavailableFixtureLoader
from benchmark.runner.run import CaseError, run_case, run_suite
from benchmark.runner.suite import LoadedCase, Suite, load_suite
from benchmark.runner.test_suite import suite_dir
from benchmark.runner.testdata import ADMIN_TOKEN, USER_TOKEN, FakeNoema, Script, reconstruction

Json = dict[str, Any]
_loaded: dict[str, Suite] = {}


def suite() -> Suite:
    if "s" not in _loaded:
        _loaded["s"] = load_suite(suite_dir())
    return _loaded["s"]


def pick(predicate: Any) -> LoadedCase:
    return next(lc for lc in suite().cases if predicate(lc))


def plain_case() -> LoadedCase:
    return pick(lambda lc: lc.plan is None and lc.case["category"] == "CLEAN_UNIQUE" and len(lc.case["utterances"]) == 1
                and [e["event"] for e in lc.case["events_timeline"]] == ["session_start:synthetic"] and lc.case["principal"]["roles"] == ["customer"])


class Sessions:
    def __init__(self, token: str = USER_TOKEN) -> None:
        self.token, self.asked = token, []  # type: ignore[var-annotated]

    def token_for(self, principal_id: str, roles: list[str]) -> str:
        self.asked.append((principal_id, tuple(roles)))
        return self.token


class Loader:
    def __init__(self, stub: otd.StubBench) -> None:
        self.stub, self.seen = stub, []  # type: ignore[var-annotated]

    def load(self, fixture: Json) -> None:
        self.seen.append((fixture["fixture_id"], [(m, p) for m, p, _ in self.stub.requests]))


class Rig:
    """A runner wired to a stand-in core and a stand-in bench listener, with the oracle log in a temp file."""

    def __init__(self, script: Script | None = None, *, token: str = USER_TOKEN, with_log: bool = True, timeout_s: float = 2.0) -> None:
        self.dir = tempfile.TemporaryDirectory()
        self.log = Path(self.dir.name) / "mock-case.log"
        self.log.write_text("")
        self.noema = FakeNoema(script)
        self.stub = otd.StubBench()
        self.token, self.with_log, self.timeout_s = token, with_log, timeout_s
        self.sessions: Sessions = Sessions(token)

    def __enter__(self) -> "Rig":
        self.noema.__enter__()
        self.stub.__enter__()
        self.loader = Loader(self.stub)
        truth = MockCaseTruth(self.stub.url, otd.BENCH_TOKEN)
        self.oracle = OracleClient(truth, refusal_log=RefusalLog(self.log) if self.with_log else None, governance=RecordGovernance("MODEL"),
                                   monetary=DeclaredAbsent("monetary_calls", "test run: no monetary route"))
        self.env = RunEnv(api=PublicApi(self.noema.url), control=MockCaseControl(self.stub.url, otd.BENCH_TOKEN), oracle=self.oracle, fixtures=self.loader,
                          sessions=self.sessions, run_id="run1", admin_token=ADMIN_TOKEN, tenant_id="ten_synth", settle_timeout_s=self.timeout_s,
                          poll_s=0.0, sleep=lambda s: None)
        return self

    def __exit__(self, *exc: Any) -> None:
        self.stub.__exit__(*exc)
        self.noema.__exit__(*exc)
        self.dir.cleanup()


def with_timeline(lc: LoadedCase, events: list[str], *, turns: list[str] | None = None, at: str = "+00:05:00") -> LoadedCase:
    """The same case with extra timeline events and follow-up turns (a copy; the suite is untouched)."""
    case = json.loads(json.dumps(lc.case))
    case["events_timeline"] += [{"at": at, "event": e} for e in events]
    for text in turns or []:
        case["utterances"].append({"turn": len(case["utterances"]) + 1, "text": text})
    return LoadedCase(case, lc.fixture, lc.plan, lc.entry)


def errors(result: Json) -> list[str]:
    return Contracts().errors("benchmark-case-result", result)


class RunCase(unittest.TestCase):
    def test_a_clean_case_completes_and_validates(self) -> None:
        lc = plain_case()
        with Rig(Script(reconstruction=reconstruction("ALLOW", claims=[{"text": "completed", "bound_fact_status": "KNOWN"}]))) as rig:
            result = run_case(rig.env, lc)
        self.assertEqual(result["status"], "COMPLETED", result.get("error"))
        self.assertEqual(errors(result), [])
        self.assertEqual((result["case_id"], result["case_version"], result["run_id"]), (lc.case["case_id"], 1, "run1"))
        self.assertEqual(result["environment"]["effects"], [])
        self.assertEqual(result["path"], "CLEAN")
        self.assertIsNone(result["cost_usd_micros"], "no source for provider cost exists yet: null, never an estimate")

    def test_the_order_is_reset_fixture_plan_baseline_inputs(self) -> None:
        lc = pick(lambda x: x.plan is not None and any(f["target"] == "mock-case" and f["effect"] == "DROP_RESPONSE" for f in x.plan["faults"]) and len(x.plan["faults"]) == 1)
        with Rig() as rig:
            run_case(rig.env, lc)
            self.assertEqual(rig.loader.seen[0][1], [("POST", "/control/v1/reset")], "the fixture is loaded right after the reset, before anything else")
            order = [(m, p) for m, p, _ in rig.stub.requests]
            self.assertLess(order.index(("PUT", "/control/v1/fault-plan")), order.index(("GET", "/truth/v1/state")), "the plan is loaded before the baseline")
            self.assertEqual(rig.stub.plan is not None, True)
            self.assertEqual(rig.stub.plan["fault_plan_id"], lc.plan["fault_plan_id"])  # type: ignore[index]

    def test_a_write_the_core_makes_is_read_from_the_oracle_not_from_the_response(self) -> None:
        lc = pick(lambda x: x.case["expected"]["disposition"] == "ESCALATE" and x.case["expected"]["writes_expected"] and x.plan is None and len(x.case["utterances"]) == 1
                  and [e["event"] for e in x.case["events_timeline"]] == ["session_start:synthetic"])
        target = lc.case["expected"]["writes_expected"][0]["target"]

        def core_acts() -> None:
            rig_stub.state["cases"].append(otd.case("cas_1", tx=target))
            rig_stub.commands.append(otd.command(1, "create_case"))

        script = Script(reconstruction=reconstruction("ESCALATE", writes=[("fin.cap.create_escalation_case", target)], events=["DECISION_MADE", "VERDICT"]), on_start=core_acts)
        with Rig(script) as rig:
            rig_stub = rig.stub
            result = run_case(rig.env, lc)
        self.assertEqual(result["status"], "COMPLETED", result.get("error"))
        self.assertEqual(result["environment"]["effects"], [{"capability": "fin.cap.create_escalation_case", "target": target, "count": 1}])
        self.assertTrue(result["observed"]["escalation_opened"])
        self.assertEqual(result["path"], "ESCALATION")

    def test_only_public_api_calls_reach_noema(self) -> None:
        with Rig() as rig:
            run_case(rig.env, plain_case())
            self.assertTrue(rig.noema.paths())
            self.assertTrue(all(p.startswith("/v1/") for p in rig.noema.paths()), rig.noema.paths())
            self.assertFalse([p for p in rig.noema.paths() if "truth" in p or "control" in p])
            self.assertTrue(all(p.startswith(("/truth/", "/control/")) for _, p, _ in rig.stub.requests), "the oracle and control surfaces are never asked of Noema")

    def test_the_session_is_the_cases_principal(self) -> None:
        lc = plain_case()
        with Rig() as rig:
            run_case(rig.env, lc)
            self.assertEqual(rig.sessions.asked, [(lc.case["principal"]["principal_id"], tuple(lc.case["principal"]["roles"]))])

    def test_an_unsupported_fault_is_an_error_and_noema_is_never_called(self) -> None:
        lc = pick(lambda x: x.plan is not None and any(f["target"] == "mock-ledger" for f in x.plan["faults"]))
        with Rig() as rig:
            result = run_case(rig.env, lc)
            self.assertEqual(rig.noema.requests, [])
        self.assertEqual((result["status"], result["error"]["code"]), ("ERROR", "FAULT_UNSUPPORTED"))
        self.assertEqual(errors(result), [])
        self.assertIsNone(result["observed"])

    def test_an_event_kind_without_a_driver_is_an_error_and_noema_is_never_called(self) -> None:
        lc = pick(lambda x: x.plan is None and any(e["event"].startswith("webhook:") for e in x.case["events_timeline"]))
        with Rig() as rig:
            result = run_case(rig.env, lc)
            self.assertEqual(rig.noema.requests, [])
        self.assertEqual(result["error"]["code"], "EVENT_KIND_UNSUPPORTED")
        self.assertIn("webhook", result["error"]["message"])

    def test_a_fault_no_producer_has_is_an_error_naming_it(self) -> None:
        lc = pick(lambda x: any(e["event"].startswith("needs_fault:") for e in x.case["events_timeline"]))
        with Rig() as rig:
            result = run_case(rig.env, lc)
            self.assertEqual(rig.noema.requests, [])
        self.assertEqual(result["error"]["code"], "EVENT_KIND_UNSUPPORTED")
        self.assertIn("needs_fault:", result["error"]["message"])

    def test_no_fixture_loader_is_an_error(self) -> None:
        with Rig() as rig:
            env = RunEnv(**{**rig.env.__dict__, "fixtures": UnavailableFixtureLoader("the Tushe sandbox is not reachable")})
            result = run_case(env, plain_case())
        self.assertEqual(result["error"]["code"], "FIXTURE_UNAVAILABLE")

    def test_a_counter_with_no_source_is_an_error_not_zero(self) -> None:
        with Rig(with_log=False) as rig:
            result = run_case(rig.env, plain_case())
        self.assertEqual(result["error"]["code"], "ORACLE_COUNTER_UNAVAILABLE")
        self.assertIn("oracle_accesses_by_runtime", result["error"]["message"])

    def test_a_case_that_never_settles_is_an_error(self) -> None:
        with Rig(Script(states=["EVALUATING"]), timeout_s=0.05) as rig:
            result = run_case(rig.env, plain_case())
        self.assertEqual(result["error"]["code"], "NOT_SETTLED")

    def test_a_refused_session_is_an_error(self) -> None:
        with Rig(token="wrong") as rig:
            result = run_case(rig.env, plain_case())
        self.assertEqual(result["error"]["code"], "API_REFUSED")

    def test_an_unreachable_core_is_an_error_with_an_unknown_outcome(self) -> None:
        with Rig() as rig:
            env = RunEnv(**{**rig.env.__dict__, "api": PublicApi("http://127.0.0.1:9", timeout=0.5)})
            result = run_case(env, plain_case())
        self.assertEqual(result["error"]["code"], "API_UNREACHABLE")

    def test_an_unexpected_reconstruction_is_an_error(self) -> None:
        with Rig(Script(reconstruction={"nothing": "useful"})) as rig:
            result = run_case(rig.env, plain_case())
        self.assertEqual(result["error"]["code"], "RECONSTRUCTION_UNEXPECTED")

    def test_a_clock_advance_moves_the_simulated_clock_after_the_first_input(self) -> None:
        lc = pick(lambda x: x.plan is not None and all(f["target"] == "mock-case" and f["effect"] in ("TIMEOUT_AFTER_COMMIT", "HTTP_5XX", "DROP_RESPONSE", "TIMEOUT_BEFORE_COMMIT")
                                                       for f in x.plan["faults"]) and any(e["event"] == "clock_advance:PT31S" for e in x.case["events_timeline"])
                  and len(x.case["utterances"]) == 1 and all(e["event"].split(":")[0] in ("session_start", "clock_advance") for e in x.case["events_timeline"]))
        with Rig() as rig:
            result = run_case(rig.env, lc)
            self.assertEqual(result["status"], "COMPLETED", result.get("error"))
            self.assertEqual(rig.stub.advanced, [{"by": "PT31S"}])
            self.assertEqual(rig.stub.plan["clock"]["start"], "2026-10-01T09:40:00+01:00")  # type: ignore[index]
            self.assertEqual(result["path"], "FAULTED")

    def test_follow_up_turns_come_after_the_timeline_events(self) -> None:
        lc = with_timeline(plain_case(), ["clock_advance:PT61S"], turns=["The one ending 1234"])
        with Rig() as rig:
            result = run_case(rig.env, lc)
            self.assertEqual(result["status"], "COMPLETED", result.get("error"))
            order = [(m, p) for m, p, _, _ in rig.noema.requests if m == "POST"]
            self.assertEqual(order, [("POST", "/v1/conversations"), ("POST", "/v1/conversations/conv_0001/messages")])
            self.assertEqual(rig.stub.advanced, [{"by": "PT61S"}])
            advance_at = [i for i, (_, p, _) in enumerate(rig.stub.requests) if p == "/control/v1/clock/advance"][0]
            self.assertGreater(advance_at, 0)

    def test_a_confirm_event_answers_the_confirmation_request(self) -> None:
        lc = with_timeline(plain_case(), ["confirm:ACCEPT"])
        with Rig(Script(sse=[("confirmation", {"confirmation_request_id": "cnf_0001"})])) as rig:
            result = run_case(rig.env, lc)
            self.assertEqual(result["status"], "COMPLETED", result.get("error"))
            self.assertIn("/v1/confirmations/cnf_0001", rig.noema.paths())
        with Rig(Script(sse=[("progress", {"state": "EVALUATING"})])) as rig:
            self.assertEqual(run_case(rig.env, lc)["error"]["code"], "CONFIRMATION_NOT_FOUND")

    def test_a_confirmation_argument_the_api_does_not_take_has_no_driver(self) -> None:
        lc = with_timeline(plain_case(), ["confirm:ACCEPT_BY_REQUESTER"])
        with Rig() as rig:
            self.assertEqual(run_case(rig.env, lc)["error"]["code"], "EVENT_KIND_UNSUPPORTED")

    def test_the_kill_switch_is_set_before_the_first_input(self) -> None:
        lc = with_timeline(plain_case(), ["kill_switch:on"], at="+00:00:00")
        with Rig() as rig:
            result = run_case(rig.env, lc)
            self.assertEqual(result["status"], "COMPLETED", result.get("error"))
            paths = rig.noema.paths()
            self.assertLess(paths.index("/v1/admin/tenants/ten_synth/kill-switch"), paths.index("/v1/conversations"))

    def test_the_outcome_digest_is_stable_across_reruns(self) -> None:
        with Rig() as rig:
            a, b = run_case(rig.env, plain_case()), run_case(rig.env, plain_case())
        self.assertEqual(a["outcome_digest"], b["outcome_digest"])
        self.assertTrue(a["outcome_digest"].startswith("sha256:"))

    def test_a_completed_result_is_scorable(self) -> None:
        """The scorer accepts what the runner writes: the contract between them holds."""
        import json as _json
        from benchmark.contracts import ROOT
        from benchmark.scoring.registry import registry_from_pack
        from benchmark.scoring.scorecard import score
        lc = plain_case()
        oracle = _json.loads((suite_dir() / lc.case["oracle_ref"]).read_text())
        target = oracle["correct_target"]
        script = Script(reconstruction=reconstruction("ALLOW", claims=[{"text": "completed", "bound_fact_status": "KNOWN"}], resolved_target=target,
                                                      classification=lc.case["expected"]["classification"], events=lc.case["expected"]["required_audit_events"]))
        with Rig(script) as rig:
            result = run_case(rig.env, lc)
        pack = _json.loads((ROOT / "packs/fintech_v1.json").read_text())
        card = score([lc.case], [oracle], [result], registry_from_pack(pack), run_id="run1")
        self.assertTrue(card["complete"], card["failures"])
        self.assertEqual(card["failures"], [], "the runner's result for a correctly handled case is scored as correctly handled")
        self.assertIsNone(card["tier3"]["cost_usd_micros_total"])


class RunSuite(unittest.TestCase):
    def _small(self, n: int = 2) -> Suite:
        s = suite()
        return Suite(s.root, s.manifest, [plain_case(), pick(lambda x: x.plan is not None and any(f["target"] == "mock-ledger" for f in x.plan["faults"]))][:n], [])

    def test_results_and_a_valid_manifest_are_written(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as out:
            summary = run_suite(rig.env, self._small(), Path(out), commit_sha="0" * 40, providers=["evil-provider"], suite_manifest_bytes=(suite_dir() / "manifest.json").read_bytes())
            files = sorted(p.name for p in (Path(out) / "results").glob("*.json"))
            self.assertEqual(len(files), 2)
            manifest = json.loads((Path(out) / "run-manifest.json").read_text())
            self.assertEqual(Contracts().errors("benchmark-run-manifest", manifest), [])
            self.assertEqual(manifest["status"], "INCOMPLETE", "one case was an ERROR")
            self.assertEqual(manifest["counts"], {"total": 2, "run": 2, "completed": 1, "error": 1, "invalid": 0})
            self.assertEqual(manifest["errors_by_code"], {"FAULT_UNSUPPORTED": 1})
            self.assertEqual(manifest["counter_sources"], rig.oracle.sources())
            self.assertEqual((manifest["commit_sha"], manifest["providers"]), ("0" * 40, ["evil-provider"]))
            self.assertEqual(summary["status"], "INCOMPLETE")
            for f in (Path(out) / "results").glob("*.json"):
                self.assertEqual(errors(json.loads(f.read_text())), [])

    def test_a_complete_run_says_so(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as out:
            s = self._small(1)
            summary = run_suite(rig.env, s, Path(out), commit_sha=None, providers=[], suite_manifest_bytes=b"{}")
            self.assertEqual(summary["status"], "COMPLETE")

    def test_a_run_that_cannot_start_is_not_run_and_calls_nothing(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as out:
            env = RunEnv(**{**rig.env.__dict__, "fixtures": UnavailableFixtureLoader("the Tushe sandbox is not reachable from this build")})
            summary = run_suite(env, self._small(), Path(out), commit_sha=None, providers=[], suite_manifest_bytes=b"{}")
            self.assertEqual(summary["status"], "NOT_RUN")
            self.assertEqual(rig.noema.requests, [])
            self.assertEqual(rig.stub.requests, [])
            self.assertFalse(list((Path(out) / "results").glob("*.json")) if (Path(out) / "results").exists() else [])
            manifest = json.loads((Path(out) / "run-manifest.json").read_text())
            self.assertEqual(Contracts().errors("benchmark-run-manifest", manifest), [])
            self.assertIn("Tushe", manifest["not_run_reason"])

    def test_invalid_cases_are_listed_and_make_the_run_incomplete(self) -> None:
        s = self._small(1)
        s = Suite(s.root, s.manifest, s.cases, [("BEN-CLEAN-0002", ["expected: required"])])
        with Rig() as rig, tempfile.TemporaryDirectory() as out:
            summary = run_suite(rig.env, s, Path(out), commit_sha=None, providers=[], suite_manifest_bytes=b"{}")
            self.assertEqual(summary["status"], "INCOMPLETE")
            self.assertEqual(json.loads((Path(out) / "run-manifest.json").read_text())["invalid_case_ids"], ["BEN-CLEAN-0002"])

    def test_only_selected_cases_run(self) -> None:
        with Rig() as rig, tempfile.TemporaryDirectory() as out:
            s = self._small(2)
            run_suite(rig.env, s, Path(out), commit_sha=None, providers=[], suite_manifest_bytes=b"{}", only={s.cases[0].case["case_id"]})
            self.assertEqual(len(list((Path(out) / "results").glob("*.json"))), 1)

    def test_case_error_is_a_value_object(self) -> None:
        e = CaseError("X_Y", "m")
        self.assertEqual((e.code, str(e)), ("X_Y", "m"))
        self.assertTrue(issubclass(EnvironmentUnavailable, Exception))


if __name__ == "__main__":
    unittest.main()
