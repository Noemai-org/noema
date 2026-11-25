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
INJECTION = "fintech/context/injection_patterns.v1.json"  # A645, A664: the injection-screen patterns are pinned too (none or one file below manifest version 4, exactly one from it, always with its expected id)
LISTED = {"pack": PACK, "evidence_mapping": MAPPING, "recon_rules": RULES, "injection_patterns": INJECTION}
# A510: the action graphs the pack names (`action_graph_files`) are pinned too, one `action_graph` entry each.
GRAPHS = ["fintech/graph/g1_investigate.json", "fintech/graph/g2_escalate.json", "fintech/graph/g3_complaint.json"]
# A544 (interim): the limits files of the pack are pinned too, one `limits` entry each (an additive kind, as A510 added `action_graph`).
LIMITS = [
    "fintech/limits/execution_limits.json",
    "fintech/limits/masking_limits.json",
    "fintech/limits/session_limits.json",
    "fintech/limits/trust_limits.json",
]
INJECTION_ID = "fin.ctx.injection_patterns@2"  # the shipped file's own pack_id@version (A664 round 2: the patterns changed, so the version did)
EXPECTED = {"evidence_mapping_id": "fin.map.v1@2", "recon_rule_set_id": "fin.recon.rules@2", "injection_patterns_id": INJECTION_ID}


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def good_manifest(root: Path) -> dict:
    return {
        "manifest_id": "fin.manifest",
        "version": 1,
        "label": "SYNTHETIC",
        "note": "SYNTHETIC test manifest.",
        "expected": dict(EXPECTED),
        "files": [{"kind": kind, "path": rel, "sha256": sha256(root / rel)} for kind, rel in LISTED.items()]
        + [{"kind": "action_graph", "path": rel, "sha256": sha256(root / rel)} for rel in GRAPHS]
        + [{"kind": "limits", "path": rel, "sha256": sha256(root / rel)} for rel in LIMITS],
    }


def build_root(root: Path) -> Path:
    """A pack folder with the three real files, the three real action graphs and a good manifest. Returns the manifest path."""
    for rel in [*LISTED.values(), *GRAPHS, *LIMITS]:
        (root / rel).parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(PACKS / rel, root / rel)
    path = root / "fintech/manifest.v1.json"
    path.write_text(json.dumps(good_manifest(root), indent=2), encoding="utf-8")
    return path


def write(path: Path, doc) -> None:
    path.write_text(json.dumps(doc, indent=2), encoding="utf-8")


def entry(doc: dict, kind: str) -> dict:
    return next(f for f in doc["files"] if f["kind"] == kind)


def entry_of(doc: dict, path: str) -> dict:
    return next(f for f in doc["files"] if f["path"] == path)


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

    def test_it_is_labelled_synthetic_and_lists_exactly_the_five_kinds(self):
        doc = self.load()
        self.assertEqual(doc["label"], "SYNTHETIC")
        self.assertEqual({f["kind"]: f["path"] for f in doc["files"] if f["kind"] not in ("action_graph", "limits", "priority_routing")}, LISTED)
        self.assertEqual(sorted(f["path"] for f in doc["files"] if f["kind"] == "action_graph"), GRAPHS)  # A510
        self.assertEqual(sorted(f["path"] for f in doc["files"] if f["kind"] == "limits"), LIMITS)  # A544
        self.assertEqual([f["path"] for f in doc["files"] if f["kind"] == "priority_routing"], ["fintech/routing/priority_routing.v1.json"])  # PCK-09, A644
        self.assertEqual(len(doc["files"]), 12)  # the three single kinds, the three action graphs, the four limits files and (A645) the injection patterns

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
        found = {str(p.relative_to(PACKS)) for d in ("fintech/evidence_mapping", "fintech/recon_rules", "fintech/graph", "fintech/limits", "fintech/context", "fintech/routing") for p in (PACKS / d).rglob("*.json")}
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
                # A664: the patterns kind is optional below manifest version 4, so its file missing while its id is named is a pairing problem (the gate cases are in InjectionGate).
                self.fails(root, manifest, "names expected.injection_patterns_id but lists no injection_patterns file" if kind == "injection_patterns" else f"exactly one {kind} file, it lists 0")

    def test_two_entries_of_one_kind_fail(self):
        root, manifest = self.tree()
        # A second rule set with the right bytes and the right id, listed: only the one-per-kind rule can refuse it.
        shutil.copyfile(root / RULES, root / "fintech/recon_rules/rules.v2.json")
        def add(doc):
            doc["files"].append({"kind": "recon_rules", "path": "fintech/recon_rules/rules.v2.json", "sha256": sha256(root / RULES)})
        self.edit(root, manifest, add)
        self.fails(root, manifest, "exactly one recon_rules file, it lists 2")


