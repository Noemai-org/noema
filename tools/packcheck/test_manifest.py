"""packcheck against the pack manifest (PCK-02; A333, A332, A320 EVD-06 hash-pin; SoT INV-14, INV-21; AGENT-16).

`packs/fintech/manifest.v1.json` lists the pack files the core loads, each with its SHA-256, and names the evidence-mapping id and the
reconciliation rule-set id the core expects, so the core hard-codes neither (INV-14). packcheck must fail when:
a listed file's SHA-256 differs, a listed file is missing, a file of a listed kind is not listed, an id the manifest names is not the id the
file carries, or a path escapes the pack directory (`..`, absolute, symlink).

Every test builds its own small pack tree in a temporary folder (copies of the real files, so the ids and shapes are real) and breaks one thing,
so a test fails for the reason it names and for no other. Seeded fuzz, as in RoutingFuzz. All data SYNTHETIC.
"""
import copy
import hashlib
import json
import os
import random
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import checks  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent
PACKS = ROOT / "packs"
REAL_MANIFEST = PACKS / "fintech/manifest.v1.json"
MAPPING = "fintech/evidence_mapping/mapping.v1.json"
RULES = "fintech/recon_rules/rules.v1.json"
PACK = "fintech_v1.json"
LISTED = {"pack": PACK, "evidence_mapping": MAPPING, "recon_rules": RULES}
EXPECTED = {"evidence_mapping_id": "fin.map.v1@2", "recon_rule_set_id": "fin.recon.rules@1"}


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def good_manifest(root: Path) -> dict:
    return {
        "manifest_id": "fin.manifest",
        "version": 1,
        "label": "SYNTHETIC",
        "note": "SYNTHETIC test manifest.",
        "expected": dict(EXPECTED),
        "files": [{"kind": kind, "path": rel, "sha256": sha256(root / rel)} for kind, rel in LISTED.items()],
    }


def build_root(root: Path) -> Path:
    """A pack folder with the three real files and a good manifest. Returns the manifest path."""
    for rel in LISTED.values():
        (root / rel).parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(PACKS / rel, root / rel)
    path = root / "fintech/manifest.v1.json"
    path.write_text(json.dumps(good_manifest(root), indent=2), encoding="utf-8")
    return path


def write(path: Path, doc) -> None:
    path.write_text(json.dumps(doc, indent=2), encoding="utf-8")


def entry(doc: dict, kind: str) -> dict:
    return next(f for f in doc["files"] if f["kind"] == kind)


class Base(unittest.TestCase):
    def check(self, manifest: Path, root: Path):
        fn = getattr(checks, "check_manifest", None)
        if fn is None:
            self.fail("tools/packcheck/checks.py has no check_manifest(manifest_path, root) (PCK-02, A333)")
        return fn(manifest, root)

    def messages(self, problems) -> str:
        self.assertTrue(all(c == "manifest" for c, _ in problems), problems)
        return " | ".join(m for _, m in problems)

    def tree(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name) / "packs"
        root.mkdir()
        return root, build_root(root)

    def edit(self, root: Path, manifest: Path, fn):
        doc = json.loads(manifest.read_text(encoding="utf-8"))
        fn(doc)
        write(manifest, doc)
        return doc

    def fails(self, root: Path, manifest: Path, *fragments: str) -> None:
        problems = self.check(manifest, root)
        self.assertTrue(problems, "expected a manifest problem, got none")
        text = self.messages(problems)
        for f in fragments:
            self.assertIn(f, text)


