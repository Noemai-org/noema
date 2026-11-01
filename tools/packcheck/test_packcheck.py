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


if __name__ == "__main__":
    unittest.main()
