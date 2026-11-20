#!/usr/bin/env python3
"""SEC-07: the cross-scope scanner finds a value of one scope on another scope's surface, and only that (REQ-SEC-003, REQ-SEC-004; PERD 44.1). All tokens are SYNTHETIC.

Run: python3 -I tools/ci/test_scopescan.py
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
import scopescan  # noqa: E402

A, A1, A2, B = "ten_alpha", "usr_a1", "usr_a2", "ten_beta"
TOK = {A: "MARK-alpha-7f3k9q2m", A1: "MARK-a1-x81p0d4z", A2: "MARK-a2-q55w2e8r", B: "MARK-beta-h4n7c1v6"}
REGISTRY = {"scopes": [
    {"id": A, "tokens": [A, TOK[A]]},
    {"id": A1, "parent": A, "tokens": ["usr_a1abcd", TOK[A1]]},
    {"id": A2, "parent": A, "tokens": ["usr_a2abcd", TOK[A2]]},
    {"id": B, "tokens": [B + "x", TOK[B]]},
]}


class Case(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        self.reg_path = self.dir / "registry.json"
        self.reg_path.write_text(json.dumps(REGISTRY), encoding="utf-8")

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def registry(self) -> scopescan.Registry:
        return scopescan.load_registry(self.reg_path)

    def surface(self, scope: str, text: str, kind: str = "api", field: str | None = None):
        f = self.dir / f"s{len(list(self.dir.iterdir()))}.txt"
        f.write_text(text, encoding="utf-8")
        return scopescan.Surface("surf", kind, scope, field, (str(f),))

    def scan(self, scope: str, text: str, kind: str = "api", field: str | None = None):
        s = self.surface(scope, text, kind, field)
        return scopescan.scan_surfaces([s], self.registry())[0]


class Detection(Case):
    def test_another_tenants_token_on_a_tenants_surface_is_a_disclosure(self) -> None:
        found = self.scan(A, f"order for {TOK[B]} was served\n")
        self.assertEqual([(d.owner, d.line) for d in found], [(B, 1)])

    def test_the_scopes_own_tokens_are_not(self) -> None:
        self.assertEqual(self.scan(A, f"{TOK[A]} and ten_alpha\n"), [])

    def test_a_principal_may_see_its_tenant_and_a_tenant_may_see_its_principals(self) -> None:
        self.assertEqual(self.scan(A1, f"{TOK[A]} {TOK[A1]}\n"), [])
        self.assertEqual(self.scan(A, f"{TOK[A1]} {TOK[A2]}\n"), [])

    def test_a_sibling_principal_is_a_disclosure(self) -> None:
        found = self.scan(A1, f"{TOK[A2]}\n")
        self.assertEqual([d.owner for d in found], [A2])

    def test_a_token_of_another_tenants_principal_is_a_disclosure_on_a_principal_surface(self) -> None:
        self.assertEqual([d.owner for d in self.scan(A1, f"{TOK[B]}\n")], [B])

    def test_a_token_is_not_found_inside_a_longer_one(self) -> None:
        self.assertEqual(self.scan(A, f"{TOK[B]}9 and x{TOK[B]} and {TOK[B]}_z\n"), [])
        self.assertEqual(len(self.scan(A, f"[{TOK[B]}] ({TOK[B]}), {TOK[B]}.\n")), 1)

    def test_every_line_is_judged_and_the_line_number_is_right(self) -> None:
        found = self.scan(A, f"fine\n{TOK[B]}\nfine\n{TOK[B]}\n")
        self.assertEqual([d.line for d in found], [2, 4])

    def test_a_star_surface_judges_each_line_by_its_own_scope_member(self) -> None:
        text = json.dumps({"tenant_id": A, "msg": TOK[A]}) + "\n" + json.dumps({"tenant_id": A, "msg": TOK[B]}) + "\n" + json.dumps({"tenant_id": B, "msg": TOK[B]}) + "\n"
        found = self.scan("*", text, "logs", "tenant_id")
        self.assertEqual([(d.owner, d.line) for d in found], [(B, 2)])

    def test_an_unscoped_line_may_hold_one_scope_but_not_two(self) -> None:
        self.assertEqual(self.scan("*", f"startup {TOK[A]}\n", "logs", "tenant_id"), [])
        found = self.scan("*", f"{TOK[A]} and {TOK[B]}\n", "logs", "tenant_id")
        self.assertEqual(sorted(d.owner for d in found), [A, B])
        self.assertEqual(self.scan("*", f"{TOK[A]} and {TOK[A1]}\n", "logs", "tenant_id"), [])

    def test_a_scope_member_naming_an_unknown_scope_is_unscoped(self) -> None:
        text = json.dumps({"tenant_id": "ten_nobody", "msg": f"{TOK[A]} {TOK[B]}"}) + "\n"
        self.assertEqual(len(self.scan("*", text, "logs", "tenant_id")), 2)

    def test_a_token_that_starts_another_scopes_longer_token_is_attributed_to_the_longer(self) -> None:
        reg = {"scopes": [{"id": A, "tokens": [A, "MARK-gamma"]}, {"id": B, "tokens": [B + "x", "MARK-gamma-7f3k9q2m"]}]}
        self.reg_path.write_text(json.dumps(reg), encoding="utf-8")
        found = self.scan(A, "see MARK-gamma-7f3k9q2m here\n")
        self.assertEqual([d.owner for d in found], [B])

    def test_a_line_whose_scope_member_is_unknown_is_unscoped_and_one_scope_in_it_is_fine(self) -> None:
        text = json.dumps({"tenant_id": "ten_nobody", "msg": TOK[A]}) + "\n"
        self.assertEqual(self.scan("*", text, "logs", "tenant_id"), [])

    def test_the_fingerprint_matches_one_value_and_the_token_is_never_in_the_text(self) -> None:
        found = self.scan(A, f"{TOK[B]}\n{TOK[B]}\n")
        self.assertEqual(len({d.fingerprint for d in found}), 1)
        self.assertNotIn(TOK[B], str(found[0]))


class Inputs(Case):
    def load(self, doc) -> scopescan.Registry:
        self.reg_path.write_text(json.dumps(doc), encoding="utf-8")
        return scopescan.load_registry(self.reg_path)

    def test_the_registry_is_refused_when_it_cannot_prove_anything(self) -> None:
        bad = {
            "one scope": {"scopes": [REGISTRY["scopes"][0]]},
            "no scopes key": {},
            "a short token": {"scopes": [{"id": A, "tokens": ["abc"]}, REGISTRY["scopes"][3]]},
            "a padded token": {"scopes": [{"id": A, "tokens": [" " + TOK[A]]}, REGISTRY["scopes"][3]]},
            "a token in two scopes": {"scopes": [{"id": A, "tokens": [TOK[A]]}, {"id": B, "tokens": [TOK[A]]}]},
            "no token": {"scopes": [{"id": A, "tokens": []}, REGISTRY["scopes"][3]]},
            "a repeated id": {"scopes": [REGISTRY["scopes"][0], REGISTRY["scopes"][0]]},
            "an unknown parent": {"scopes": [{"id": A, "parent": "ten_none", "tokens": [TOK[A]]}, REGISTRY["scopes"][3]]},
            "an id off its shape": {"scopes": [{"id": "a b", "tokens": [TOK[A]]}, REGISTRY["scopes"][3]]},
        }
        for label, doc in bad.items():
            with self.subTest(label), self.assertRaises(scopescan.InputError):
                self.load(doc)

    def test_a_missing_or_broken_registry_file(self) -> None:
        with self.assertRaises(scopescan.InputError):
            scopescan.load_registry(self.dir / "absent.json")
        self.reg_path.write_text("{not json", encoding="utf-8")
        with self.assertRaises(scopescan.InputError):
            scopescan.load_registry(self.reg_path)

    def test_the_surfaces_are_refused_when_they_are_not_usable(self) -> None:
        reg = self.registry()
        ok = {"name": "a", "kind": "api", "scope": A, "paths": ["x"]}
        bad = {
            "no surface": {"surfaces": []},
            "a kind outside the enum": {"surfaces": [{**ok, "kind": "whatever"}]},
            "an unknown scope": {"surfaces": [{**ok, "scope": "ten_nobody"}]},
            "a star without its field": {"surfaces": [{**ok, "scope": "*"}]},
            "a field on a scoped surface": {"surfaces": [{**ok, "scope_field": "tenant_id"}]},
            "no path": {"surfaces": [{**ok, "paths": []}]},
            "no name": {"surfaces": [{**ok, "name": ""}]},
        }
        path = self.dir / "surfaces.json"
        for label, doc in bad.items():
            path.write_text(json.dumps(doc), encoding="utf-8")
            with self.subTest(label), self.assertRaises(scopescan.InputError):
                scopescan.load_surfaces(path, reg)

    def test_a_scan_that_cannot_read_a_file_is_not_clean(self) -> None:
        reg = self.registry()
        for label, make in {
            "a missing path": lambda: self.dir / "absent",
            "a binary file": lambda: self._binary(),
        }.items():
            s = scopescan.Surface("s", "api", A, None, (str(make()),))
            with self.subTest(label), self.assertRaises(scopescan.InputError):
                scopescan.scan_surfaces([s], reg)

    def test_a_path_the_walk_cannot_list_or_a_link_is_not_clean(self) -> None:
        reg = self.registry()
        f = self.dir / "ok.txt"
        f.write_text("fine\n", encoding="utf-8")

        def broken(root, found):
            found.append(scopescan.leakscan.Finding(str(root), 0, "symlink", 0, "-"))
            return [f]

        with mock.patch.object(scopescan.leakscan, "_files_of", broken), self.assertRaises(scopescan.InputError):
            scopescan.scan_surfaces([scopescan.Surface("s", "api", A, None, (str(f),))], reg)

    def _binary(self) -> Path:
        p = self.dir / "b.bin"
        p.write_bytes(b"\x00\xff\xfe\x00")
        return p


class Command(Case):
    def files(self, surface_text: str):
        reg, sur = self.dir / "reg.json", self.dir / "sur.json"
        data = self.dir / "data"
        data.mkdir(exist_ok=True)
        (data / "api.txt").write_text(surface_text, encoding="utf-8")
        sur.write_text(json.dumps({"surfaces": [{"name": "api-alpha", "kind": "api", "scope": A, "paths": [str(data)]}]}), encoding="utf-8")
        return reg, sur

    def run_main(self, args):
        out = io.StringIO()
        with redirect_stdout(out):
            code = scopescan.main(args)
        return code, out.getvalue()

    def test_clean_exits_zero_and_leak_exits_one_without_printing_the_token(self) -> None:
        reg, sur = self.files(f"{TOK[A]} fine\n")
        self.assertEqual(self.run_main(["--registry", str(self.reg_path), "--surfaces", str(sur)])[0], 0)
        reg, sur = self.files(f"{TOK[B]} leaked\n")
        code, out = self.run_main(["--registry", str(self.reg_path), "--surfaces", str(sur), "--metric", str(self.dir / "m.prom")])
        self.assertEqual(code, 1)
        self.assertNotIn(TOK[B], out)
        self.assertIn("scope ten_beta", out)

    def test_nothing_scanned_is_exit_two(self) -> None:
        reg, sur = self.files("")
        code, out = self.run_main(["--registry", str(self.reg_path), "--surfaces", str(sur)])
        self.assertEqual(code, 2)
        self.assertIn("looked at nothing", out)

    def test_a_bad_registry_is_exit_two(self) -> None:
        reg, sur = self.files("x\n")
        self.reg_path.write_text("{}", encoding="utf-8")
        self.assertEqual(self.run_main(["--registry", str(self.reg_path), "--surfaces", str(sur)])[0], 2)

    def test_the_metric_has_every_surface_kind_and_counts_the_disclosures(self) -> None:
        metric = self.dir / "m.prom"
        reg, sur = self.files(f"{TOK[B]}\n{TOK[B]}\n")
        self.run_main(["--registry", str(self.reg_path), "--surfaces", str(sur), "--metric", str(metric)])
        text = metric.read_text(encoding="utf-8")
        for kind in scopescan.SURFACES:
            self.assertIn(f'noema_cross_scope_disclosure_total{{surface="{kind}"}}', text)
        self.assertIn('noema_cross_scope_disclosure_total{surface="api"} 2', text)
        self.assertIn('noema_cross_scope_disclosure_total{surface="logs"} 0', text)
        self.assertIn("# TYPE noema_cross_scope_disclosure_total counter", text)
        self.assertNotIn(TOK[B], text)

    def test_an_empty_run_has_a_zero_line_for_every_kind(self) -> None:
        text = scopescan.render_metric([])
        self.assertEqual(sum(1 for ln in text.splitlines() if ln.startswith("noema_cross_scope_disclosure_total{")), len(scopescan.SURFACES))


if __name__ == "__main__":
    unittest.main()