class RealManifest(Base):
    """The committed manifest, checked against the committed files and against an independent computation."""

    def load(self) -> dict:
        self.assertTrue(REAL_MANIFEST.is_file(), "packs/fintech/manifest.v1.json does not exist (A333)")
        return json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))

    def test_the_real_manifest_passes(self):
        self.assertTrue(REAL_MANIFEST.is_file(), "packs/fintech/manifest.v1.json does not exist (A333)")
        self.assertEqual(self.check(REAL_MANIFEST, PACKS), [])

    def test_it_is_labelled_synthetic_and_lists_exactly_the_three_kinds(self):
        doc = self.load()
        self.assertEqual(doc["label"], "SYNTHETIC")
        self.assertEqual({f["kind"]: f["path"] for f in doc["files"]}, LISTED)
        self.assertEqual(len(doc["files"]), 3)

    def test_each_listed_hash_is_the_independent_sha256_of_the_file(self):
        for f in self.load()["files"]:
            with self.subTest(f["path"]):
                self.assertEqual(f["sha256"], sha256(PACKS / f["path"]))

    def test_the_expected_ids_are_the_ids_the_files_carry(self):
        doc = self.load()
        mapping = json.loads((PACKS / MAPPING).read_text(encoding="utf-8"))
        rules = json.loads((PACKS / RULES).read_text(encoding="utf-8"))
        self.assertEqual(doc["expected"], EXPECTED)
        self.assertEqual(doc["expected"]["evidence_mapping_id"], f"{mapping['mapping_id']}@{mapping['version']}")
        self.assertEqual(doc["expected"]["recon_rule_set_id"], f"{rules['ruleset_id']}@{rules['version']}")
        self.assertEqual(mapping["ruleset"], doc["expected"]["recon_rule_set_id"])

    def test_every_json_file_of_a_listed_kind_is_listed(self):
        listed = {f["path"] for f in self.load()["files"]}
        found = {str(p.relative_to(PACKS)) for d in ("fintech/evidence_mapping", "fintech/recon_rules") for p in (PACKS / d).rglob("*.json")}
        found |= {p.name for p in PACKS.glob("fintech_v*.json")}
        self.assertEqual(found, listed)

    def test_the_core_does_not_hard_code_the_ids(self):
        # INV-14: nothing in the core names the ids; the manifest does. The Go loader is another pull request, so only the absence is asserted.
        hits = [str(p) for p in (ROOT / "services/core").rglob("*.go") if not p.name.endswith("_test.go") and "fin.map.v1@2" in p.read_text(encoding="utf-8", errors="ignore")]
        self.assertEqual(hits, [])

    def test_the_makefile_runs_the_manifest_check(self):
        self.assertIn("tools/packcheck manifest packs/fintech/manifest.v1.json", (ROOT / "Makefile").read_text(encoding="utf-8"))


class GoodTree(Base):
    def test_a_good_tree_passes(self):
        root, manifest = self.tree()
        self.assertEqual(self.check(manifest, root), [])

    def test_a_file_that_is_not_a_listed_kind_is_not_flagged(self):
        # The recon_rules folder holds a README; only *.json files are of a kind. A conformance pack beside the pack is not a fintech pack file.
        root, manifest = self.tree()
        (root / "fintech/recon_rules/README.md").write_text("SYNTHETIC", encoding="utf-8")
        (root / "fintech/evidence_mapping/notes.txt").write_text("SYNTHETIC", encoding="utf-8")
        (root / "conformance").mkdir()
        (root / "conformance/conformance_telecom.json").write_text("{}", encoding="utf-8")
        (root / "fintech/other").mkdir()
        os.symlink(root / MAPPING, root / "unrelated_link.txt")  # a link in the pack folder that is not named like a pack file
        (root / "fintech/other/fintech_v2.json").write_text("{}", encoding="utf-8")  # the pack kind is the pack folder's own files only
        self.assertEqual(self.check(manifest, root), [])


class HashPin(Base):
    def test_a_changed_file_fails_for_each_kind(self):
        for kind, rel in LISTED.items():
            with self.subTest(kind):
                root, manifest = self.tree()
                with open(root / rel, "ab") as fh:
                    fh.write(b" ")
                self.fails(root, manifest, "sha256", rel)

    def test_a_changed_hash_in_the_manifest_fails_for_each_kind(self):
        for kind, rel in LISTED.items():
            with self.subTest(kind):
                root, manifest = self.tree()
                def flip(doc):
                    e = entry(doc, kind)
                    e["sha256"] = ("0" if e["sha256"][0] != "0" else "1") + e["sha256"][1:]
                self.edit(root, manifest, flip)
                self.fails(root, manifest, "sha256", rel)

    def test_the_message_names_both_hashes(self):
        root, manifest = self.tree()
        listed = sha256(root / MAPPING)
        with open(root / MAPPING, "ab") as fh:
            fh.write(b"\n")
        self.fails(root, manifest, listed, sha256(root / MAPPING))

    def test_the_hash_is_over_the_raw_bytes(self):
        # A CRLF copy of the same text is a different file: no newline normalisation (the pin is a byte pin).
        root, manifest = self.tree()
        data = (root / RULES).read_bytes()
        (root / RULES).write_bytes(data.replace(b"\n", b"\r\n"))
        self.fails(root, manifest, "sha256", RULES)

    def test_any_single_byte_flip_is_caught(self):
        rng = random.Random(20261118)
        for kind, rel in LISTED.items():
            root, manifest = self.tree()
            original = (root / rel).read_bytes()
            for _ in range(40):
                data = bytearray(original)
                data[rng.randrange(len(data))] ^= 1 << rng.randrange(8)
                (root / rel).write_bytes(bytes(data))
                problems = self.check(manifest, root)
                self.assertTrue(any("sha256" in m and rel in m for c, m in problems), (kind, problems))

    def test_an_uppercase_or_short_or_padded_hash_fails(self):
        for bad in ("A" * 64, "a" * 63, "a" * 65, "g" * 64, "", sha256(Path(__file__)) + "\n"):
            with self.subTest(bad[:10]):
                root, manifest = self.tree()
                self.edit(root, manifest, lambda d, b=bad: entry(d, "recon_rules").__setitem__("sha256", b))
                self.assertTrue(self.check(manifest, root))
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: entry(d, "recon_rules").__setitem__("sha256", entry(d, "recon_rules")["sha256"].upper()))
        self.assertTrue(self.check(manifest, root), "an uppercase digest is not accepted: the manifest form is lowercase hex")


