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


class GraphPermits(unittest.TestCase):
    """REQ-TRU-005, PERD 23.3 T11: every capability a graph node names has a PERMIT rule in its pack's policy. Trust evaluates the policy for each capability a plan uses, so a node
    whose capability has none could never be planned (NO_PERMITTING_RULE). The expectation is computed here from the pack's own files, not from the check under test."""

    def test_every_graph_capability_of_every_real_pack_has_a_permit(self):
        for path in [PACKS / "fintech_v1.json", *CONFORMANCE]:
            pack = json.loads(path.read_text())
            permitted = {c for r in pack["policy"]["rules"] if r["effect"] == "PERMIT" for c in r["applies_to"]}
            for rel in pack.get("action_graph_files", []):
                graph = json.loads((PACKS / rel).read_text())
                for node in graph["nodes"]:
                    with self.subTest(f"{path.name} {graph['graph_id']}.{node['node_id']}"):
                        self.assertIn(node["capability_id"], permitted)

    def test_a_node_whose_capability_loses_its_permit_is_named(self):
        pack = json.loads((PACKS / "fintech_v1.json").read_text())
        graphs = {rel: json.loads((PACKS / rel).read_text()) for rel in pack["action_graph_files"]}
        for rule in pack["policy"]["rules"]:
            if rule["effect"] == "PERMIT" and "fin.cap.lookup_account" in rule["applies_to"]:
                rule["applies_to"] = [c for c in rule["applies_to"] if c != "fin.cap.lookup_account"]
        hits = [m for c, m in checks.check_graph_permits(pack, graphs) if c == "graph"]
        self.assertEqual(len(hits), 1, hits)
        self.assertIn("fin.graph.investigate.lookup_account", hits[0])
        self.assertIn("fin.cap.lookup_account", hits[0])


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


class GraphShape(unittest.TestCase):
    """REQ-AG-001 as amended by DEC-2026-10-31 D9: a graph, never a list of endpoints (PCK-04)."""

    def test_graph_files_are_action_graph_v2(self):
        # A graph that declares no roots is a v1 document; packcheck loads graphs as action-graph.v2.json only.
        import tempfile

        base = json.loads((PACKS / "fintech_v1.json").read_text())
        with tempfile.TemporaryDirectory() as tmp:
            for rel in base["action_graph_files"]:
                doc = json.loads((PACKS / rel).read_text())
                doc.pop("roots", None)
                (Path(tmp) / rel).parent.mkdir(parents=True, exist_ok=True)
                (Path(tmp) / rel).write_text(json.dumps(doc))
            graphs, problems = checks.load_graphs(base, Path(tmp))
        self.assertEqual(graphs, {})
        self.assertEqual(len(problems), len(base["action_graph_files"]))
        self.assertTrue(all(c == "schema" and "'roots' is a required property" in m for c, m in problems), problems)

    def test_connectedness_matches_an_independent_union_find(self):
        # Seeded, so reproducible: random edge sets over the investigation graph's nodes. packcheck reports "not connected"
        # exactly when the undirected graph has more than one component.
        base = json.loads((PACKS / "fintech_v1.json").read_text())
        rel = "fintech/graph/g1_investigate.json"
        real = {r: json.loads((PACKS / r).read_text()) for r in base["action_graph_files"]}
        ids = [n["node_id"] for n in real[rel]["nodes"]]
        rng = random.Random(20261101)
        disconnected = connected = 0
        for _ in range(300):
            edges = []
            for i, child in enumerate(ids):
                for parent in ids[:i]:  # parents before children: never a cycle
                    if rng.random() < 0.18:
                        edges.append({"node_id": child, "depends_on": parent, "condition": None})
            graphs = copy.deepcopy(real)
            graphs[rel]["edges"] = edges
            parent_of = {i: i for i in ids}

            def find(x):
                while parent_of[x] != x:
                    x = parent_of[x]
                return x

            for e in edges:
                parent_of[find(e["node_id"])] = find(e["depends_on"])
            expect = len({find(i) for i in ids}) > 1
            problems = checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs)
            got = any(c == "graph" and "fin.graph.investigate" in m and "not connected" in m for c, m in problems)
            self.assertEqual(got, expect, edges)
            disconnected += expect
            connected += not expect
        self.assertGreater(disconnected, 50)
        self.assertGreater(connected, 20)

    def test_a_single_node_graph_needs_no_edge(self):
        # The complaint graph is one write node: connectedness applies only to graphs with more than one node.
        problems = checks.run(PACKS / "fintech_v1.json")
        self.assertFalse([m for c, m in problems if "fin.graph.complaint" in m], problems)

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

    def test_a_template_for_a_part_of_the_answer_is_allowed_and_checked_like_the_summary(self):
        # A271: <outcome_id>/<classification>/<part> for a part of the closed list. A part is optional for now; when present it must be non-empty text, and a part name that is not
        # on the list, a part of a key the pack does not have, and a deeper path each fail. SYNTHETIC text.
        with tempfile.TemporaryDirectory() as tmp:
            keys = sorted(checks.required_template_keys(json.loads((PACKS / "fintech_v1.json").read_text()), PACKS))
            good = f"{keys[0]}/found"
            pack = self.pack_dir(Path(tmp), extra=("en-NG", good))
            self.assertEqual(checks.check_templates(pack, Path(tmp)), [])
            self.assertEqual(checks.TEMPLATE_PARTS, ("found", "not_known_what", "not_known_why", "advice_do", "advice_do_not", "next_what", "next_when"))
        for bad in (f"{keys[0]}/nope", f"{keys[0]}/summary", f"{keys[0]}/found/found", "fin.outcome.nope/SUCCESS_CONFIRMED/found", f"{keys[0]}/FOUND"):
            with tempfile.TemporaryDirectory() as tmp:
                pack = self.pack_dir(Path(tmp), extra=("en-NG", bad))
                problems = checks.check_templates(pack, Path(tmp))
                self.assertTrue(any(bad in m for _, m in problems), (bad, problems))
        with tempfile.TemporaryDirectory() as tmp:
            pack = self.pack_dir(Path(tmp), empty=("en-NG", good))
            problems = checks.check_templates(pack, Path(tmp))
            self.assertTrue(any(good in m and "empty" in m for _, m in problems), problems)

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


