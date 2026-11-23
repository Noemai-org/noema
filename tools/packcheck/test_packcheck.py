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
        self.assertEqual(covered, {"schema", "references", "capability", "predicate", "graph", "routing", "reasons", "understanding", "roles"})

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


class ApproverRoles(unittest.TestCase):
    """A377 (interim, relayed, not yet committed): packcheck refuses ROLE:<role> naming a role outside the A1 set, in a policy rule's
    approver_requirement and in a trust profile's approvals. SELF, MAKER_CHECKER and the four A1 roles pass."""

    def run_with(self, requirement=None, approval=None):
        pack = json.loads((PACKS / "fintech_v1.json").read_text())
        graphs = {rel: json.loads((PACKS / rel).read_text()) for rel in pack["action_graph_files"]}
        if requirement is not None:
            pack["policy"]["rules"][3]["params"]["approver_requirement"] = requirement
        if approval is not None:
            pack["trust_profiles"][0]["approvals"] = [approval]
        return [m for c, m in checks.run(PACKS / "fintech_v1.json", pack=pack, graphs_override=graphs) if c == "roles"]

    def test_the_four_a1_roles_and_the_two_fixed_requirements_pass(self):
        for req in ("SELF", "MAKER_CHECKER", "ROLE:customer", "ROLE:ops", "ROLE:approver", "ROLE:audit"):
            with self.subTest(req):
                self.assertEqual(self.run_with(requirement=req), [])
                self.assertEqual(self.run_with(approval=req), [])

    def test_a_role_outside_a1_is_refused_in_a_rule_and_in_a_profile(self):
        for role in ("supervisor", "ops_manager", "ops_agent", "viewer", "admin", "Approver", "approver2"):
            with self.subTest(role):
                self.assertTrue(any(f"ROLE:{role}" in m for m in self.run_with(requirement=f"ROLE:{role}")))
                self.assertTrue(any(f"ROLE:{role}" in m for m in self.run_with(approval=f"ROLE:{role}")))

    def test_an_empty_role_is_refused(self):
        self.assertTrue(self.run_with(requirement="ROLE:"))


class EdgeConditions(unittest.TestCase):
    """A377 ruling 10: an edge condition is a predicate, null is unconditional. A condition that is not a Boolean predicate, or names an unknown variable, fails."""

    def run_edge(self, condition):
        pack = json.loads((PACKS / "fintech_v1.json").read_text())
        graphs = {rel: json.loads((PACKS / rel).read_text()) for rel in pack["action_graph_files"]}
        graphs["fintech/graph/g1_investigate.json"]["edges"][0]["condition"] = condition
        return checks.run(PACKS / "fintech_v1.json", pack=pack, graphs_override=graphs)

    def test_null_is_unconditional_and_a_boolean_predicate_passes(self):
        self.assertEqual(self.run_edge(None), [])
        self.assertEqual(self.run_edge("subject_resource != null"), [])

    def test_a_non_boolean_or_unknown_condition_fails(self):
        for bad in ("subject_resource", "no_such_variable == 1", "== =="):
            with self.subTest(bad):
                self.assertTrue([m for c, m in self.run_edge(bad) if c == "predicate" and "edge[" in m])


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


