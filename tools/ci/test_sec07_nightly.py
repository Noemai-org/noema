#!/usr/bin/env python3
"""SEC-07: planted leaks on every surface, the scanners that must see them, and the nightly result (REQ-SEC-003, REQ-SEC-007; PERD 44.1). All values are SYNTHETIC.

Run: python3 -I tools/ci/test_sec07_nightly.py
"""
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import leakscan  # noqa: E402
import scopescan  # noqa: E402
import sec07_nightly as sec  # noqa: E402

A, B = "ten_alpha", "ten_beta"
TA, TB = "MARK-alpha-7f3k9q2m", "MARK-beta-h4n7c1v6"
REGISTRY = {"scopes": [{"id": A, "tokens": [A, TA]}, {"id": "usr_a1", "parent": A, "tokens": ["usr_a1abcd", "MARK-a1-x81p0d4z"]}, {"id": B, "tokens": [B + "x", TB]}]}
KINDS = ("api", "explanations", "logs", "record", "traces")
# SYNTHETIC secret-shaped line, built at run time so no secret-shaped literal sits in the repository (the way test_leakscan.py does it).
SECRET_LINE = "api_" + "key=" + "abcdef" + "0123456789" + "abcd"


class Run(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        self.data = self.dir / "data"
        self.data.mkdir()
        self.work = self.dir / "work"
        (self.dir / "registry.json").write_text(json.dumps(REGISTRY), encoding="utf-8")
        self.paths: dict[str, Path] = {}
        surfaces = []
        for kind in KINDS:
            folder = self.data / kind
            folder.mkdir()
            (folder / "out.jsonl").write_text(json.dumps({"tenant_id": A, "msg": f"served {TA} ok"}) + "\n", encoding="utf-8")
            self.paths[kind] = folder
            scope = "*" if kind in ("logs", "traces") else A
            entry = {"name": f"{kind}-surface", "kind": kind, "scope": scope, "paths": [str(folder)]}
            if scope == "*":
                entry["scope_field"] = "tenant_id"
            surfaces.append(entry)
        (self.dir / "surfaces.json").write_text(json.dumps({"surfaces": surfaces}), encoding="utf-8")
        self.metric = self.dir / "m.prom"

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def go(self, *extra: str, selftest: bool = False):
        args = ["--registry", str(self.dir / "registry.json"), "--surfaces", str(self.dir / "surfaces.json"), "--workdir", str(self.work)]
        args += ["--selftest"] if selftest else ["--metric", str(self.metric)]
        out = io.StringIO()
        with redirect_stdout(out):
            code = sec.main(args + list(extra))
        return code, out.getvalue()


class Probe(Run):
    def test_the_selftest_passes_when_the_scanners_see_every_planted_leak_on_every_surface(self) -> None:
        code, out = self.go(selftest=True)
        self.assertEqual(code, 0, out)
        self.assertIn("each of 5 surfaces", out)

    def test_the_probe_writes_one_file_per_planted_leak_on_every_surface(self) -> None:
        self.go(selftest=True)
        folders = sorted(p.name for p in (self.work / "probe").iterdir())
        self.assertEqual(len(folders), 5)
        for folder in (self.work / "probe").iterdir():
            names = {f.name for f in folder.iterdir()}
            for kind in (*sec.PROBE_KINDS, "cross_scope"):
                self.assertIn(f"planted-{kind}.jsonl", names)

    def test_a_scanner_that_misses_one_planted_kind_is_not_run_never_clean(self) -> None:
        for label, target in {
            "account number": mock.patch.object(leakscan, "_digit_findings", return_value=[]),
            "secrets": mock.patch.object(leakscan, "_secret_findings", return_value=[]),
        }.items():
            with self.subTest(label), target:
                code, out = self.go(selftest=True)
                self.assertEqual(code, 3, out)
                self.assertIn("NOT RUN", out)

    def test_a_scope_scanner_that_misses_the_foreign_token_is_not_run(self) -> None:
        with mock.patch.object(scopescan, "scan_text", return_value=[]):
            code, out = self.go(selftest=True)
        self.assertEqual(code, 3, out)
        self.assertIn("cross_scope", out)

    def test_each_missing_kind_is_named_with_its_surface(self) -> None:
        with mock.patch.object(leakscan, "_digit_findings", return_value=[]):
            _, out = self.go(selftest=True)
        for kind in KINDS:
            self.assertIn(f"{kind}-surface ({kind}): account_number", out)

    def test_a_registry_with_no_scope_outside_a_surfaces_family_is_refused(self) -> None:
        (self.dir / "registry.json").write_text(json.dumps({"scopes": [REGISTRY["scopes"][0], REGISTRY["scopes"][1]]}), encoding="utf-8")
        code, out = self.go(selftest=True)
        self.assertEqual(code, 2, out)

    def test_no_planted_value_or_token_is_ever_printed(self) -> None:
        _, out = self.go(selftest=True)
        for token in (TA, TB, "CANARY-"):
            self.assertNotIn(token, out)


class Scan(Run):
    def read_metric(self) -> str:
        return self.metric.read_text(encoding="utf-8")

    def test_a_clean_run_exits_zero_and_writes_every_counter_at_zero(self) -> None:
        code, out = self.go()
        self.assertEqual(code, 0, out)
        text = self.read_metric()
        for kind in scopescan.SURFACES:
            self.assertIn(f'noema_cross_scope_disclosure_total{{surface="{kind}"}} 0', text)
            self.assertIn(f'noema_leak_scan_findings_total{{surface="{kind}"}} 0', text)

    def test_a_full_account_number_on_a_surface_fails_the_run_and_the_counter_names_that_surface(self) -> None:
        (self.paths["api"] / "out.jsonl").write_text("a response with account 0123456789 in it\n", encoding="utf-8")
        code, out = self.go()
        self.assertEqual(code, 1, out)
        self.assertNotIn("0123456789", out)
        self.assertIn('noema_leak_scan_findings_total{surface="api"} 1', self.read_metric())
        self.assertIn('noema_leak_scan_findings_total{surface="logs"} 0', self.read_metric())

    def test_a_secret_in_the_record_surface_fails_the_run(self) -> None:
        (self.paths["record"] / "out.jsonl").write_text('{"payload": "' + SECRET_LINE + '"}\n', encoding="utf-8")
        code, out = self.go()
        self.assertEqual(code, 1, out)
        self.assertIn('noema_leak_scan_findings_total{surface="record"} 1', self.read_metric())

    def test_another_tenants_token_in_the_explanations_feeds_the_cross_scope_counter(self) -> None:
        (self.paths["explanations"] / "out.jsonl").write_text(f"your reference is {TB}\n", encoding="utf-8")
        code, out = self.go()
        self.assertEqual(code, 1, out)
        self.assertNotIn(TB, out)
        text = self.read_metric()
        self.assertIn('noema_cross_scope_disclosure_total{surface="explanations"} 1', text)
        self.assertIn('noema_cross_scope_disclosure_total{surface="api"} 0', text)

    def test_a_cross_tenant_line_in_the_logs_is_found_by_its_own_tenant_member(self) -> None:
        (self.paths["logs"] / "out.jsonl").write_text(json.dumps({"tenant_id": A, "msg": f"handled {TB}"}) + "\n", encoding="utf-8")
        code, _ = self.go()
        self.assertEqual(code, 1)
        self.assertIn('noema_cross_scope_disclosure_total{surface="logs"} 1', self.read_metric())

    def test_a_scan_that_looked_at_nothing_is_not_run(self) -> None:
        for kind in KINDS:
            (self.paths[kind] / "out.jsonl").write_text("", encoding="utf-8")
        code, out = self.go()
        self.assertEqual(code, 3, out)
        self.assertFalse(self.metric.exists())

    def test_a_missing_surface_path_is_bad_input_not_a_pass(self) -> None:
        (self.paths["traces"] / "out.jsonl").unlink()
        self.paths["traces"].rmdir()
        code, out = self.go()
        self.assertEqual(code, 2, out)

    def test_a_workdir_inside_a_scanned_path_is_refused(self) -> None:
        self.work = self.paths["api"] / "work"
        code, out = self.go()
        self.assertEqual(code, 2, out)
        self.assertIn("work directory", out)

    def test_a_real_run_needs_the_metric_path(self) -> None:
        out = io.StringIO()
        with redirect_stdout(out):
            code = sec.main(["--registry", str(self.dir / "registry.json"), "--surfaces", str(self.dir / "surfaces.json"), "--workdir", str(self.work)])
        self.assertEqual(code, 2)

    def test_the_allow_file_skips_the_digit_rule_only(self) -> None:
        (self.dir / "allow.txt").write_text(r"^port [0-9]+$" + "\n", encoding="utf-8")
        (self.paths["api"] / "out.jsonl").write_text("port 80801\n", encoding="utf-8")
        self.assertEqual(self.go("--allow", str(self.dir / "allow.txt"))[0], 0)
        (self.paths["api"] / "out.jsonl").write_text("port 80801\n" + SECRET_LINE + "\n", encoding="utf-8")
        code, _ = self.go("--allow", str(self.dir / "allow.txt"))
        self.assertEqual(code, 1)
        self.assertIn('noema_leak_scan_findings_total{surface="api"} 1', self.metric.read_text(encoding="utf-8"))

    def test_an_allow_pattern_that_matches_the_empty_string_is_refused(self) -> None:
        (self.dir / "allow.txt").write_text("x*\n", encoding="utf-8")
        self.assertEqual(self.go("--allow", str(self.dir / "allow.txt"))[0], 2)

    def test_the_probe_runs_before_the_real_scan_and_a_blind_scanner_never_reports_clean(self) -> None:
        with mock.patch.object(leakscan, "_digit_findings", return_value=[]):
            code, _ = self.go()
        self.assertEqual(code, 3)
        self.assertFalse(self.metric.exists())


if __name__ == "__main__":
    unittest.main()