class ReconRulesFile(Base):
    """A659 (interim, relayed, not yet committed): the listed rule set is checked against `recon-rule-set.v1` as the manifest is, so a rule set of version 2 with no window cannot be pinned."""

    def reseal(self, root: Path, manifest: Path, expected: str) -> None:
        mapping = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        mapping["ruleset"] = expected  # the mapping feeds the rule set the manifest expects (PCK-02)
        write(root / MAPPING, mapping)

        def fix(doc):
            entry(doc, "recon_rules")["sha256"] = sha256(root / RULES)
            entry(doc, "evidence_mapping")["sha256"] = sha256(root / MAPPING)
            doc["expected"]["recon_rule_set_id"] = expected
        self.edit(root, manifest, fix)

    def test_a_version_2_rule_set_with_no_window_is_refused_even_when_pinned_and_named(self):
        root, manifest = self.tree()
        doc = json.loads((root / RULES).read_text(encoding="utf-8"))
        doc["version"] = 2
        write(root / RULES, doc)
        self.reseal(root, manifest, "fin.recon.rules@2")
        self.fails(root, manifest, "convergence_window_seconds")

    def test_a_version_2_rule_set_with_every_window_is_accepted(self):
        root, manifest = self.tree()
        doc = json.loads((root / RULES).read_text(encoding="utf-8"))
        doc["version"] = 2
        for e in doc["incompatible"] + doc["single_valued"]:
            e["convergence_window_seconds"] = 60
        write(root / RULES, doc)
        self.reseal(root, manifest, "fin.recon.rules@2")
        self.assertEqual(self.check(manifest, root), [])

    def test_a_rule_set_that_is_not_a_rule_set_is_refused_even_when_pinned_and_named(self):
        root, manifest = self.tree()
        doc = json.loads((root / RULES).read_text(encoding="utf-8"))
        doc["surprise"] = 1
        write(root / RULES, doc)
        self.reseal(root, manifest, "fin.recon.rules@1")
        self.fails(root, manifest, RULES)


