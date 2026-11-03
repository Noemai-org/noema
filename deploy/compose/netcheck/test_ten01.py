#!/usr/bin/env python3
"""TEN-01 findings, offline (task ENV-14, internal). The pure function must pass on a clean stack and must fail on each way a
runtime container could reach the oracle. SYNTHETIC inputs. Run: python3 deploy/compose/netcheck/test_ten01.py"""
import copy
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location("matrix", pathlib.Path(__file__).resolve().parent / "matrix.py")
matrix = importlib.util.module_from_spec(spec)
spec.loader.exec_module(matrix)

CLEAN_CONFIG = {"services": {
    "core": {"environment": {"NOEMA_DB_USER": "noema_core", "NOEMA_DB_PASSWORD_FILE": "/run/secrets/noema_core_password"}, "secrets": ["noema_core_password"]},
    "model": {"environment": {"NOEMA_MODEL_ADDR": ":8081"}},
    "ui": {},
}}
REFUSED = {t: "refused" for t in (matrix.TARGETS[x] for x in matrix.ORACLE_TARGETS)}
CLEAN_OBSERVED = {s: dict(REFUSED) for s in matrix.RUNTIME}


class Ten01(unittest.TestCase):
    def test_a_clean_stack_has_no_finding(self) -> None:
        self.assertEqual(matrix.ten01_findings(CLEAN_OBSERVED, CLEAN_CONFIG), [])

    def test_an_open_oracle_path_is_a_finding_for_each_runtime_service(self) -> None:
        for service in matrix.RUNTIME:
            for target in matrix.ORACLE_TARGETS:
                obs = copy.deepcopy(CLEAN_OBSERVED)
                obs[service][matrix.TARGETS[target]] = "open"
                got = matrix.ten01_findings(obs, CLEAN_CONFIG)
                self.assertEqual(len(got), 1, (service, target, got))
                self.assertIn(f"{service} -> {target}", got[0])

    def test_a_missing_probe_result_is_a_finding_not_a_pass(self) -> None:
        obs = copy.deepcopy(CLEAN_OBSERVED)
        obs["core"] = {}
        self.assertEqual(len(matrix.ten01_findings(obs, CLEAN_CONFIG)), len(matrix.ORACLE_TARGETS))

    def test_an_oracle_credential_in_the_environment_is_a_finding(self) -> None:
        for name in ("MOCK_CASE_BENCH_TOKEN_FILE", "TUSHE_ADMIN_KEY", "ORACLE_URL", "BENCHMARK_HELDOUT_PATH"):
            cfg = copy.deepcopy(CLEAN_CONFIG)
            cfg["services"]["model"]["environment"][name] = "x"
            self.assertTrue(matrix.ten01_findings(CLEAN_OBSERVED, cfg), name)

    def test_an_oracle_credential_by_secret_or_volume_is_a_finding(self) -> None:
        cfg = copy.deepcopy(CLEAN_CONFIG)
        cfg["services"]["ui"]["secrets"] = ["mock_case_bench_token"]
        self.assertTrue(matrix.ten01_findings(CLEAN_OBSERVED, cfg))
        for vol in ("mock_case_bench_credential", "hostility_proxy_credentials", "mock_case_runtime_credential"):
            cfg = copy.deepcopy(CLEAN_CONFIG)
            cfg["services"]["core"]["volumes"] = [{"source": vol, "type": "volume"}]
            self.assertTrue(matrix.ten01_findings(CLEAN_OBSERVED, cfg), vol)

    def test_the_core_may_hold_its_own_database_secret_only(self) -> None:
        cfg = copy.deepcopy(CLEAN_CONFIG)
        cfg["services"]["core"]["secrets"] = ["noema_core_password", "postgres_superuser_password"]
        self.assertTrue(matrix.ten01_findings(CLEAN_OBSERVED, cfg))


if __name__ == "__main__":
    unittest.main()