class MissingFile(Base):
    def test_a_listed_file_that_is_missing_fails_for_each_kind(self):
        for kind, rel in LISTED.items():
            with self.subTest(kind):
                root, manifest = self.tree()
                (root / rel).unlink()
                self.fails(root, manifest, rel, "does not exist")

    def test_a_listed_path_that_is_a_folder_is_not_a_file(self):
        root, manifest = self.tree()
        (root / RULES).unlink()
        (root / RULES).mkdir()
        self.fails(root, manifest, RULES, "not a regular file")

    def test_a_listed_file_that_was_never_there_fails(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: entry(d, "evidence_mapping").__setitem__("path", "fintech/evidence_mapping/mapping.v2.json"))
        self.fails(root, manifest, "mapping.v2.json", "does not exist")


class UnlistedFile(Base):
    def test_an_unlisted_file_of_each_listed_kind_fails(self):
        for rel in ("fintech/evidence_mapping/mapping.v2.json", "fintech/recon_rules/rules.v2.json", "fintech_v2.json"):
            with self.subTest(rel):
                root, manifest = self.tree()
                (root / rel).write_text("{}", encoding="utf-8")
                self.fails(root, manifest, rel, "not listed")

    def test_a_nested_unlisted_file_fails(self):
        root, manifest = self.tree()
        (root / "fintech/recon_rules/old").mkdir()
        (root / "fintech/recon_rules/old/rules.v0.json").write_text("{}", encoding="utf-8")
        self.fails(root, manifest, "old/rules.v0.json", "not listed")

    def test_an_unlisted_symlink_in_a_kind_folder_fails(self):
        root, manifest = self.tree()
        os.symlink(root / MAPPING, root / "fintech/evidence_mapping/alias.json")
        self.fails(root, manifest, "alias.json", "symlink")

    def test_an_unlisted_symlinked_folder_in_a_kind_folder_fails(self):
        root, manifest = self.tree()
        (root.parent / "elsewhere").mkdir()
        (root.parent / "elsewhere/inner.json").write_text("{}", encoding="utf-8")
        os.symlink(root.parent / "elsewhere", root / "fintech/recon_rules/more")
        self.fails(root, manifest, "fintech/recon_rules/more", "symlink")
        self.assertNotIn("inner.json", self.messages(self.check(manifest, root)), "the link is reported, not followed")

    def test_a_listed_kind_entry_must_sit_where_its_kind_lives(self):
        # A real mapping (right bytes, right id) listed from the wrong folder, and the pack listed from a subfolder.
        for kind, rel, copy_to in (("evidence_mapping", MAPPING, "fintech/recon_rules/mapping_copy.json"), ("pack", PACK, "fintech/fintech_v1.json"), ("recon_rules", RULES, "fintech/evidence_mapping/rules_copy.json")):
            with self.subTest(kind):
                root, manifest = self.tree()
                shutil.copyfile(root / rel, root / copy_to)
                self.edit(root, manifest, lambda d, k=kind, c=copy_to: entry(d, k).__setitem__("path", c))
                self.fails(root, manifest, copy_to, "lives at")

    def test_listing_the_same_path_twice_fails(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: d["files"].append(copy.deepcopy(entry(d, "recon_rules"))))
        self.fails(root, manifest, RULES, "listed twice")

    def test_a_missing_kind_fails(self):
        for kind in LISTED:
            with self.subTest(kind):
                root, manifest = self.tree()
                self.edit(root, manifest, lambda d, k=kind: d["files"].remove(entry(d, k)))
                self.fails(root, manifest, f"exactly one {kind} file, it lists 0")

    def test_two_entries_of_one_kind_fail(self):
        root, manifest = self.tree()
        # A second rule set with the right bytes and the right id, listed: only the one-per-kind rule can refuse it.
        shutil.copyfile(root / RULES, root / "fintech/recon_rules/rules.v2.json")
        def add(doc):
            doc["files"].append({"kind": "recon_rules", "path": "fintech/recon_rules/rules.v2.json", "sha256": sha256(root / RULES)})
        self.edit(root, manifest, add)
        self.fails(root, manifest, "exactly one recon_rules file, it lists 2")