class ActionGraphs(Base):
    """A510: every action graph the pack names is pinned as an `action_graph` entry and verified, and no other graph is: nothing unverified is ever served."""

    def reseal(self, root: Path, manifest: Path) -> None:
        """Write the digest of every listed file as it is on disk now, so that only the structure is left to differ."""
        def fix(doc):
            for f in doc["files"]:
                if (root / f["path"]).is_file() and not (root / f["path"]).is_symlink():
                    f["sha256"] = sha256(root / f["path"])
        self.edit(root, manifest, fix)

    def set_named(self, root: Path, manifest: Path, graphs: list[str] | None) -> None:
        """Rewrite the pack file's `action_graph_files` (None removes the member) and reseal."""
        pack = root / PACK
        doc = json.loads(pack.read_text(encoding="utf-8"))
        if graphs is None:
            doc.pop("action_graph_files", None)
        else:
            doc["action_graph_files"] = graphs
        write(pack, doc)
        self.reseal(root, manifest)

    def test_the_real_manifest_pins_exactly_the_graphs_the_pack_names(self):
        pack = json.loads((PACKS / PACK).read_text(encoding="utf-8"))
        doc = json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))
        self.assertEqual(sorted(pack["action_graph_files"]), GRAPHS)
        pinned = {f["path"]: f["sha256"] for f in doc["files"] if f["kind"] == "action_graph"}
        self.assertEqual(sorted(pinned), GRAPHS)
        for rel, digest in pinned.items():
            with self.subTest(rel):
                self.assertEqual(digest, sha256(PACKS / rel))  # an independent computation over the raw bytes

    def test_a_good_tree_with_the_graphs_passes(self):
        root, manifest = self.tree()
        self.assertEqual(self.check(manifest, root), [])

    def test_a_changed_byte_in_a_pinned_graph_fails_for_each_graph(self):
        for rel in GRAPHS:
            with self.subTest(rel):
                root, manifest = self.tree()
                path = root / rel
                path.write_bytes(path.read_bytes() + b" ")
                self.fails(root, manifest, "sha256")

    def test_a_graph_the_pack_names_but_the_manifest_does_not_pin_fails(self):
        for rel in GRAPHS:
            with self.subTest(rel):
                root, manifest = self.tree()
                self.edit(root, manifest, lambda d: d.__setitem__("files", [f for f in d["files"] if f["path"] != rel]))
                self.fails(root, manifest, rel)

    def test_a_pinned_graph_the_pack_does_not_name_fails(self):
        root, manifest = self.tree()
        self.set_named(root, manifest, GRAPHS[:2])
        self.fails(root, manifest, GRAPHS[2])

    def test_a_pinned_graph_entry_whose_file_is_missing_fails(self):
        root, manifest = self.tree()
        (root / GRAPHS[1]).unlink()
        self.fails(root, manifest, GRAPHS[1], "does not exist")

    def test_a_graph_listed_twice_fails(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: d["files"].append(dict(entry_of(d, GRAPHS[0]))))
        self.fails(root, manifest, GRAPHS[0], "twice")

    def test_an_unlisted_graph_file_in_the_graph_folder_fails(self):
        root, manifest = self.tree()
        shutil.copyfile(root / GRAPHS[0], root / "fintech/graph/g9_extra.json")
        self.fails(root, manifest, "fintech/graph/g9_extra.json", "not listed")

    def test_a_graph_listed_under_another_kind_fails(self):
        for kind in ("pack", "evidence_mapping", "recon_rules", "graph", "action_graphs"):
            with self.subTest(kind):
                root, manifest = self.tree()
                self.edit(root, manifest, lambda d: entry_of(d, GRAPHS[0]).__setitem__("kind", kind))
                self.assertTrue(self.check(manifest, root), "a graph listed as another kind must fail")

    def test_a_graph_path_that_escapes_or_is_a_symlink_fails(self):
        root, manifest = self.tree()
        outside = root.parent / "outside.json"
        outside.write_text("{}", encoding="utf-8")
        (root / GRAPHS[0]).unlink()
        (root / GRAPHS[0]).symlink_to(outside)
        self.fails(root, manifest, GRAPHS[0], "symlink")
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: entry_of(d, GRAPHS[0]).__setitem__("path", "../outside.json"))
        self.fails(root, manifest, "..")

    def test_a_pack_that_names_no_graph_needs_no_graph_entry_and_may_pin_none(self):
        for named in (None, []):
            with self.subTest(named=named):
                root, manifest = self.tree()
                self.edit(root, manifest, lambda d: d.__setitem__("files", [f for f in d["files"] if f["kind"] != "action_graph"]))
                self.set_named(root, manifest, named)
                for rel in GRAPHS:
                    (root / rel).unlink()
                self.assertEqual(self.check(manifest, root), [])

    def test_a_graph_pinned_for_a_pack_that_names_none_fails(self):
        root, manifest = self.tree()
        self.set_named(root, manifest, None)
        self.fails(root, manifest, GRAPHS[0])