class MayWrite(unittest.TestCase):
    """A342 and A345 (interim, relayed, not yet committed). `may_write` (evidence-source) says whether a capability may write to the
    source; absent means false. `produced_by` (evidence-descriptor) names the evidence source that produced the evidence."""

    def pack(self):
        return json.loads((PACKS / "fintech_v1.json").read_text())

    def run_pack(self, pack):
        return checks.run(PACKS / "fintech_v1.json", pack=pack)

    def test_only_the_case_system_may_be_written_to(self):
        sources = {s["source_id"]: s.get("may_write", False) for s in self.pack()["evidence_sources"]}
        self.assertEqual({k for k, v in sources.items() if v}, {"fin.src.case_system"})

    def test_an_absent_may_write_is_false_so_a_write_capability_is_refused(self):
        pack = self.pack()
        for s in pack["evidence_sources"]:
            s.pop("may_write", None)
        problems = [m for c, m in self.run_pack(pack) if c == "capability" and "may_write" in m]
        self.assertTrue(problems, "the four case-writing capabilities must be refused")
        self.assertEqual(len(problems), 4, problems)

    def test_a_prohibited_capability_on_a_read_only_source_is_not_refused(self):
        pack = self.pack()
        self.assertTrue(any(c["prohibited"] for c in pack["capabilities"]))
        self.assertEqual([m for c, m in self.run_pack(pack) if "may_write" in m], [])

    def test_a_read_capability_on_a_read_only_source_is_not_refused(self):
        pack = self.pack()
        reads = [c for c in pack["capabilities"] if c["risk_class"] == "R0_read" and not c["prohibited"]]
        self.assertTrue(reads)
        self.assertEqual([m for c, m in self.run_pack(pack) if "may_write" in m], [])

    def test_produced_by_may_name_any_source_of_the_pack(self):
        pack = self.pack()
        for d in pack["evidence_descriptors"]:
            d["produced_by"] = d["source"]
        self.assertEqual(self.run_pack(pack), [])

    def test_the_real_descriptors_say_who_produced_them(self):  # A345
        for d in self.pack()["evidence_descriptors"]:
            self.assertEqual(d.get("produced_by"), d["source"], d["entity"])