class IdMatch(Base):
    def test_an_expected_id_that_is_not_the_files_id_fails(self):
        cases = (
            ("evidence_mapping_id", "fin.map.v2@1", "fin.map.v1@2"),
            ("evidence_mapping_id", "fin.map.v1@1", "fin.map.v1@2"),
            ("recon_rule_set_id", "fin.recon.rules@2", "fin.recon.rules@1"),
            ("recon_rule_set_id", "fin.recon.other@1", "fin.recon.rules@1"),
        )
        for key, named, carried in cases:
            with self.subTest(named):
                root, manifest = self.tree()
                self.edit(root, manifest, lambda d, k=key, n=named: d["expected"].__setitem__(k, n))
                problems = self.check(manifest, root)
                if key == "evidence_mapping_id":  # a wrong rule-set id also breaks the mapping's own reference to it; that second problem is fine
                    self.assertEqual(len(problems), 1, problems)
                self.fails(root, manifest, named, carried)

    def test_a_file_whose_id_changed_fails_even_when_the_hash_is_updated(self):
        # Only the id is wrong: the hash is re-pinned, so the failure is the id comparison and nothing else.
        for rel, key, kind in ((MAPPING, "mapping_id", "evidence_mapping"), (RULES, "ruleset_id", "recon_rules")):
            with self.subTest(key):
                root, manifest = self.tree()
                doc = json.loads((root / rel).read_text(encoding="utf-8"))
                doc[key] = doc[key] + ".x"
                (root / rel).write_text(json.dumps(doc, indent=2), encoding="utf-8")
                self.edit(root, manifest, lambda d, k=kind, r=rel: entry(d, k).__setitem__("sha256", sha256(root / r)))
                problems = self.check(manifest, root)
                self.assertTrue(problems)
                self.assertFalse([m for _, m in problems if "sha256" in m], problems)
                self.assertIn(doc[key], " | ".join(m for _, m in problems))

    def test_a_file_whose_version_changed_fails_even_when_the_hash_is_updated(self):
        for rel, kind in ((MAPPING, "evidence_mapping"), (RULES, "recon_rules")):
            with self.subTest(kind):
                root, manifest = self.tree()
                doc = json.loads((root / rel).read_text(encoding="utf-8"))
                doc["version"] = doc["version"] + 1
                (root / rel).write_text(json.dumps(doc, indent=2), encoding="utf-8")
                self.edit(root, manifest, lambda d, k=kind, r=rel: entry(d, k).__setitem__("sha256", sha256(root / r)))
                problems = self.check(manifest, root)
                self.assertEqual(len(problems), 1, problems)
                self.assertIn("@2", problems[0][1])

    def test_the_mappings_own_rule_set_reference_must_be_the_expected_rule_set(self):
        # fin.map.v1 says which rule set it feeds; that must be the rule set the manifest expects (one rule set in the pack).
        root, manifest = self.tree()
        doc = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        doc["ruleset"] = "fin.recon.rules@2"
        (root / MAPPING).write_text(json.dumps(doc, indent=2), encoding="utf-8")
        self.edit(root, manifest, lambda d: entry(d, "evidence_mapping").__setitem__("sha256", sha256(root / MAPPING)))
        problems = self.check(manifest, root)
        self.assertEqual(len(problems), 1, problems)
        self.assertIn("fin.recon.rules@2", problems[0][1])

    def test_a_file_without_an_id_or_with_a_bad_version_or_not_json_fails(self):
        for rel, kind, mutate in (
            (MAPPING, "evidence_mapping", lambda d: d.pop("mapping_id")),
            (MAPPING, "evidence_mapping", lambda d: d.__setitem__("version", "1")),
            (RULES, "recon_rules", lambda d: d.pop("ruleset_id")),
            (RULES, "recon_rules", lambda d: d.__setitem__("version", True)),
            (RULES, "recon_rules", lambda d: d.__setitem__("version", 1.0)),
        ):
            with self.subTest(rel + str(mutate.__code__.co_firstlineno)):
                root, manifest = self.tree()
                doc = json.loads((root / rel).read_text(encoding="utf-8"))
                mutate(doc)
                (root / rel).write_text(json.dumps(doc), encoding="utf-8")
                self.edit(root, manifest, lambda d, k=kind, r=rel: entry(d, k).__setitem__("sha256", sha256(root / r)))
                self.assertTrue(self.check(manifest, root))
        root, manifest = self.tree()
        (root / RULES).write_text("{not json", encoding="utf-8")
        self.edit(root, manifest, lambda d: entry(d, "recon_rules").__setitem__("sha256", sha256(root / RULES)))
        self.fails(root, manifest, RULES, "not valid JSON")


