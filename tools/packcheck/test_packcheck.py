"""packcheck against the real packs and the crafted bad packs (packs/fixtures/bad_packs). REQ-CAP-001, REQ-AG-001, REQ-AG-002.

A bad pack is a patch on packs/fintech_v1.json (or on one of its graph files): small enough to review, and the real pack is
the base, so a fixture can never drift into failing for an unrelated reason. Each fixture names the check that must fail and
a fragment of the message, so a fixture that fails for the wrong reason fails the test.
"""
import copy
import json
import random
import string
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import checks  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent
PACKS = ROOT / "packs"
BAD = sorted((PACKS / "fixtures/bad_packs").glob("*.json"))
CONFORMANCE = sorted((PACKS / "conformance").glob("*.json"))


def apply(doc, op):
    parts = [p for p in op["path"].split("/") if p]
    parent = doc
    for p in parts[:-1]:
        parent = parent[int(p)] if isinstance(parent, list) else parent[p]
    last = parts[-1]
    if op["op"] == "add" and isinstance(parent, list):
        parent.append(op["value"]) if last == "-" else parent.insert(int(last), op["value"])
    elif op["op"] in ("add", "replace"):
        if isinstance(parent, list):
            parent[int(last)] = op["value"]
        else:
            parent[last] = op["value"]
    elif op["op"] == "remove":
        del parent[int(last) if isinstance(parent, list) else last]
    else:
        raise ValueError(op)


def mutant(fixture):
    pack = json.loads((ROOT / fixture["base"]).read_text())
    graphs = {rel: json.loads((PACKS / rel).read_text()) for rel in pack["action_graph_files"]}
    for op in fixture["patch"]:
        apply(graphs[op["file"]] if "file" in op else pack, op)
    return pack, graphs


class RealPacks(unittest.TestCase):
    def test_fintech_passes(self):
        self.assertEqual(checks.run(PACKS / "fintech_v1.json"), [])

    def test_conformance_packs_pass(self):
        for path in CONFORMANCE:
            with self.subTest(path.name):
                self.assertEqual(checks.run(path, conformance=True), [])


class BadPacks(unittest.TestCase):
    def test_there_are_enough_fixtures(self):
        self.assertGreaterEqual(len(BAD), 8)  # REQ-CAP-001: 8 crafted invalid descriptors
        covered = {json.loads(p.read_text())["expect_check"] for p in BAD}
        self.assertEqual(covered, {"schema", "references", "capability", "predicate", "graph", "routing", "reasons", "understanding"})

    def test_each_fails_for_its_stated_reason(self):
        for path in BAD:
            fixture = json.loads(path.read_text())
            with self.subTest(path.name):
                pack, graphs = mutant(fixture)
                missing = fixture["expect_check"] == "schema" and fixture["expect_message_contains"] == "does not exist"
                problems = checks.run(PACKS / "fintech_v1.json", pack=pack, graphs_override=None if missing else graphs)
                hits = [m for c, m in problems if c == fixture["expect_check"] and fixture["expect_message_contains"] in m]
                self.assertTrue(hits, f"{path.name}: expected [{fixture['expect_check']}] containing {fixture['expect_message_contains']!r}, got {problems}")

    def test_a_fixture_never_passes_the_unpatched_pack(self):
        self.assertEqual(checks.run(PACKS / "fintech_v1.json"), [])


class RoutingFuzz(unittest.TestCase):
    """REQ-AG-002: fuzzed routing parameters outside the allowlist are rejected 100% of the time (seeded, so reproducible)."""

    def test_every_outside_queue_is_rejected(self):
        base = json.loads((PACKS / "fintech_v1.json").read_text())
        graphs = {rel: json.loads((PACKS / rel).read_text()) for rel in base["action_graph_files"]}
        allow = set(base["routing_allowlist"])
        rng = random.Random(20261020)
        alphabet = string.ascii_letters + string.digits + "@:/._- \\"
        seen = 0
        for _ in range(500):
            queue = "".join(rng.choice(alphabet) for _ in range(rng.randint(0, 24)))
            if queue in allow:
                continue
            pack = copy.deepcopy(base)
            pack["policy"]["rules"][2]["params"]["owner_queue"] = queue
            problems = checks.run(PACKS / "fintech_v1.json", pack=pack, graphs_override=graphs)
            self.assertTrue(any(c == "routing" for c, _ in problems), repr(queue))
            seen += 1
        self.assertGreater(seen, 400)

    def test_every_allowlisted_queue_is_accepted(self):
        base = json.loads((PACKS / "fintech_v1.json").read_text())
        graphs = {rel: json.loads((PACKS / rel).read_text()) for rel in base["action_graph_files"]}
        for queue in base["routing_allowlist"]:
            pack = copy.deepcopy(base)
            pack["policy"]["rules"][2]["params"]["owner_queue"] = queue
            self.assertEqual(checks.run(PACKS / "fintech_v1.json", pack=pack, graphs_override=graphs), [])


