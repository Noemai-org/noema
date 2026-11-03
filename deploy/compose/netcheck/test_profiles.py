#!/usr/bin/env python3
"""The two decisions behind the per-profile runtime checks (matrix.py, check 9), offline. SYNTHETIC inputs. They must pass on a
correct profile and fail on each way a profile can be wrong. Run: python3 deploy/compose/netcheck/test_profiles.py"""
import copy
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location("matrix", pathlib.Path(__file__).resolve().parent / "matrix.py")
matrix = importlib.util.module_from_spec(spec)
spec.loader.exec_module(matrix)


def config_for(profile: str) -> dict:
    return {"services": {n: {"networks": {x: {} for x in matrix.NETWORKS.get(n, {"edge_net"})}} for n in matrix.PROFILE_SERVICES[profile]}}


class ProfileFindings(unittest.TestCase):
    def test_a_correct_profile_has_no_finding(self) -> None:
        for profile in matrix.PROFILE_SERVICES:
            self.assertEqual(matrix.profile_findings(profile, config_for(profile)), [], profile)

    def test_the_test_environment_never_leaks_into_a_profile_that_must_not_have_it(self) -> None:
        for profile, extra in (("dev", "hostility-proxy"), ("demo", "hostility-proxy"), ("demo", "evil-provider"), ("dev", "webhook-relay"),
                               ("ci", "evil-provider"), ("bench", "webhook-relay"), ("demo", "webhook-relay")):
            cfg = config_for(profile)
            cfg["services"][extra] = {"networks": {x: {} for x in matrix.NETWORKS.get(extra, {"provider_egress"})}}
            got = matrix.profile_findings(profile, cfg)
            self.assertEqual(len(got), 1, (profile, extra, got))
            self.assertIn(f"{extra} must not run", got[0])

    def test_a_missing_service_is_a_finding(self) -> None:
        for profile, gone in (("ci", "hostility-proxy"), ("ci", "webhook-relay"), ("bench", "evil-provider"), ("demo", "mock-case"), ("dev", "core")):
            cfg = config_for(profile)
            del cfg["services"][gone]
            self.assertIn(f"{gone} is missing", " ".join(matrix.profile_findings(profile, cfg)), (profile, gone))

    def test_a_wrong_network_or_a_published_port_is_a_finding(self) -> None:
        cfg = config_for("demo")
        cfg["services"]["core"]["networks"]["bench_net"] = {}
        self.assertTrue(matrix.profile_findings("demo", cfg))
        loopback = {"mode": "ingress", "host_ip": "127.0.0.1", "target": 8080, "published": "8080", "protocol": "tcp"}
        for service in ("core", "model", "mock-case"):
            cfg = copy.deepcopy(config_for("demo"))
            cfg["services"][service]["ports"] = [loopback]
            self.assertTrue(any("publishes a port" in f for f in matrix.profile_findings("demo", cfg)), service)

    def test_only_the_ui_and_the_relay_may_publish_and_only_on_loopback(self) -> None:
        loopback = {"mode": "ingress", "host_ip": "127.0.0.1", "target": 8090, "published": "8090", "protocol": "tcp"}
        everywhere = dict(loopback, host_ip="0.0.0.0")
        unspecified = {k: v for k, v in loopback.items() if k != "host_ip"}
        cfg = config_for("ci")
        cfg["services"]["ui"]["ports"] = [loopback]
        cfg["services"]["webhook-relay"]["ports"] = [loopback]
        self.assertEqual(matrix.profile_findings("ci", cfg), [])
        for bad in (everywhere, unspecified, "8090:8090"):
            for service in ("ui", "webhook-relay"):
                cfg = config_for("ci")
                cfg["services"][service]["ports"] = [bad]
                self.assertTrue(any("more than the loopback" in f for f in matrix.profile_findings("ci", cfg)), (service, bad))

    def test_the_run_profiles_are_exactly_the_ones_with_a_live_check(self) -> None:
        self.assertEqual(set(matrix.LIVE_PROFILES), {"ci", "bench", "demo"})
        self.assertNotIn("dev", matrix.LIVE_PROFILES)  # dev is the stack every other check runs on


class LiveExpectation(unittest.TestCase):
    def test_open_only_when_the_target_runs_and_a_network_is_shared(self) -> None:
        demo = set(matrix.PROFILE_SERVICES["demo"])
        self.assertEqual(matrix.live_expectation("core", "mock-case", demo), "open")
        self.assertEqual(matrix.live_expectation("core", "hostility-proxy", demo), "refused", "not running in demo")
        self.assertEqual(matrix.live_expectation("core", "mock-case-truth", demo), "refused", "bench_net is not the core's")
        ci = set(matrix.PROFILE_SERVICES["ci"])
        self.assertEqual(matrix.live_expectation("core", "hostility-proxy", ci), "open")
        self.assertEqual(matrix.live_expectation("core", "hostility-proxy-control", ci), "refused")
        self.assertEqual(matrix.live_expectation("model", "mock-case", ci), "refused")
        self.assertEqual(matrix.live_expectation("ui", "core", ci), "open")

    def test_the_truth_and_control_listeners_follow_their_container(self) -> None:
        self.assertEqual(matrix.SERVICE_OF["mock-case-truth"], "mock-case")
        self.assertEqual(matrix.SERVICE_OF["hostility-proxy-control"], "hostility-proxy")

    def test_no_runtime_service_may_reach_the_oracle_in_any_profile(self) -> None:
        for profile in matrix.PROFILE_SERVICES:
            running = set(matrix.PROFILE_SERVICES[profile])
            for source in matrix.RUNTIME:
                for target in matrix.ORACLE_TARGETS:
                    if target in matrix.TARGETS and not target.startswith("sentinel-"):
                        self.assertEqual(matrix.live_expectation(source, target, running), "refused", (profile, source, target))


if __name__ == "__main__":
    unittest.main()
