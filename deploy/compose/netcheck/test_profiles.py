#!/usr/bin/env python3
"""The two decisions behind the per-profile runtime probes (matrix.py, check 9), offline. SYNTHETIC inputs. They must pass on a
correct running profile and fail on each way it can be wrong. What a profile must START is profiles.py's (SEC-11, noema#114). Run: python3 deploy/compose/netcheck/test_profiles.py"""
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location("matrix", pathlib.Path(__file__).resolve().parent / "matrix.py")
matrix = importlib.util.module_from_spec(spec)
spec.loader.exec_module(matrix)

spec_p = importlib.util.spec_from_file_location("profiles", pathlib.Path(__file__).resolve().parent / "profiles.py")
profiles = importlib.util.module_from_spec(spec_p)
spec_p.loader.exec_module(profiles)

BASE = "FROM python:3.13-slim\nENV UVICORN_SSL_CERT_REQS=2\nENTRYPOINT [\"python\"]\n"


class ModelDockerfileRule(unittest.TestCase):  # Codex P2 on noema#670: Docker takes both ENV forms, so the rule reads both
    def test_the_requirement_set_once_to_2_is_clean(self) -> None:
        self.assertEqual(profiles.dockerfile_problems(BASE), [])

    def test_a_later_legacy_env_assignment_of_0_is_a_problem(self) -> None:
        self.assertTrue(profiles.dockerfile_problems(BASE + "ENV UVICORN_SSL_CERT_REQS 0\n"))

    def test_a_later_legacy_env_assignment_of_2_is_not(self) -> None:
        self.assertEqual(profiles.dockerfile_problems(BASE + "ENV UVICORN_SSL_CERT_REQS 2\n"), [])

    def test_a_legacy_env_value_is_everything_after_the_key(self) -> None:
        self.assertTrue(profiles.dockerfile_problems(BASE + "ENV UVICORN_SSL_CERT_REQS 2 extra\n"))

    def test_a_later_pair_among_several_on_one_line_is_read(self) -> None:
        self.assertTrue(profiles.dockerfile_problems(BASE + "ENV A=1 UVICORN_SSL_CERT_REQS=0 B=2\n"))

    def test_a_quoted_legacy_value_is_unquoted(self) -> None:
        self.assertTrue(profiles.dockerfile_problems(BASE + 'ENV UVICORN_SSL_CERT_REQS "0"\n'))

    def test_the_legacy_form_alone_with_2_satisfies_the_requirement(self) -> None:
        self.assertEqual(profiles.dockerfile_problems("FROM x\nENV UVICORN_SSL_CERT_REQS 2\n"), [])


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

    def test_the_one_shot_grant_keys_init_step_may_have_exited_and_nothing_else_named_like_it_is_exempt(self) -> None:  # Codex P2 on noema#700: SEC-09, INV-29
        declared = {"core", "model", "ui", "postgres", "temporal", "grant-keys-init"}
        self.assertEqual(matrix.live_findings("ci", declared, declared - {"grant-keys-init"}), [])  # it ran to completion and exited
        declared = {"core", "model", "ui", "postgres", "temporal", "another-init"}
        self.assertEqual(matrix.live_findings("ci", declared, declared - {"another-init"}), ["ci: another-init should be running and is not"])  # a suffix is not an exemption

    def test_the_one_shot_trust_dsn_init_step_may_have_exited_too_by_exact_name(self) -> None:  # A356: the step that writes the per-tenant Trust login files
        declared = {"core", "model", "ui", "postgres", "temporal", "grant-keys-init", "trust-dsn-init"}
        self.assertEqual(matrix.live_findings("bench", declared, declared - {"grant-keys-init", "trust-dsn-init"}), [])
        declared = {"core", "model", "ui", "postgres", "temporal", "trust-dsn-init-2"}
        self.assertEqual(matrix.live_findings("bench", declared, declared - {"trust-dsn-init-2"}), ["bench: trust-dsn-init-2 should be running and is not"])

    # A477 (interim): the bench settings of the gateway, the worker and recovery, with the ordering constraints the loaders enforce (the activity timeout and the recovery idle time must exceed the run
    # timeout, or a live run could be picked up twice). The table is checked by number against the resolved configuration, and in itself.
    def test_the_a477_values_are_in_the_table_and_obey_their_ordering(self) -> None:
        want = {'NOEMA_INPUT_DEDUP_WINDOW': '5m', 'NOEMA_INPUT_RECORD_TIMEOUT': '5s', 'NOEMA_INVESTIGATION_QUEUE': '64', 'NOEMA_INVESTIGATION_WORKERS': '1', 'NOEMA_INVESTIGATION_RUN_TIMEOUT_MS': '180000', 'NOEMA_INVESTIGATION_START_TIMEOUT_MS': '30000', 'NOEMA_EDGE_KEY_MAX_ACTIVE': '2', 'NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS': '240000', 'NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS': '30000', 'NOEMA_STM_RECOVERY_IDLE_MS': '240000', 'NOEMA_STM_RECOVERY_BATCH_SIZE': '50'}
        for name, value in want.items():
            self.assertEqual(profiles.BENCH_CORE_SETTINGS.get(name), value, name)
        self.assertEqual(profiles.settings_order_problems(profiles.BENCH_CORE_SETTINGS), [])

    # A506 (interim): the rows the final composition reads, each checked by value; the grant lifetime must not exceed the record lifetime (EL's loader refuses it if it does).
    def test_the_a506_rows_are_in_the_table_and_the_lifetimes_are_ordered(self) -> None:
        want = {'NOEMA_RECORD_READ_DSN_DIR': '/run/record-read-dsn', 'NOEMA_ORCHESTRATOR_DSN_DIR': '/run/orchestrator-dsn', 'NOEMA_EDGE_DSN_DIR': '/run/edge-dsn', 'NOEMA_ORCHESTRATOR_KEY_DIR': '/run/orchestrator-keys', 'NOEMA_EDGE_KEY_DIR': '/run/edge-keys', 'NOEMA_INPUT_HASH_KEY_FILE': '/run/input-hash/key', 'NOEMA_ORCHESTRATOR_KEY_MAX_ACTIVE': '2', 'NOEMA_TEMPORAL_NAMESPACE': 'default', 'NOEMA_TEMPORAL_TASK_QUEUE': 'noema-investigations', 'NOEMA_TEMPORAL_TLS_CA_FILE': '/run/tls/ca.crt', 'NOEMA_TRUST_ESCALATION_GRAPH': 'fin.graph.escalate', 'NOEMA_TRUST_GRANT_LIFETIME_MS': '600000', 'NOEMA_TRUST_RECORD_LIFETIME_MS': '600000', 'NOEMA_TRUST_TX_TIMEOUT_MS': '10000'}
        for name, value in want.items():
            self.assertEqual(profiles.BENCH_CORE_SETTINGS.get(name), value, name)
        self.assertEqual(profiles.settings_order_problems(profiles.BENCH_CORE_SETTINGS), [])
        for grant, record in (("600001", "600000"), ("x", "600000"), ("600000", "")):
            bad = dict(profiles.BENCH_CORE_SETTINGS, NOEMA_TRUST_GRANT_LIFETIME_MS=grant, NOEMA_TRUST_RECORD_LIFETIME_MS=record)
            self.assertTrue(profiles.settings_order_problems(bad), (grant, record))

    def test_a_table_that_breaks_an_ordering_is_found(self) -> None:
        table = dict(profiles.BENCH_CORE_SETTINGS)
        for name, value in (("NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS", "180000"), ("NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS", "100000"), ("NOEMA_STM_RECOVERY_IDLE_MS", "180000"), ("NOEMA_STM_RECOVERY_IDLE_MS", "1"),
                             ("NOEMA_INVESTIGATION_RUN_TIMEOUT_MS", "240000"), ("NOEMA_INVESTIGATION_RUN_TIMEOUT_MS", "x")):
            bad = dict(table, **{name: value})
            self.assertTrue(profiles.settings_order_problems(bad), (name, value))

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