class SubjectSlot(unittest.TestCase):
    """A520 (interim, DEBT-54). The subject of an intent is a pack-declared slot of its node: `subject_slot` names a slot of the SAME node that is a Resource slot. The core reads the name
    from the pack and never writes it in code (INV-14). A node with no Resource slot declares none."""

    def problems(self, node_index, value):
        base, graphs = real()
        base["intent_nodes"][node_index]["subject_slot"] = value
        return [m for c, m in checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs) if c == "subject"]

    def test_the_investigate_node_declares_its_subject_and_a_node_without_a_resource_slot_declares_none(self):
        base, _ = real()
        declared = {n["node_id"]: n.get("subject_slot") for n in base["intent_nodes"]}
        self.assertEqual(declared["fin.payments.exception.investigate"], "subject_resource")
        self.assertEqual([k for k, v in declared.items() if v], ["fin.payments.exception.investigate"])
        for n in base["intent_nodes"]:
            if n["node_id"] in declared and declared[n["node_id"]]:
                slot = next(s for s in n["slots"] if s["name"] == declared[n["node_id"]])
                self.assertEqual(slot["type"], "Resource")

    def test_the_real_pack_passes(self):
        base, graphs = real()
        self.assertEqual(checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs), [])

    def test_a_subject_slot_that_is_not_a_slot_of_its_node_is_refused(self):
        self.assertTrue(self.problems(0, "no_such_slot"))
        base, graphs = real()
        for bad in ("SUBJECT_RESOURCE", "", "fin.transaction"):  # not a slot name at all: the contract's pattern refuses it before the pack check runs
            base["intent_nodes"][0]["subject_slot"] = bad
            self.assertTrue(checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs), f"{bad!r} is accepted")

    def test_a_subject_slot_that_is_not_a_resource_slot_is_refused(self):
        for bad in ("amount", "counterparty", "when", "question_type", "requested_remedy"):
            found = self.problems(0, bad)
            self.assertTrue(found, f"{bad!r} is accepted")
            self.assertTrue(any("Resource" in m for m in found), found)

    def test_a_node_with_no_slot_cannot_declare_a_subject(self):
        self.assertTrue(self.problems(1, "subject_resource"))

    def test_a_subject_slot_that_is_not_a_string_is_refused_by_the_schema(self):
        base, graphs = real()
        for bad in (None, 7, ["subject_resource"], {"a": 1}, True):
            base["intent_nodes"][0]["subject_slot"] = bad
            found = checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs)
            self.assertTrue(found, f"{bad!r} is accepted")


if __name__ == "__main__":
    unittest.main()


WRITE_WHEN = "question_type == 'ESCALATION_REQUEST' or requested_remedy != null"
G2 = "fintech/graph/g2_escalate.json"


def real():
    base = json.loads((PACKS / "fintech_v1.json").read_text())
    return base, {rel: json.loads((PACKS / rel).read_text()) for rel in base["action_graph_files"]}


class WriteWhen(unittest.TestCase):
    """A364 (interim, relayed, not yet committed). `write_when` is a member of the intent NODE: a predicate over the node's slots (an absent slot is null) that says the intent
    needs write evaluation. It parses, is Boolean and uses only the node's slot names."""

    def check(self, text):
        base, graphs = real()
        base["intent_nodes"][0]["write_when"] = text
        return [m for c, m in checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs) if c == "predicate" and "write_when" in m]

    def test_the_investigate_node_says_when_it_needs_a_write(self):
        base, _ = real()
        nodes = {n["node_id"]: n.get("write_when") for n in base["intent_nodes"]}
        self.assertEqual(nodes["fin.payments.exception.investigate"], WRITE_WHEN)
        self.assertEqual([k for k, v in nodes.items() if v], ["fin.payments.exception.investigate"])
        self.assertFalse(any({"may_write", "write_when"} & set(s) for s in base["evidence_sources"]))
        self.assertFalse(any("produced_by" in d for d in base["evidence_descriptors"]))

    def test_the_predicate_uses_the_packs_own_grammar_and_the_nodes_slots_only(self):
        for text, ok in ((WRITE_WHEN, True), ("true", True), ("requested_remedy == null and amount == null", True), ("not (when != null)", True),
                         ("undeclared_slot == null", False), ("question_type ==", False), ("1 + 1", False), ("question_type", False),
                         ("question_type == 'NOT_A_QUESTION'", False), ("case_id != null", False), ("subject_resource.status == 'RESOLVED'", False)):
            problems = self.check(text)
            self.assertEqual(not problems, ok, f"{text!r}: {problems}")


