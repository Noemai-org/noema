"""The pack manifest schema (PCK-02; A333, A332): packs/schemas/manifest.v1.json and the committed packs/fintech/manifest.v1.json.

The schema is closed (additionalProperties false at every level), the digest is 64 lowercase hex digits, a path is a plain relative path
with no `.` or `..` component, and the two ids the core expects are named as `<id>@<version>`. The path rules that need the disk (missing
file, symlink, hash) are packcheck's (tools/packcheck/test_manifest.py). All data SYNTHETIC.
"""
import copy
import json
import unittest
from pathlib import Path

import jsonschema

PACKS = Path(__file__).resolve().parents[1]
SCHEMA = PACKS / "schemas/manifest.v1.json"
MANIFEST = PACKS / "fintech/manifest.v1.json"
HEX = "0123456789abcdef" * 4


def schema() -> dict:
    assert SCHEMA.is_file(), "packs/schemas/manifest.v1.json does not exist (A333)"
    return json.loads(SCHEMA.read_text(encoding="utf-8"))


def good() -> dict:
    return {
        "manifest_id": "fin.manifest",
        "version": 1,
        "label": "SYNTHETIC",
        "note": "SYNTHETIC.",
        "expected": {"evidence_mapping_id": "fin.map.v1@1", "recon_rule_set_id": "fin.recon.rules@1", "injection_patterns_id": "fin.ctx.injection_patterns@1"},
        "files": [
            {"kind": "pack", "path": "fintech_v1.json", "sha256": HEX},
            {"kind": "evidence_mapping", "path": "fintech/evidence_mapping/mapping.v1.json", "sha256": HEX},
            {"kind": "recon_rules", "path": "fintech/recon_rules/rules.v1.json", "sha256": HEX},
            {"kind": "injection_patterns", "path": "fintech/context/injection_patterns.v1.json", "sha256": HEX},  # A645
        ],
    }


def errors(doc) -> list:
    return list(jsonschema.Draft202012Validator(schema()).iter_errors(doc))