class Limits(Base):
    """A544 (interim): the pack's limits files are pinned as `limits` entries, every file of the folder, and verified like every other pack file. AGENT-16: a limit is data that a
    Founder reviews, so a byte changed after the manifest was made refuses the pack; a limits file nobody pinned is a file nobody reviewed."""

    def test_the_real_manifest_pins_every_limits_file_of_the_folder(self):
        doc = json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))
        pinned = {f["path"]: f["sha256"] for f in doc["files"] if f["kind"] == "limits"}
        self.assertEqual(sorted(pinned), LIMITS)
        on_disk = sorted(str(p.relative_to(PACKS)) for p in (PACKS / "fintech/limits").glob("*.json"))
        self.assertEqual(sorted(pinned), on_disk)
        for rel, digest in pinned.items():
            self.assertEqual(digest, sha256(PACKS / rel), rel)

    def test_a_good_tree_with_the_limits_passes(self):
        root, manifest = self.tree()
        self.assertEqual(self.check(manifest, root), [])

    def test_a_changed_byte_in_a_pinned_limits_file_fails_for_each_file(self):
        for rel in LIMITS:
            with self.subTest(rel):
                root, manifest = self.tree()
                with open(root / rel, "ab") as f:
                    f.write(b" ")
                self.fails(root, manifest, rel, "sha256")

    def test_a_limits_file_nobody_pinned_fails(self):
        root, manifest = self.tree()
        shutil.copyfile(root / LIMITS[0], root / "fintech/limits/extra_limits.json")
        self.fails(root, manifest, "extra_limits.json", "not listed")

    def test_a_pinned_limits_entry_whose_file_is_missing_fails(self):
        root, manifest = self.tree()
        (root / LIMITS[1]).unlink()
        self.fails(root, manifest, LIMITS[1], "does not exist")

    def test_a_limits_entry_listed_twice_fails(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: d["files"].append(dict(entry_of(d, LIMITS[2]))))
        self.fails(root, manifest, LIMITS[2], "twice")

    def test_a_limits_path_that_escapes_or_is_a_symlink_fails(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: entry_of(d, LIMITS[0]).__setitem__("path", "../outside.json"))
        self.fails(root, manifest, "outside.json")
        root, manifest = self.tree()
        outside = root.parent / "outside_limits.json"
        shutil.copyfile(root / LIMITS[3], outside)
        (root / LIMITS[3]).unlink()
        (root / LIMITS[3]).symlink_to(outside)
        self.fails(root, manifest, LIMITS[3], "symlink")

    def test_a_pack_with_no_limits_needs_no_limits_entry(self):
        """The kind is additive: a manifest without one is still a manifest (the earlier packs and the test packs have none)."""
        root, manifest = self.tree()
        for rel in LIMITS:
            (root / rel).unlink()
        self.edit(root, manifest, lambda d: d.__setitem__("files", [f for f in d["files"] if f["kind"] != "limits"]))
        self.assertEqual(self.check(manifest, root), [])



class InjectionPatterns(Base):
    """A645: the injection-screen patterns are pack data, hash-pinned like every other pack file (kind `injection_patterns`, with its expected id; exactly one entry from manifest version 4, none or one below it: A664), so the detector runs only on verified bytes."""

    def test_the_real_manifest_pins_the_pattern_file_once(self):
        doc = json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))
        self.assertEqual([f["path"] for f in doc["files"] if f["kind"] == "injection_patterns"], [INJECTION])
        self.assertEqual(doc["expected"]["injection_patterns_id"], INJECTION_ID)

    def test_a_second_pattern_file_beside_the_pinned_one_fails(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            manifest = build_root(root)
            (root / "fintech/context/injection_patterns.v2.json").write_text("{}", encoding="utf-8")
            self.fails(root, manifest, "injection_patterns.v2.json")

    def test_two_pattern_entries_fail(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            manifest = build_root(root)
            doc = json.loads(manifest.read_text(encoding="utf-8"))
            (root / "fintech/context/other.json").write_text("{}", encoding="utf-8")
            doc["files"].append({"kind": "injection_patterns", "path": "fintech/context/other.json", "sha256": sha256(root / "fintech/context/other.json")})
            write(manifest, doc)
            self.fails(root, manifest, "injection_patterns")

    def test_the_pattern_file_listed_under_another_kind_fails(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            manifest = build_root(root)
            doc = json.loads(manifest.read_text(encoding="utf-8"))
            entry(doc, "injection_patterns")["kind"] = "recon_rules"
            write(manifest, doc)
            # A664: with its entry relabelled the manifest names the patterns id and lists no patterns file; that is what is reported (the schema also refuses it).
            self.fails(root, manifest, "names expected.injection_patterns_id but lists no injection_patterns file")

    def test_a_manifest_without_the_expected_pattern_id_fails(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            manifest = build_root(root)
            doc = json.loads(manifest.read_text(encoding="utf-8"))
            del doc["expected"]["injection_patterns_id"]
            write(manifest, doc)
            self.fails(root, manifest, "injection_patterns_id")

    def test_a_pinned_file_that_is_not_the_patterns_document_fails_even_when_the_hash_is_updated(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            manifest = build_root(root)
            f = root / INJECTION
            f.write_text('{"pack_id":"fin.ctx.other","version":1,"patterns":[]}', encoding="utf-8")
            doc = json.loads(manifest.read_text(encoding="utf-8"))
            entry(doc, "injection_patterns")["sha256"] = sha256(f)
            write(manifest, doc)
            self.fails(root, manifest, "injection_patterns")


class ShippedPatterns(Base):
    """A664 round 2 (SR condition 3 and finding 1): the manifest that ships is at or above the gate and pins the patterns, so the gate cannot turn the screen off in production, and the identity it
    expects is the pattern file's own `pack_id@version`. The Record stores that identity with each detection (not the file's hash), so the patterns changing means the version changes."""

    def test_the_shipped_manifest_is_at_or_above_the_gate_and_pins_the_patterns_file_with_its_digest(self):
        doc = json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))
        self.assertGreaterEqual(doc["version"], checks.MANIFEST_INJECTION_FROM_VERSION)
        pinned = [f for f in doc["files"] if f["kind"] == "injection_patterns"]
        self.assertEqual([f["path"] for f in pinned], [INJECTION])
        self.assertEqual(pinned[0]["sha256"], sha256(PACKS / INJECTION))
        self.assertEqual(self.check(REAL_MANIFEST, PACKS), [])

    def test_the_manifests_expected_patterns_id_is_the_files_own_pack_id_and_version(self):
        doc = json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))
        body = json.loads((PACKS / INJECTION).read_text(encoding="utf-8"))
        self.assertEqual(doc["expected"]["injection_patterns_id"], f"{body['pack_id']}@{body['version']}")

    def test_the_patterns_changed_after_version_1_so_the_shipped_version_is_at_least_2(self):
        body = json.loads((PACKS / INJECTION).read_text(encoding="utf-8"))
        self.assertGreaterEqual(body["version"], 2, "versioned data is not edited under the same version (A664 round 2)")
        self.assertEqual(json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))["expected"]["injection_patterns_id"], "fin.ctx.injection_patterns@2")


