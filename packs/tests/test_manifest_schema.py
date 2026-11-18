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
        "expected": {"evidence_mapping_id": "fin.map.v1@1", "recon_rule_set_id": "fin.recon.rules@1"},
        "files": [
            {"kind": "pack", "path": "fintech_v1.json", "sha256": HEX},
            {"kind": "evidence_mapping", "path": "fintech/evidence_mapping/mapping.v1.json", "sha256": HEX},
            {"kind": "recon_rules", "path": "fintech/recon_rules/rules.v1.json", "sha256": HEX},
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
            self.assertEqual(set(obj["required"]), set(obj["properties"]) - ({"note"} if obj is s else set()))

    def test_the_real_manifest_and_a_good_one_validate(self):
        self.assertEqual(errors(good()), [])
        self.assertTrue(MANIFEST.is_file(), "packs/fintech/manifest.v1.json does not exist (A333)")
        self.assertEqual(errors(json.loads(MANIFEST.read_text(encoding="utf-8"))), [])

    def test_kinds_are_a_closed_list(self):
        kinds = schema()["properties"]["files"]["items"]["properties"]["kind"]["enum"]
        self.assertEqual(sorted(kinds), ["evidence_mapping", "pack", "recon_rules"])

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
            "version float": put(["version"], 1.0),
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