class PathEscape(Base):
    def listed_as(self, root: Path, manifest: Path, path: str):
        self.edit(root, manifest, lambda d: entry(d, "recon_rules").__setitem__("path", path))

    def test_dot_dot_fails_and_nothing_outside_is_read(self):
        root, manifest = self.tree()
        outside = root.parent / "outside.json"
        shutil.copyfile(PACKS / RULES, outside)  # a file with the right content and the right hash, outside the folder
        for path in ("../outside.json", "fintech/../../outside.json", "fintech/recon_rules/../../../outside.json", "..", "fintech/recon_rules/..", "fintech/../fintech/recon_rules/rules.v1.json"):
            with self.subTest(path):
                self.listed_as(root, manifest, path)
                self.fails(root, manifest, path, "escapes the pack directory")

    def test_an_absolute_path_fails(self):
        root, manifest = self.tree()
        for path in ("/etc/passwd", str(root / RULES), "/" + RULES, "C:/Windows/system.json", "C:\\x.json"):
            with self.subTest(path):
                self.listed_as(root, manifest, path)
                self.fails(root, manifest, path, "escapes the pack directory")

    def test_a_path_that_is_not_a_plain_relative_path_fails(self):
        root, manifest = self.tree()
        for path in ("./" + RULES, "fintech//recon_rules/rules.v1.json", "fintech\\recon_rules\\rules.v1.json", "", ".", "fintech/recon_rules/", RULES + "\n", "fintech/recon_rules/rules.v1.json\x00x", "~/" + RULES):
            with self.subTest(repr(path)):
                self.listed_as(root, manifest, path)
                self.assertTrue(self.check(manifest, root))

    def test_a_listed_symlink_fails_even_when_its_target_has_the_right_hash(self):
        root, manifest = self.tree()
        target = root / "fintech/recon_rules/real_rules.json"
        (root / RULES).rename(target)
        os.symlink(target.name, root / RULES)
        self.assertEqual(sha256(root / RULES), entry(json.loads(manifest.read_text()), "recon_rules")["sha256"])
        self.fails(root, manifest, RULES, "symlink")

    def test_a_symlink_to_a_file_outside_the_folder_fails(self):
        root, manifest = self.tree()
        outside = root.parent / "outside.json"
        shutil.copyfile(PACKS / RULES, outside)
        (root / RULES).unlink()
        os.symlink(outside, root / RULES)
        self.fails(root, manifest, RULES, "symlink")

    def test_a_symlinked_folder_in_the_path_fails(self):
        root, manifest = self.tree()
        real = root / "fintech/elsewhere"
        (root / "fintech/recon_rules").rename(real)
        os.symlink(real, root / "fintech/recon_rules")
        self.fails(root, manifest, RULES, "symlink")

    def test_a_symlinked_pack_folder_root_is_the_callers_business_but_a_link_inside_is_not(self):
        # The folder is given by the caller (the mounted NOEMA_PACK_DIR may itself be a link); what is under it must not be.
        root, manifest = self.tree()
        link = root.parent / "mounted"
        os.symlink(root, link)
        self.assertEqual(self.check(link / "fintech/manifest.v1.json", link), [])

    def test_fuzzed_paths_with_dot_dot_or_a_leading_slash_are_always_rejected(self):
        root, manifest = self.tree()
        outside = root.parent / "outside.json"
        shutil.copyfile(PACKS / RULES, outside)
        rng = random.Random(20261119)
        pieces = ["..", "fintech", "recon_rules", "rules.v1.json", "outside.json", "x", "."]
        seen = 0
        for _ in range(300):
            parts = [rng.choice(pieces) for _ in range(rng.randint(1, 6))]
            path = "/".join(parts)
            if rng.random() < 0.3:
                path = "/" + path
            if (".." not in parts and not path.startswith("/")) or path == RULES:
                continue
            self.listed_as(root, manifest, path)
            self.assertTrue(self.check(manifest, root), path)
            seen += 1
        self.assertGreater(seen, 150)