class InjectionGate(Base):
    """A664 (interim; the A579 pattern): the injection patterns are required from manifest version 4 and optional below it. A manifest below the gate that lists none still passes; one from the gate
    that lists none fails; the id and the file go together at every version; the rule is none or one below the gate and exactly one from it."""

    GATE = 4

    def old_tree(self, version: int, *, keep_file: bool = False):
        root, manifest = self.tree()

        def drop(d):
            d["version"] = version
            d["files"] = [f for f in d["files"] if f["kind"] != "injection_patterns"]
            del d["expected"]["injection_patterns_id"]

        self.edit(root, manifest, drop)
        if not keep_file:
            (root / INJECTION).unlink()
        return root, manifest

    def test_a_manifest_below_the_gate_that_lists_no_patterns_passes(self):
        for v in range(1, self.GATE):
            with self.subTest(v):
                root, manifest = self.old_tree(v)
                self.assertEqual(self.check(manifest, root), [])

    def test_a_manifest_below_the_gate_passes_even_with_the_file_on_disk_unlisted(self):
        # The loader serves only what the manifest pins and does not scan for an unlisted patterns file of an older manifest; packcheck agrees (it is not a pinned kind there).
        root, manifest = self.old_tree(self.GATE - 1, keep_file=True)
        self.assertEqual(self.check(manifest, root), [])

    def test_a_manifest_from_the_gate_that_lists_no_patterns_fails_naming_the_version_and_the_rule(self):
        for v in (self.GATE, self.GATE + 1, 99):
            with self.subTest(v):
                root, manifest = self.old_tree(v)
                self.fails(root, manifest, f"version {v}", "exactly one injection_patterns file", "it lists 0")

    def test_a_gated_manifest_that_deleted_the_entry_and_left_the_file_on_disk_fails(self):
        # SR condition 1: the entry (and its id) removed, the file still in the folder; refused at the gate, not read as "no patterns".
        for v in (self.GATE, self.GATE + 1):
            with self.subTest(v):
                root, manifest = self.old_tree(v, keep_file=True)
                self.assertTrue((root / INJECTION).exists())
                self.fails(root, manifest, f"version {v}", "exactly one injection_patterns file", "it lists 0")

    def test_a_manifest_from_the_gate_that_lists_the_file_but_no_expected_id_fails(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: (d.__setitem__("version", self.GATE), d["expected"].pop("injection_patterns_id")))
        self.fails(root, manifest, "injection_patterns_id")

    def test_a_manifest_from_the_gate_with_the_file_and_its_id_passes(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: d.__setitem__("version", self.GATE))
        self.assertEqual(self.check(manifest, root), [])

    def test_the_id_and_the_file_go_together_at_every_version(self):
        for v in (1, self.GATE - 1, self.GATE, self.GATE + 1):
            with self.subTest(v, case="file without id"):
                root, manifest = self.tree()
                self.edit(root, manifest, lambda d, v=v: (d.__setitem__("version", v), d["expected"].pop("injection_patterns_id")))
                self.fails(root, manifest, "injection_patterns_id")
            with self.subTest(v, case="id without file"):
                root, manifest = self.old_tree(v)
                self.edit(root, manifest, lambda d: d["expected"].__setitem__("injection_patterns_id", INJECTION_ID))
                self.fails(root, manifest, "injection_patterns_id")

    def test_two_pattern_files_fail_at_every_version_naming_the_rule(self):
        for v in (1, self.GATE - 1, self.GATE):
            with self.subTest(v):
                root, manifest = self.tree()
                (root / "fintech/context/other.json").write_text("{}", encoding="utf-8")
                self.edit(root, manifest, lambda d, v=v: (d.__setitem__("version", v), d["files"].append({"kind": "injection_patterns", "path": "fintech/context/other.json", "sha256": sha256(root / "fintech/context/other.json")})))
                self.fails(root, manifest, "injection_patterns file", "it lists 2")
                self.assertNotIn("at most one", self.messages(self.check(manifest, root)))