class ManifestSchema(unittest.TestCase):
    def test_the_schema_is_a_valid_2020_12_schema_with_the_folders_id_convention(self):
        s = schema()
        jsonschema.Draft202012Validator.check_schema(s)
        self.assertEqual(s["$schema"], "https://json-schema.org/draft/2020-12/schema")
        self.assertEqual(s["$id"], "https://packs.noema.invalid/fintech/schemas/manifest.v1.json")
        self.assertEqual(s["type"], "object")

    def test_every_object_is_closed(self):
        s = schema()
        self.assertIs(s["additionalProperties"], False)
        self.assertIs(s["properties"]["expected"]["additionalProperties"], False)
        self.assertIs(s["properties"]["files"]["items"]["additionalProperties"], False)
        for obj in (s, s["properties"]["expected"], s["properties"]["files"]["items"]):
            optional = {"note"} if obj is s else {"injection_patterns_id"} if obj is s["properties"]["expected"] else set()  # A664: the patterns id is optional below the gate, and the gate requires it
            self.assertEqual(set(obj["required"]), set(obj["properties"]) - optional)

    def test_the_patterns_are_required_from_manifest_version_4_and_optional_below_it(self):
        """A664 (interim; the A579 pattern): a manifest of version 4 or later lists the patterns file and names its id; below that both may be absent; they go together at every version."""

        def with_version(v, files=True, ident=True):
            d = good()
            d["version"] = v
            if not files:
                d["files"] = [f for f in d["files"] if f["kind"] != "injection_patterns"]
            if not ident:
                del d["expected"]["injection_patterns_id"]
            return d

        for v in (1, 2, 3):
            self.assertEqual(errors(with_version(v, files=False, ident=False)), [], f"version {v} without the patterns must validate")
        for v in (4, 5, 99):
            self.assertEqual(errors(with_version(v)), [], f"version {v} with the patterns must validate")
            self.assertTrue(errors(with_version(v, files=False, ident=False)), f"version {v} without the patterns must be refused")
            self.assertTrue(errors(with_version(v, files=False)), f"version {v}: an id and no file must be refused")
            self.assertTrue(errors(with_version(v, ident=False)), f"version {v}: a file and no id must be refused")
        for v in (1, 3, 4):
            self.assertTrue(errors(with_version(v, files=False)), f"version {v}: an id without its file must be refused")
            self.assertTrue(errors(with_version(v, ident=False)), f"version {v}: a file without its id must be refused")

    def test_two_pattern_files_are_refused_at_every_version(self):
        for v in (1, 3, 4):
            d = good()
            d["version"] = v
            d["files"].append({"kind": "injection_patterns", "path": "fintech/context/other.json", "sha256": HEX})
            self.assertTrue(errors(d), f"version {v} with two pattern files must be refused")

    def test_the_real_manifest_and_a_good_one_validate(self):
        self.assertEqual(errors(good()), [])
        self.assertTrue(MANIFEST.is_file(), "packs/fintech/manifest.v1.json does not exist (A333)")
        self.assertEqual(errors(json.loads(MANIFEST.read_text(encoding="utf-8"))), [])

    def test_kinds_are_a_closed_list(self):
        kinds = schema()["properties"]["files"]["items"]["properties"]["kind"]["enum"]
        self.assertEqual(sorted(kinds), ["action_graph", "evidence_mapping", "injection_patterns", "pack", "recon_rules"])  # A510: the action graphs are pinned too; A645: and the injection patterns

    def test_the_action_graph_kind_may_repeat_and_an_old_manifest_without_it_still_validates(self):
        doc = good()
        self.assertEqual(errors(doc), [])  # a manifest with no action graph is still valid: the action graphs are additive (A510); the patterns file is required (A645)
        for name in ("g_one", "g_two", "g_three"):
            doc["files"].append({"kind": "action_graph", "path": f"fintech/graph/{name}.json", "sha256": HEX})
        self.assertEqual(errors(doc), [])

    def test_each_bad_manifest_is_refused(self):
        def put(path, value):
            def go(d):
                node = d
                for p in path[:-1]:
                    node = node[p]
                node[path[-1]] = value
            return go

        cases = {
            "extra top field": put(["extra"], 1),
            "extra file field": put(["files", 0, "mode"], "0644"),
            "extra expected field": put(["expected", "x"], "a@1"),
            "wrong label": put(["label"], "REAL"),
            "version 0": put(["version"], 0),
            "version float": put(["version"], 1.5),
            "no files": put(["files"], []),
            "too many files": put(["files"], [good()["files"][0]] * 17),
            "unknown kind": put(["files", 0, "kind"], "lexicon"),
            "uppercase digest": put(["files", 0, "sha256"], "A" * 64),
            "short digest": put(["files", 0, "sha256"], "a" * 63),
            "digest with newline": put(["files", 0, "sha256"], "a" * 64 + "\n"),
            "dot dot": put(["files", 0, "path"], "../fintech_v1.json"),
            "dot dot inside": put(["files", 0, "path"], "fintech/../fintech_v1.json"),
            "absolute": put(["files", 0, "path"], "/fintech_v1.json"),
            "backslash": put(["files", 0, "path"], "fintech\\x.json"),
            "dot segment": put(["files", 0, "path"], "./fintech_v1.json"),
            "double slash": put(["files", 0, "path"], "fintech//x.json"),
            "trailing slash": put(["files", 0, "path"], "fintech/"),
            "hidden file": put(["files", 0, "path"], "fintech/.x.json"),
            "path with newline": put(["files", 0, "path"], "fintech_v1.json\n"),
            "empty path": put(["files", 0, "path"], ""),
            "id without version": put(["expected", "evidence_mapping_id"], "fin.map.v1"),
            "id version 0": put(["expected", "recon_rule_set_id"], "fin.recon.rules@0"),
            "id with newline": put(["expected", "recon_rule_set_id"], "fin.recon.rules@1\n"),
            "id with space": put(["expected", "evidence_mapping_id"], "fin map@1"),
        }
        for name, mutate in cases.items():
            with self.subTest(name):
                doc = copy.deepcopy(good())
                mutate(doc)
                self.assertTrue(errors(doc), f"{name}: the schema accepted it")

    def test_a_missing_required_field_is_refused(self):
        for key in ("manifest_id", "version", "label", "expected", "files"):
            with self.subTest(key):
                doc = good()
                del doc[key]
                self.assertTrue(errors(doc))
        for key in ("kind", "path", "sha256"):
            with self.subTest(key):
                doc = good()
                del doc["files"][0][key]
                self.assertTrue(errors(doc))
        for key in ("evidence_mapping_id", "recon_rule_set_id"):
            with self.subTest(key):
                doc = good()
                del doc["expected"][key]
                self.assertTrue(errors(doc))


if __name__ == "__main__":
    unittest.main()