class ManifestShape(Base):
    def test_unknown_or_missing_fields_and_wrong_kinds_fail(self):
        cases = {
            "extra top-level field": lambda d: d.__setitem__("extra", 1),
            "extra entry field": lambda d: d["files"][0].__setitem__("mode", "0644"),
            "extra expected field": lambda d: d["expected"].__setitem__("other_id", "x@1"),
            "no expected": lambda d: d.pop("expected"),
            "no files": lambda d: d.__setitem__("files", []),
            "unknown kind": lambda d: d["files"][0].__setitem__("kind", "lexicon"),
            "no sha256": lambda d: d["files"][0].pop("sha256"),
            "wrong label": lambda d: d.__setitem__("label", "REAL"),
            "id without version": lambda d: d["expected"].__setitem__("evidence_mapping_id", "fin.map.v1"),
            "version zero": lambda d: d["expected"].__setitem__("recon_rule_set_id", "fin.recon.rules@0"),
            "version float": lambda d: d.__setitem__("version", 1.5),
        }
        for name, fn in cases.items():
            with self.subTest(name):
                root, manifest = self.tree()
                self.edit(root, manifest, fn)
                self.assertTrue(self.check(manifest, root), name)

    def test_a_manifest_that_is_not_json_or_has_a_duplicate_key_fails(self):
        root, manifest = self.tree()
        manifest.write_text("{not json", encoding="utf-8")
        self.fails(root, manifest, "not valid JSON")
        good = json.dumps(good_manifest(root))
        manifest.write_text(good[:-1] + ', "label": "SYNTHETIC"}', encoding="utf-8")
        self.assertTrue(self.check(manifest, root))

    def test_problems_come_in_a_stable_order(self):
        root, manifest = self.tree()
        for rel in LISTED.values():
            with open(root / rel, "ab") as fh:
                fh.write(b" ")
        first = self.check(manifest, root)
        self.assertGreaterEqual(len(first), 3)
        self.assertEqual(first, self.check(manifest, root))


class CommandLine(Base):
    def run_cli(self, *args: str):
        return subprocess.run([sys.executable, "-I", str(ROOT / "tools/packcheck"), *args], capture_output=True, text=True, timeout=60, cwd=ROOT)

    def test_the_real_manifest_passes_from_the_command_line(self):
        self.assertTrue(REAL_MANIFEST.is_file(), "packs/fintech/manifest.v1.json does not exist (A333)")
        r = self.run_cli("manifest", "packs/fintech/manifest.v1.json")
        self.assertEqual((r.returncode, r.stderr), (0, ""), r.stdout)
        self.assertIn("packcheck OK", r.stdout)

    def test_a_broken_tree_exits_one_and_prints_the_problem(self):
        root, manifest = self.tree()
        with open(root / MAPPING, "ab") as fh:
            fh.write(b" ")
        r = self.run_cli("manifest", str(manifest), str(root))
        self.assertEqual(r.returncode, 1, (r.stdout, r.stderr))
        self.assertIn("packcheck FAIL [manifest]", r.stdout)
        self.assertIn(MAPPING, r.stdout)

    def test_a_good_tree_exits_zero(self):
        root, manifest = self.tree()
        r = self.run_cli("manifest", str(manifest), str(root))
        self.assertEqual(r.returncode, 0, (r.stdout, r.stderr))

    def test_a_missing_manifest_is_a_usage_error(self):
        r = self.run_cli("manifest", "packs/fintech/no_such_manifest.json")
        self.assertEqual(r.returncode, 2, (r.stdout, r.stderr))

    def test_the_default_root_is_the_folder_above_the_manifests_folder(self):
        # packs/fintech/manifest.v1.json is relative to packs/: copy the tree, run with no root argument.
        root, manifest = self.tree()
        r = self.run_cli("manifest", str(manifest))
        self.assertEqual(r.returncode, 0, (r.stdout, r.stderr))
        with open(root / RULES, "ab") as fh:
            fh.write(b" ")
        self.assertEqual(self.run_cli("manifest", str(manifest)).returncode, 1)


if __name__ == "__main__":
    unittest.main()