WINDOW_PAIRS = ("switch_failed_credit_credited", "credit_without_debit", "switch_successful_credit_not_credited")
WINDOW = "convergence_window_seconds"


class ConvergenceWindows(Base):
    """PCK-05 (A674, A659; REQ-RES-001, REQ-AG-001): rule set version 2 gives every matrix entry one `convergence_window_seconds`. packcheck reads the entity-to-source link from the evidence mapping
    (no member in the rule file) and requires the window to equal the smaller freshness_sla_seconds of the pair's two sources in the pack file, and the rule file to be labelled SYNTHETIC. SYNTHETIC."""

    def rules(self, root: Path) -> dict:
        return json.loads((root / RULES).read_text(encoding="utf-8"))

    def repin(self, root: Path, manifest: Path) -> None:
        def fix(d):
            for kind, rel in LISTED.items():
                entry(d, kind)["sha256"] = sha256(root / rel)
        self.edit(root, manifest, fix)

    def v2_tree(self):
        """The real files, normalised to the shape A674 asks for, so that a mutant below differs from a good tree in one thing."""
        root, manifest = self.tree()
        rules = self.rules(root)
        rules["version"] = 2
        rules["incompatible"] = [dict(m, **{WINDOW: 300}) for m in rules["incompatible"] if m["label"] in WINDOW_PAIRS]
        write(root / RULES, rules)
        mapping = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        mapping["ruleset"] = "fin.recon.rules@2"
        write(root / MAPPING, mapping)
        self.edit(root, manifest, lambda d: d["expected"].__setitem__("recon_rule_set_id", "fin.recon.rules@2"))
        self.repin(root, manifest)
        return root, manifest

    def mutate_rules(self, root, manifest, fn):
        rules = self.rules(root)
        fn(rules)
        write(root / RULES, rules)
        self.repin(root, manifest)

    def test_the_shipped_rule_set_is_version_2_with_the_three_pairs_at_300_seconds(self):
        rules = self.rules(PACKS)
        self.assertEqual(rules["version"], 2)
        self.assertEqual(rules["label"], "SYNTHETIC")
        self.assertEqual([m["label"] for m in rules["incompatible"]], list(WINDOW_PAIRS))
        self.assertEqual([m[WINDOW] for m in rules["incompatible"]], [300, 300, 300])
        self.assertNotIn("reversal_conflict", json.dumps(rules["incompatible"]))  # A674: out of the active list until Tushe answers
        mapping = json.loads((PACKS / MAPPING).read_text(encoding="utf-8"))
        self.assertEqual(mapping["ruleset"], "fin.recon.rules@2")

    def test_an_entry_has_no_member_for_the_source(self):
        # A674 (1): the entity-to-source link is read from the mapping; the rule file gets no new member for it.
        for m in self.rules(PACKS)["incompatible"]:
            self.assertLessEqual(set(m), {"label", "a", "b", "unless", WINDOW}, m["label"])

    def test_the_baseline_tree_passes(self):
        root, manifest = self.v2_tree()
        self.assertEqual(self.check(manifest, root), [])

    def test_a_window_that_is_not_the_smaller_freshness_of_the_pairs_sources_fails(self):
        for bad in (299, 301, 60, 600):
            with self.subTest(bad):
                root, manifest = self.v2_tree()
                self.mutate_rules(root, manifest, lambda r, b=bad: r["incompatible"][0].__setitem__(WINDOW, b))
                self.fails(root, manifest, "switch_failed_credit_credited", WINDOW)

    def test_a_missing_or_malformed_window_fails_and_names_the_pair(self):
        cases = {"missing": lambda m: m.pop(WINDOW), "zero": lambda m: m.__setitem__(WINDOW, 0), "negative": lambda m: m.__setitem__(WINDOW, -300),
                 "a string": lambda m: m.__setitem__(WINDOW, "300"), "a float": lambda m: m.__setitem__(WINDOW, 300.0), "a boolean": lambda m: m.__setitem__(WINDOW, True),
                 "null": lambda m: m.__setitem__(WINDOW, None)}
        for name, fn in cases.items():
            for idx, label in enumerate(WINDOW_PAIRS):
                with self.subTest(f"{name} {label}"):
                    root, manifest = self.v2_tree()
                    self.mutate_rules(root, manifest, lambda r, f=fn, i=idx: f(r["incompatible"][i]))
                    self.fails(root, manifest, label, WINDOW)

    def test_the_window_is_the_smaller_of_the_two_sources_freshness(self):
        # switch 120 s, destination 300 s, ledger 300 s: the pairs holding the switch need 120, the other stays 300.
        root, manifest = self.v2_tree()
        pack = json.loads((root / PACK).read_text(encoding="utf-8"))
        next(s for s in pack["evidence_sources"] if s["source_id"] == "fin.src.switch")["freshness_sla_seconds"] = 120
        write(root / PACK, pack)
        self.repin(root, manifest)
        self.fails(root, manifest, "switch_failed_credit_credited", "switch_successful_credit_not_credited", WINDOW)
        self.assertNotIn("credit_without_debit", self.messages(self.check(manifest, root)))
        self.mutate_rules(root, manifest, lambda r: [m.__setitem__(WINDOW, 120) for m in r["incompatible"] if m["label"] != "credit_without_debit"])
        self.assertEqual(self.check(manifest, root), [])

    def test_the_link_is_read_from_the_mapping(self):
        # Re-pointing the credit row at the case system (60 s) makes every pair holding the credit need 60, with nothing changed in the rule file.
        root, manifest = self.v2_tree()
        mapping = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        next(r for r in mapping["rows"] if r["entity"] == "credit")["source"] = "fin.src.case_system"
        write(root / MAPPING, mapping)
        self.repin(root, manifest)
        self.fails(root, manifest, "switch_failed_credit_credited", "credit_without_debit", "switch_successful_credit_not_credited")

    def test_an_entity_the_mapping_does_not_link_to_a_source_fails(self):
        root, manifest = self.v2_tree()
        mapping = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        mapping["rows"] = [r for r in mapping["rows"] if r["entity"] != "switch"]
        write(root / MAPPING, mapping)
        self.repin(root, manifest)
        self.fails(root, manifest, "switch", "switch_failed_credit_credited")

    def test_an_entity_the_mapping_links_to_two_sources_fails(self):
        root, manifest = self.v2_tree()
        mapping = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        mapping["rows"].append({"entity": "switch", "source": "fin.src.settlement", "fields": {}})
        write(root / MAPPING, mapping)
        self.repin(root, manifest)
        self.fails(root, manifest, "switch", "switch_failed_credit_credited")

    def test_a_source_the_pack_does_not_declare_fails(self):
        root, manifest = self.v2_tree()
        pack = json.loads((root / PACK).read_text(encoding="utf-8"))
        pack["evidence_sources"] = [s for s in pack["evidence_sources"] if s["source_id"] != "fin.src.destination"]
        write(root / PACK, pack)
        self.repin(root, manifest)
        self.fails(root, manifest, "fin.src.destination")

    def test_a_rule_file_not_labelled_synthetic_fails(self):
        for name, fn in {"missing": lambda r: r.pop("label"), "another": lambda r: r.__setitem__("label", "PRODUCTION")}.items():
            with self.subTest(name):
                root, manifest = self.v2_tree()
                self.mutate_rules(root, manifest, fn)
                self.fails(root, manifest, "SYNTHETIC")

    def test_a_version_1_rule_set_is_not_asked_for_windows(self):
        root, manifest = self.tree()
        rules = self.rules(root)
        rules["version"] = 1
        rules["incompatible"] = [{k: v for k, v in m.items() if k != WINDOW} for m in rules["incompatible"]]
        write(root / RULES, rules)
        mapping = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        mapping["ruleset"] = "fin.recon.rules@1"
        write(root / MAPPING, mapping)
        self.edit(root, manifest, lambda d: d["expected"].__setitem__("recon_rule_set_id", "fin.recon.rules@1"))
        self.repin(root, manifest)
        self.assertEqual(self.check(manifest, root), [])