class GraphParams(unittest.TestCase):
    """A369 (interim, relayed, not yet committed). The escalation graph's params, and what packcheck holds them to: every required input of the capability is covered, no extra
    param, a name once per node, slots and map keys of the intent node, map values in `case_reasons`, facts declared, policy params carried by a PERMIT rule."""

    def test_the_real_escalation_graph_declares_the_specified_params(self):
        base, graphs = real()
        nodes = {n["node_id"]: {p["name"]: {k: v for k, v in p.items() if k != "name"} for p in n.get("params", [])} for n in graphs[G2]["nodes"]}
        self.assertEqual(nodes["create_escalation_case"].keys(), {"transaction_ref", "reason_code"})
        self.assertEqual(nodes["create_escalation_case"]["transaction_ref"], {"slot": "subject_resource"})
        m = nodes["create_escalation_case"]["reason_code"]["map"]
        self.assertEqual(m["from"], "question_type")
        vocab = next(s for s in base["intent_nodes"][0]["slots"] if s["name"] == "question_type")["vocabulary"]
        self.assertEqual(set(m["values"]), set(vocab))
        self.assertLessEqual(set(m["values"].values()), {c["code"] for c in base["case_reasons"]})
        self.assertEqual(nodes["record_investigation_evidence"], {"case_id": {"fact": "case_id"}, "payload": {"evidence_packet": True}})
        self.assertEqual(nodes["route_to_internal_owner"], {"case_id": {"fact": "case_id"}, "owner_queue": {"policy_param": "owner_queue"}})

    def test_case_reasons_are_unique_codes(self):
        base, graphs = real()
        base["case_reasons"].append(dict(base["case_reasons"][0]))
        hits = [m for c, m in checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs) if "case_reasons" in m]
        self.assertTrue(hits)

    def test_a_param_name_is_declared_once_per_node(self):
        base, graphs = real()
        graphs[G2]["nodes"][0]["params"].append({"name": "transaction_ref", "const": "x"})
        hits = [m for c, m in checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs) if c == "graph" and "declares param" in m]
        self.assertEqual(len(hits), 1, hits)

    def test_an_optional_input_may_be_given_and_a_node_without_params_declares_none(self):
        base, graphs = real()
        graphs[G2]["nodes"][0]["params"].append({"name": "priority", "const": "NORMAL"})
        self.assertEqual(checks.run(PACKS / "fintech_v1.json", pack=base, graphs_override=graphs), [])
        self.assertNotIn("params", next(n for g in graphs.values() for n in g["nodes"] if n["capability_id"] == "fin.cap.log_complaint_reference"))


class HandoffRoutes(unittest.TestCase):
    """A531 (interim, relayed, not yet committed; REQ-TRU-004, REQ-AG-002). The pack's `handoff_routes`: which reason code goes to a handoff, to a queue of the pack's own
    routing allowlist, for which intent nodes (absent: all). The route is data; a free-form destination is not representable. All data is SYNTHETIC."""

    ROUTE = {"reason_code": "NO_RUNNABLE_CAPABILITY", "owner_queue": "PAYMENTS_OPERATIONS"}

    def routing(self, base):
        return [m for c, m in checks.run(PACKS / "fintech_v1.json", pack=base) if c == "routing" and "handoff" in m]

    def with_routes(self, routes):
        base, _ = real()
        base["handoff_routes"] = routes
        return base

    def test_the_real_pack_routes_no_runnable_capability_to_a_queue_of_its_own_allowlist(self):
        base, _ = real()
        routes = {r["reason_code"]: r for r in base.get("handoff_routes", [])}
        self.assertIn("NO_RUNNABLE_CAPABILITY", routes)
        self.assertIn(routes["NO_RUNNABLE_CAPABILITY"]["owner_queue"], base["routing_allowlist"])
        self.assertNotIn("intent_nodes", routes["NO_RUNNABLE_CAPABILITY"])

    def test_a_route_to_a_queue_outside_the_allowlist_is_refused(self):
        hits = self.routing(self.with_routes([{**self.ROUTE, "owner_queue": "NOT_A_QUEUE"}]))
        self.assertTrue(any("allowlist" in m for m in hits), hits)

    def test_a_reason_code_routed_twice_is_refused(self):
        hits = self.routing(self.with_routes([self.ROUTE, dict(self.ROUTE)]))
        self.assertTrue(any("twice" in m for m in hits), hits)

    def test_an_intent_node_the_pack_does_not_declare_is_refused(self):
        hits = self.routing(self.with_routes([{**self.ROUTE, "intent_nodes": ["fin.not.a.node"]}]))
        self.assertTrue(any("intent node" in m for m in hits), hits)

    def test_a_route_for_a_declared_node_and_a_distinct_reason_passes(self):
        base, _ = real()
        node = base["intent_nodes"][0]["node_id"]
        base["handoff_routes"] = [self.ROUTE, {"reason_code": "INSUFFICIENT_EVIDENCE", "owner_queue": base["routing_allowlist"][0], "intent_nodes": [node]}]
        self.assertEqual(self.routing(base), [])
        self.assertEqual([p for p in checks.run(PACKS / "fintech_v1.json", pack=base) if p[0] in ("schema", "reasons")], [])

    def test_a_reason_code_that_is_not_a_core_code_is_refused(self):
        hits = [m for c, m in checks.run(PACKS / "fintech_v1.json", pack=self.with_routes([{**self.ROUTE, "reason_code": "fin.reason.permitted"}]))]
        self.assertTrue(any("fin.reason.permitted" in m for m in hits), hits)
