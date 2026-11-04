#!/usr/bin/env python3
"""The two decisions behind the per-profile runtime probes (matrix.py, check 9), offline. SYNTHETIC inputs. They must pass on a
correct running profile and fail on each way it can be wrong. What a profile must START is profiles.py's (SEC-11, noema#114). Run: python3 deploy/compose/netcheck/test_profiles.py"""
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location("matrix", pathlib.Path(__file__).resolve().parent / "matrix.py")
matrix = importlib.util.module_from_spec(spec)
spec.loader.exec_module(matrix)


class LiveFindings(unittest.TestCase):
    def test_exactly_what_compose_declares_has_no_finding(self) -> None:
        declared = {"core", "model", "ui", "postgres", "temporal", "mock-case", "mock-case-db", "mock-case-credentials"}
        running = declared - {"mock-case-credentials"}  # a one-shot sidecar has exited
        self.assertEqual(matrix.live_findings("demo", declared, running), [])

    def test_a_service_that_runs_but_is_not_declared_is_a_finding(self) -> None:
        declared = {"core", "model", "ui", "postgres", "temporal", "mock-case", "mock-case-db"}
        got = matrix.live_findings("demo", declared, declared | {"hostility-proxy"})
        self.assertEqual(got, ["demo: hostility-proxy is running but is not part of this profile"])

    def test_a_declared_service_that_is_not_running_is_a_finding_unless_it_is_a_one_shot(self) -> None:
        declared = {"core", "model", "ui", "postgres", "temporal", "hostility-proxy", "hostility-proxy-anchor", "hostility-proxy-credentials"}
        running = declared - {"hostility-proxy", "hostility-proxy-anchor", "hostility-proxy-credentials"}
        self.assertEqual(matrix.live_findings("ci", declared, running), ["ci: hostility-proxy should be running and is not"])

    def test_the_run_profiles_are_exactly_the_ones_with_a_live_check(self) -> None:
        self.assertEqual(set(matrix.LIVE_PROFILES), {"ci", "bench", "demo"})
        self.assertNotIn("dev", matrix.LIVE_PROFILES)  # dev is the stack every other check runs on


class LiveExpectation(unittest.TestCase):
    def test_open_only_when_the_target_runs_and_a_network_is_shared(self) -> None:
        demo = {"core", "model", "ui", "postgres", "temporal", "mock-case", "mock-case-db"}
        self.assertEqual(matrix.live_expectation("core", "mock-case", demo), "open")
        self.assertEqual(matrix.live_expectation("core", "hostility-proxy", demo), "refused", "not running in demo")
        self.assertEqual(matrix.live_expectation("core", "mock-case-truth", demo), "refused", "bench_net is not the core's")
        ci = demo | {"hostility-proxy", "webhook-relay"}
        self.assertEqual(matrix.live_expectation("core", "hostility-proxy", ci), "open")
        self.assertEqual(matrix.live_expectation("core", "hostility-proxy-control", ci), "refused")
        self.assertEqual(matrix.live_expectation("model", "mock-case", ci), "refused")
        self.assertEqual(matrix.live_expectation("ui", "core", ci), "open")

    def test_the_truth_and_control_listeners_follow_their_container(self) -> None:
        self.assertEqual(matrix.SERVICE_OF["mock-case-truth"], "mock-case")
        self.assertEqual(matrix.SERVICE_OF["hostility-proxy-control"], "hostility-proxy")

    def test_no_runtime_service_may_reach_the_oracle_in_any_profile(self) -> None:
        everything = {"core", "model", "ui", "postgres", "temporal", "mock-case", "mock-case-db", "hostility-proxy", "webhook-relay", "evil-provider"}
        for source in matrix.RUNTIME:
            for target in matrix.ORACLE_TARGETS:
                if target in matrix.TARGETS and not target.startswith("sentinel-"):
                    self.assertEqual(matrix.live_expectation(source, target, everything), "refused", (source, target))


if __name__ == "__main__":
    unittest.main()