class IdMatch(Base):
    def test_an_expected_id_that_is_not_the_files_id_fails(self):
        cases = (
            ("evidence_mapping_id", "fin.map.v2@1", "fin.map.v1@2"),
            ("evidence_mapping_id", "fin.map.v1@1", "fin.map.v1@2"),
            ("recon_rule_set_id", "fin.recon.rules@3", "fin.recon.rules@2"),
            ("recon_rule_set_id", "fin.recon.other@1", "fin.recon.rules@2"),
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
                if kind == "recon_rules":  # A659: a version 2 rule set declares its windows, so the id comparison stays the only failure
                    for e in doc["incompatible"] + doc["single_valued"]:
                        e["convergence_window_seconds"] = 60
                (root / rel).write_text(json.dumps(doc, indent=2), encoding="utf-8")
                self.edit(root, manifest, lambda d, k=kind, r=rel: entry(d, k).__setitem__("sha256", sha256(root / r)))
                problems = self.check(manifest, root)
                self.assertEqual(len(problems), 1, problems)
                self.assertIn("@2", problems[0][1])

    def test_the_mappings_own_rule_set_reference_must_be_the_expected_rule_set(self):
        # fin.map.v1 says which rule set it feeds; that must be the rule set the manifest expects (one rule set in the pack).
        root, manifest = self.tree()
        doc = json.loads((root / MAPPING).read_text(encoding="utf-8"))
        doc["ruleset"] = "fin.recon.rules@3"
        (root / MAPPING).write_text(json.dumps(doc, indent=2), encoding="utf-8")
        self.edit(root, manifest, lambda d: entry(d, "evidence_mapping").__setitem__("sha256", sha256(root / MAPPING)))
        problems = self.check(manifest, root)
        self.assertEqual(len(problems), 1, problems)
        self.assertIn("fin.recon.rules@3", problems[0][1])

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



class LimitsRequiredFromVersionThree(Base):
    """A579 (interim; SOT-C-33): a pack of manifest version 3 or later must list its limits files; there is no opt-in. An older manifest (1, 2) may still list none."""

    def without_limits(self, version: int):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: (d.__setitem__("version", version), d.__setitem__("files", [f for f in d["files"] if f["kind"] != "limits"])))
        return root, manifest

    def test_the_real_manifest_is_version_three_or_later_and_lists_limits(self):
        doc = json.loads(REAL_MANIFEST.read_text(encoding="utf-8"))
        self.assertGreaterEqual(doc["version"], 3)
        self.assertTrue([f for f in doc["files"] if f["kind"] == "limits"])

    def test_version_three_or_later_without_a_limits_entry_fails(self):
        for version in (3, 4, 10):
            with self.subTest(version):
                root, manifest = self.without_limits(version)
                self.fails(root, manifest, "limits", str(version))

    def test_an_older_manifest_without_limits_entries_still_passes(self):
        for version in (1, 2):
            with self.subTest(version):
                root, manifest = self.without_limits(version)
                shutil.rmtree(root / "fintech/limits")  # an older pack has no limits folder (a limits file nobody pinned is refused, test above)
                self.assertEqual(self.check(manifest, root), [])

    def test_version_three_with_the_limits_passes(self):
        root, manifest = self.tree()
        self.edit(root, manifest, lambda d: d.__setitem__("version", 3))
        self.assertEqual(self.check(manifest, root), [])

if __name__ == "__main__":
    unittest.main()