class TemplateCoverage(unittest.TestCase):
    """REQ-EXP-002 (PCK-02, decision of 3 Nov 2026): a fallback template exists for every outcome x classification, in every locale of the
    pack. One file per locale, packs/<pack>/templates/<locale>.json, keyed <outcome_id>/<classification>. The classifications are the ones
    the pack's reconciliation rules can produce. The real pack is not enforced until EXP-04 delivers its templates; the mechanism is tested
    here on a pack built in a temporary folder, SYNTHETIC text."""

    def pack_dir(self, root: Path, drop=None, extra=None, empty=None, skip_locale=None, broken=None):
        """A copy of the real pack's manifest, recon rules and a complete set of synthetic templates, then one thing wrong."""
        pack = json.loads((PACKS / "fintech_v1.json").read_text())
        (root / "fintech/recon_rules").mkdir(parents=True)
        (root / "fintech/recon_rules/rules.v1.json").write_text((PACKS / "fintech/recon_rules/rules.v1.json").read_text())
        (root / "fintech/templates").mkdir()
        keys = checks.required_template_keys(pack, root)
        for loc in checks.template_locales(pack):
            if loc == skip_locale:
                continue
            doc = {k: f"SYNTHETIC {loc} {k}" for k in keys}
            if drop and loc == drop[0]:
                del doc[drop[1]]
            if extra and loc == extra[0]:
                doc[extra[1]] = "SYNTHETIC extra"
            if empty and loc == empty[0]:
                doc[empty[1]] = "  "
            text = json.dumps(doc)
            (root / f"fintech/templates/{loc}.json").write_text("{not json" if broken == loc else text)
        return pack

    def test_the_key_set_is_every_outcome_times_every_classification(self):
        pack = json.loads((PACKS / "fintech_v1.json").read_text())
        keys = checks.required_template_keys(pack, PACKS)
        outcomes = {o["outcome_id"] for o in pack["outcome_definitions"]}
        classes = {k.split("/", 1)[1] for k in keys}
        self.assertEqual({k.split("/", 1)[0] for k in keys}, outcomes)
        self.assertIn("EXECUTION_OUTCOME_UNKNOWN", classes)  # PERD 22.3
        self.assertIn("INSUFFICIENT_EVIDENCE", classes)  # the fallback rule's classification
        self.assertEqual(len(keys), len(outcomes) * len(classes))
        self.assertEqual(checks.template_locales(pack), ["en-NG", "pcm", "yo", "ha", "ig"])

    def test_a_complete_set_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            pack = self.pack_dir(Path(tmp))
            self.assertEqual(checks.check_templates(pack, Path(tmp)), [])

    def test_a_missing_key_fails_and_names_it(self):
        with tempfile.TemporaryDirectory() as tmp:
            keys = checks.required_template_keys(json.loads((PACKS / "fintech_v1.json").read_text()), PACKS)
            victim = sorted(keys)[3]
            pack = self.pack_dir(Path(tmp), drop=("yo", victim))
            problems = checks.check_templates(pack, Path(tmp))
            self.assertEqual(len(problems), 1, problems)
            self.assertEqual(problems[0][0], "templates")
            self.assertIn(victim, problems[0][1])
            self.assertIn("yo", problems[0][1])

    def test_a_missing_locale_file_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            pack = self.pack_dir(Path(tmp), skip_locale="ig")
            problems = checks.check_templates(pack, Path(tmp))
            self.assertTrue(any("ig.json" in m and "missing" in m for _, m in problems), problems)

    def test_an_empty_text_an_unknown_key_and_broken_json_fail(self):
        with tempfile.TemporaryDirectory() as tmp:
            pack = self.pack_dir(Path(tmp), extra=("ha", "fin.outcome.nope/SUCCESS_CONFIRMED"), empty=("pcm", "x"), broken="en-NG")
            keys = sorted(checks.required_template_keys(pack, Path(tmp)))
            (Path(tmp) / "fintech/templates/pcm.json").write_text(json.dumps({keys[0]: " ", **{k: "SYNTHETIC" for k in keys[1:]}}))
            joined = " | ".join(m for _, m in checks.check_templates(pack, Path(tmp)))
            self.assertIn("not valid JSON", joined)
            self.assertIn("fin.outcome.nope/SUCCESS_CONFIRMED", joined)
            self.assertIn("empty", joined)

    def test_the_real_pack_reports_its_coverage_without_failing_the_default_run(self):
        self.assertEqual(checks.run(PACKS / "fintech_v1.json"), [])
        enforced = checks.run(PACKS / "fintech_v1.json", templates="enforce")
        self.assertTrue(enforced and all(c == "templates" for c, _ in enforced), enforced[:2])


if __name__ == "__main__":
    unittest.main()
