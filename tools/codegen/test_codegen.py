"""CON-03 (REQ-API-001, REQ-TST-003): the generator, its refusals and the committed output. Standard library only.

Run: python3 -I -m unittest discover -s tools/codegen -p 'test_*.py'
"""
import json
import re
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
sys.path.insert(0, str(HERE))

import generate  # noqa: E402
import ir  # noqa: E402
import ts  # noqa: E402
from anchors import fully_anchored  # noqa: E402


def schema(**body):
    return {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://contracts.noema.invalid/t.v1.json", "title": "T", **body}


class Refusals(unittest.TestCase):
    """The generator never guesses: a keyword or a combination it does not model is an error naming the place."""

    def test_an_unknown_keyword_is_an_error(self):
        with self.assertRaisesRegex(ir.GenerationError, r"t\.v1\.json.*frobnicate"):
            ir.build_schemas({"t.v1.json": schema(type="object", frobnicate=True)})

    def test_an_unknown_keyword_inside_a_property_is_an_error(self):
        s = schema(type="object", properties={"a": {"type": "string", "frobnicate": 1}})
        with self.assertRaisesRegex(ir.GenerationError, r"properties/a.*frobnicate"):
            ir.build_schemas({"t.v1.json": s})

    def test_an_unresolvable_reference_is_an_error(self):
        s = schema(type="object", properties={"a": {"$ref": "#/$defs/Missing"}})
        with self.assertRaisesRegex(ir.GenerationError, "Missing"):
            ir.build_schemas({"t.v1.json": s})

    def test_an_object_that_refines_a_reference_with_its_own_shape_is_an_error(self):
        s = schema(type="object", properties={"a": {"type": "string"}}, allOf=[{"$ref": "#/$defs/B"}], **{"$defs": {"B": {"type": "object"}}})
        with self.assertRaisesRegex(ir.GenerationError, "allOf"):
            ir.build_schemas({"t.v1.json": s})

    def test_a_required_property_that_is_not_declared_is_an_error(self):
        s = schema(type="object", properties={"a": {"type": "string"}}, required=["a", "b"])
        with self.assertRaisesRegex(ir.GenerationError, r"required.*b"):
            ir.build_schemas({"t.v1.json": s})


class Shapes(unittest.TestCase):
    """What a schema becomes, on a small schema, so a regression names the rule that broke."""

    def build(self, **body):
        return ir.build_schemas({"t.v1.json": schema(**body)})

    def test_conditional_and_validation_keywords_do_not_change_the_shape(self):
        s = self.build(type="object", properties={"a": {"type": "string", "pattern": "^x$", "minLength": 1}}, required=["a"],
                       additionalProperties=False, allOf=[{"if": {"properties": {"a": {"const": "x"}}}, "then": {"required": ["a"]}}])
        root = s["t.v1.json"].root
        self.assertEqual([(f.prop, f.required) for f in root.type.fields], [("a", True)])
        self.assertTrue(root.type.closed)

    def test_a_nullable_pair_is_nullable_not_a_union(self):
        s = self.build(type="object", properties={"a": {"oneOf": [{"type": "string"}, {"type": "null"}]}, "b": {"type": ["integer", "null"]}}, required=["a", "b"])
        a, b = s["t.v1.json"].root.type.fields
        self.assertTrue(a.type.nullable and b.type.nullable)

    def test_an_all_string_union_is_a_string_and_a_mixed_union_is_a_union(self):
        s = self.build(type="object", properties={
            "a": {"oneOf": [{"type": "string", "pattern": "^a$"}, {"type": "string", "pattern": "^b$"}]},
            "b": {"oneOf": [{"type": "string"}, {"type": "integer"}]}})
        built = s["t.v1.json"]
        a, b = built.root.type.fields
        self.assertIsInstance(a.type, ir.Prim)
        self.assertIsInstance(b.type, ir.Ref)  # a mixed union is hoisted into a named type
        self.assertIsInstance(built.named[b.type.name].type, ir.Union)

    def test_constraint_only_alternatives_are_not_shapes(self):
        s = self.build(type="object", properties={"a": {"type": "string"}}, oneOf=[{"required": ["a"]}, {"not": {"required": ["a"]}}])
        self.assertIsInstance(s["t.v1.json"].root.type, ir.Obj)

    def test_an_inline_object_and_an_inline_enum_are_hoisted_with_a_derived_name(self):
        s = self.build(type="object", properties={"box": {"type": "object", "properties": {"x": {"type": "integer"}}}, "kind": {"enum": ["A", "B"]}})
        self.assertEqual(sorted(s["t.v1.json"].named), ["TV1", "TV1Box", "TV1Kind"])


class Determinism(unittest.TestCase):
    def test_two_runs_write_the_same_bytes(self):
        a = generate.render_all(generate.load_schemas())
        b = generate.render_all(generate.load_schemas())
        self.assertEqual(a, b)

    def test_every_schema_and_definition_has_a_type_in_both_languages(self):
        schemas = generate.load_schemas()
        files = generate.render_all(schemas)
        built = {f: b for f, b in ir.build_schemas(schemas).items() if not generate.is_excluded(f)}
        go = "\n".join(v for k, v in files.items() if k.startswith(generate.GO_DIR) and k.endswith("_gen.go"))
        built_go = {f for f in built if not generate.is_excluded(f, "go")}
        py = "\n".join(v for k, v in files.items() if k.startswith(generate.PY_DIR) and k.endswith(".py"))
        self.assertEqual(len(built), len([f for f in schemas if not generate.is_excluded(f)]))
        self.assertTrue(all(generate.is_excluded(f) for f in schemas if f not in built))
        for fname, b in built.items():
            for name in b.named:
                if fname in built_go:
                    self.assertRegex(go, r"(?m)^type %s\b" % name, f"Go type for {fname} {name}")
                self.assertRegex(py, r"(?m)^(class %s\b|%s\s*[:=])" % (name, name), f"Python type for {fname} {name}")


class Exclusions(unittest.TestCase):
    def test_only_the_benchmark_arm_and_fault_plan_contracts_are_excluded(self):
        # excluded.json matches a contract's base name in every version, so this does too: a new version of an excluded contract
        # (fault-plan.v2 came with ENV-15) is excluded, and nothing but those contracts is. No literal count or version to bump.
        def base(name):
            return re.sub(r"\.v[0-9]+\.json$", "", name)

        def belongs(name):
            return base(name).startswith("benchmark-") or base(name) in ("arm-manifest", "fault-plan")

        names = sorted(f for f in generate.load_schemas() if generate.is_excluded(f))
        self.assertTrue(all(belongs(n) for n in names), names)
        self.assertEqual(names, sorted(f for f in generate.load_schemas() if belongs(f)), "every version of those contracts is excluded")
        self.assertEqual([f for f in generate.load_schemas() if generate.is_excluded(f, "go") and not generate.is_excluded(f)], ["memory-observation.v1.json"])

    def test_an_excluded_contract_stays_excluded_in_every_version(self):
        for f in ("fault-plan.v1.json", "fault-plan.v2.json", "benchmark-scorecard.v2.json", "arm-manifest.v3.json"):
            self.assertTrue(generate.is_excluded(f), f)
        for f in ("faults.v1.json", "action-graph.v2.json", "record-event.v3.json"):
            self.assertFalse(generate.is_excluded(f), f)

    def test_a_service_contract_may_not_refer_to_an_excluded_one(self):
        s = generate.load_schemas()
        s["t.v1.json"] = schema(type="object", properties={"a": {"$ref": "https://contracts.noema.invalid/fault-plan.v1.json"}})
        s["t.v1.json"]["$id"] = "https://contracts.noema.invalid/t.v1.json"
        with self.assertRaisesRegex(ir.GenerationError, "excluded"):
            generate.render_all(s)


class CommittedOutput(unittest.TestCase):
    """REQ-API-001: regenerating gives no diff, and any hand edit, addition or removal is found."""

    def test_the_committed_output_is_current(self):
        self.assertEqual(generate.check(ROOT), [])

    def copy_output(self):
        tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, tmp, True)
        for d in (generate.GO_DIR, generate.PY_DIR):
            shutil.copytree(ROOT / d, tmp / d)
        return tmp

    def test_a_hand_edit_of_a_generated_go_file_is_found(self):
        tmp = self.copy_output()
        f = next(p for p in sorted((tmp / generate.GO_DIR).glob("*_gen.go")))
        f.write_text(f.read_text() + "\n// edited by hand\n")
        problems = generate.check(tmp)
        self.assertTrue(any(f.name in p and "differs" in p for p in problems), problems)

    def test_a_hand_edit_of_a_generated_python_file_is_found(self):
        tmp = self.copy_output()
        f = next(p for p in sorted((tmp / generate.PY_DIR).glob("common_v1.py")))
        f.write_text(f.read_text().replace("class", "class ", 1))
        problems = generate.check(tmp)
        self.assertTrue(any("common_v1.py" in p and "differs" in p for p in problems), problems)

    def test_an_added_file_in_a_generated_directory_is_found(self):
        tmp = self.copy_output()
        (tmp / generate.GO_DIR / "extra.go").write_text("package contracts\n\ntype Hand struct{}\n")
        (tmp / generate.PY_DIR / "extra.py").write_text("class Hand: ...\n")
        problems = generate.check(tmp)
        self.assertTrue(any("extra.go" in p for p in problems), problems)
        self.assertTrue(any("extra.py" in p for p in problems), problems)

    def test_a_generated_file_that_was_removed_is_found(self):
        tmp = self.copy_output()
        (tmp / generate.PY_DIR / "money_v1.py").unlink()
        self.assertTrue(any("money_v1.py" in p and "missing" in p for p in generate.check(tmp)))

    def test_the_intent_reference_parser_is_the_one_hand_written_go_file_allowed_beside_the_types(self):
        """The parser of common.v1#IntentRef sits next to the generated type (ruling A319); no other hand-written Go file is allowed."""
        tmp = self.copy_output()
        (tmp / generate.GO_DIR / "intentref.go").write_text("package contracts\n\nfunc ParseIntentRef(string) {}\n")
        self.assertEqual([p for p in generate.check(tmp) if "intentref" in p], [])
        (tmp / generate.GO_DIR / "intentref2.go").write_text("package contracts\n")
        self.assertTrue(any("intentref2.go" in p for p in generate.check(tmp)))
        (tmp / generate.GO_DIR / "intentref.go").write_text("// Code generated by tools/codegen. DO NOT EDIT.\npackage contracts\n")
        self.assertTrue(any("intentref.go" in p and "claims to be generated" in p for p in generate.check(tmp)))

    def test_a_forged_generated_header_on_an_extra_file_is_found(self):
        tmp = self.copy_output()
        (tmp / generate.GO_DIR / "forged_gen.go").write_text("// Code generated by tools/codegen. DO NOT EDIT.\npackage contracts\n")
        self.assertTrue(any("forged_gen.go" in p for p in generate.check(tmp)))


if __name__ == "__main__":
    unittest.main()


class Anchors(unittest.TestCase):
    """A138: a pattern means the whole string. Python's `$` also matches before a final newline; `\\Z` does not."""

    def test_the_leading_caret_and_trailing_dollar_become_whole_string_anchors(self):
        self.assertEqual(fully_anchored("^raw_[0-9A-Za-z]{2,32}$"), r"\Araw_[0-9A-Za-z]{2,32}\Z")

    def test_every_anchor_of_an_alternation_is_replaced(self):
        self.assertEqual(fully_anchored("^(a$|b$)"), r"\A(a\Z|b\Z)")

    def test_a_caret_or_dollar_in_a_character_class_or_escaped_is_kept(self):
        self.assertEqual(fully_anchored(r"^[^\n$]+\$[]^$]$"), r"\A[^\n$]+\$[]^$]\Z")

    def test_the_result_refuses_a_trailing_newline_that_the_dollar_form_accepts(self):
        import re

        self.assertIsNotNone(re.search("^ab$", "ab\n"))
        self.assertIsNone(re.search(fully_anchored("^ab$"), "ab\n"))
        self.assertIsNotNone(re.search(fully_anchored("^ab$"), "ab"))

    def test_text_that_may_hold_a_newline_still_may(self):
        import re

        self.assertIsNotNone(re.search(fully_anchored(r"^[\s\S]*$"), "a\nb\n"))


class TypeScriptOutput(unittest.TestCase):
    """REQ-UI-001: the TypeScript the UI reads comes from tools/codegen/ts.json, with every contract its roots refer to, and never from a contract kept out of the services."""

    def test_the_roots_and_what_they_refer_to_are_generated_and_nothing_else(self):
        files = generate.render_all(generate.load_schemas())
        ts_files = {p for p in files if p.startswith(generate.TS_DIR + "/")}
        self.assertIn(f"{generate.TS_DIR}/_runtime.ts", ts_files)
        self.assertIn(f"{generate.TS_DIR}/customer.ts", ts_files)
        self.assertIn(f"{generate.TS_DIR}/operator.ts", ts_files)
        self.assertNotIn(f"{generate.TS_DIR}/index.ts", ts_files)  # one entry point for everything would defeat the two audiences (A258)
        cfg = json.loads((HERE / "ts.json").read_text(encoding="utf-8"))
        roots = cfg["customer"] + cfg["operator"]
        for r in roots:
            self.assertIn(f"{generate.TS_DIR}/{generate.py.module_name(r)}.ts", ts_files)
        self.assertNotIn(f"{generate.TS_DIR}/benchmark_case_v1.ts", ts_files)  # a contract the UI does not read does not reach the bundle

    def test_the_front_end_reads_the_four_public_contracts_the_errors_and_the_reconstruction(self):
        # REQ-UI-001, REQ-API-001: the roots the UI reads. errors.v3 (the public error envelope) and reconstruction.v3 (the case view) are roots in their own right, not only
        # dependencies of another root, so the front end can import them by name.
        cfg = json.loads((HERE / "ts.json").read_text(encoding="utf-8"))
        roots = cfg["customer"] + cfg["operator"]
        files = generate.render_all(generate.load_schemas())
        for r in ("customer-answer.v1.json", "investigation-response.v1.json", "overlay-card.v1.json", "sse-event.v1.json", "errors.v3.json", "reconstruction.v3.json"):
            self.assertIn(r, roots, f"{r} is not a root of ts.json")
            self.assertIn(f"{generate.TS_DIR}/{generate.py.module_name(r)}.ts", files)

    def test_a_map_whose_keys_have_a_pattern_refuses_a_key_that_does_not_match(self):
        # reconstruction.v3 facts: propertyNames { pattern } with additionalProperties. The TypeScript parser must refuse a key outside the pattern, as the validator does
        # (the front end read an invalid fixture, fact_key_not_snake_case, as valid before this).
        files = generate.render_all(generate.load_schemas())
        self.assertIn("keyPattern?: RegExp", files[f"{generate.TS_DIR}/_runtime.ts"])
        self.assertRegex(files[f"{generate.TS_DIR}/reconstruction_v3.ts"], r"mapOf\([^\n]*, PATTERN_\d+\)")

    def test_the_customer_entry_point_reaches_no_operator_contract(self):
        # A258 (the Security Reviewer's question on noema#615): reconstruction.v3 and record-event.v3 carry tenant_id, the full events and their hashes: the operator and auditor UI
        # only. The customer entry point imports the four public contracts and errors.v3, and nothing it imports leads to those two.
        files = generate.render_all(generate.load_schemas())
        base = f"{generate.TS_DIR}/"
        todo, seen = [base + "customer.ts"], set()
        while todo:
            f = todo.pop()
            if f in seen:
                continue
            seen.add(f)
            todo.extend(base + m + ".ts" for m in re.findall(r'from "\./(\w+)\.js"', files[f]))
        names = {f[len(base):-3] for f in seen}
        self.assertTrue({"errors_v3", "customer_answer_v1", "overlay_card_v1", "investigation_response_v1", "message_request_v1", "sse_event_v1"} <= names)
        self.assertFalse(names & {"reconstruction_v3", "record_event_v3", "operator"}, names)
        parsers = set(re.findall(r'^  "([^"]+)": parse', files[base + "customer.ts"], re.M))
        self.assertEqual(parsers, {"customer-answer.v1", "errors.v3", "investigation-response.v1", "message-request.v1", "overlay-card.v1", "sse-event.v1"})

    def test_the_front_end_bundle_holds_only_what_a_root_reaches(self):
        # REQ-UI-001, INV-02 (the Security Reviewer's note 3 on noema#615): enums.v1 holds the core reason codes, and no root uses them; a shared contract file the UI reads only in part
        # must not put the unused definitions (the internal reason-code names) into the client bundle. Every parser the TypeScript holds is a root's or is called by another parser.
        files = generate.render_all(generate.load_schemas())
        entries = (f"{generate.TS_DIR}/operator.ts", f"{generate.TS_DIR}/benchmark.ts", f"{generate.TS_DIR}/approver.ts")
        ts = {k: v for k, v in files.items() if k.startswith(generate.TS_DIR + "/") and not k.endswith(("_runtime.ts", "customer.ts", "operator.ts", "benchmark.ts", "approver.ts"))}
        defined = {m: f for f, text in ts.items() for m in re.findall(r"^export function (parse\w+)\(", text, re.M)}
        roots = {"parse" + m for f, text in files.items() if f in entries for m in re.findall(r"import \{ parse(\w+) \}", text)}
        every = "\n".join(ts.values())
        for fn, f in sorted(defined.items()):
            uses = len(re.findall(rf"\b{fn}\(", every)) - 1 + len(re.findall(rf"\bcRef\({fn}\)", every))  # the definition itself; a rule refers to a parser as cRef(parseX)
            self.assertTrue(fn in roots or uses > 0, f"{fn} in {f} is reached by no root: it would ship in the bundle unused")
        self.assertNotIn("CoreReasonCode", "".join(ts.values()))
        self.assertIn("parseEnumsV1Verdict", defined)  # noema-resolution.v1 uses it, so it stays

    def test_the_benchmark_scorecard_stays_out_of_the_ui_bundle(self):
        # INV-22, PERD 65.1: benchmark-scorecard.v3 refers to benchmark-case.v1 (the case with its expected block and its oracle reference), which no bundle may hold. Asking for it as
        # a root is the generator's refusal, with the reason, and the committed ts.json does not list it.
        cfg = json.loads((HERE / "ts.json").read_text(encoding="utf-8"))
        self.assertNotIn("benchmark-scorecard.v3.json", cfg["customer"] + cfg["operator"])
        every = ir.build_schemas(generate.load_schemas())
        original = generate.HERE
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "ts.json").write_text(json.dumps({"customer": ["benchmark-scorecard.v3.json"], "operator": []}), encoding="utf-8")
            (Path(d) / "excluded.json").write_text((HERE / "excluded.json").read_text(encoding="utf-8"), encoding="utf-8")
            generate.HERE = Path(d)
            try:
                with self.assertRaisesRegex(ir.GenerationError, "keeps out"):
                    generate.render_ts(generate.load_schemas(), every)
            finally:
                generate.HERE = original

    # -- the benchmark entry point (noema#677; REQ-UI-004, REQ-BEN-004, INV-22, A250, A258) ---------------------------------------------------------------
    @staticmethod
    def _closure(files: dict[str, str], entry: str) -> set[str]:
        """The generated modules an entry point (`customer`, `operator`, `benchmark`) imports, directly or through another: names without the folder and the extension, the entry included."""
        todo, seen = [entry], set()
        while todo:
            m = todo.pop()
            if m in seen:
                continue
            seen.add(m)
            todo.extend(re.findall(r'from "\./(\w+)\.js"', files[f"{generate.TS_DIR}/{m}.ts"]))
        return seen

    def test_the_benchmark_entry_point_holds_the_scorecard_view_and_only_that(self):
        # noema#677: scorecard-view.v1 (the approved aggregate view, A250) is read by the benchmark UI (REQ-UI-004) through its own entry point, `benchmark.ts`: a third audience beside
        # customer and operator. It is a root of ts.json under its own list, and of neither of the others.
        cfg = json.loads((HERE / "ts.json").read_text(encoding="utf-8"))
        self.assertEqual(cfg["benchmark"], ["scorecard-view.v1.json"])
        self.assertNotIn("scorecard-view.v1.json", cfg["customer"] + cfg["operator"])
        files = generate.render_all(generate.load_schemas())
        base = f"{generate.TS_DIR}/"
        self.assertIn(base + "scorecard_view_v1.ts", files)
        self.assertIn(base + "benchmark.ts", files)
        self.assertEqual(set(re.findall(r'^  "([^"]+)": parse', files[base + "benchmark.ts"], re.M)), {"scorecard-view.v1"})
        # the view refers to no other contract, so the entry point is the view, the runtime and itself
        self.assertEqual(self._closure(files, "benchmark"), {"benchmark", "scorecard_view_v1", "_runtime"})

    def test_the_three_entry_points_do_not_leak_into_each_other(self):
        # A258 and noema#677: the benchmark entry point reaches no contract of the customer or the operator set (apart from the runtime every parser needs), and neither of those two reaches
        # the scorecard view or the benchmark entry point. A customer page cannot import the benchmark view by following imports, and the benchmark view carries nothing of a customer.
        files = generate.render_all(generate.load_schemas())
        self.assertIn(f"{generate.TS_DIR}/benchmark.ts", files, "no benchmark entry point")
        customer, operator, bench = (self._closure(files, e) for e in ("customer", "operator", "benchmark"))
        shared = {"_runtime"}
        self.assertEqual((customer | operator) & bench, shared, "the benchmark entry point shares more than the runtime")
        self.assertFalse((customer | operator) & {"scorecard_view_v1", "benchmark"}, customer | operator)
        self.assertFalse(bench & {"customer", "operator", "reconstruction_v3", "record_event_v3", "customer_answer_v1", "errors_v3", "errors_v2"}, bench)

    def test_the_benchmark_entry_point_reaches_no_case_id_oracle_or_write_access(self):
        # INV-22, REQ-UI-004: the view is "the numbers of scorecard v3 and nothing that names a case". The type closure of the entry point holds no case id, no list of ids, no failure list, no
        # structural evidence, no oracle or held-out reference, no tenant, and nothing a write role or a credential would be named by. (`writes_idempotent_and_verified` is a diagnostic rate, not write access.) The scan reads the names of the emitted types and of their
        # members, not the enum values (a Tier 0 gate is *named* ORACLE_ACCESS: it is a count, not a reference) and not the doc comments (the schema says what it leaves out).
        files = generate.render_all(generate.load_schemas())
        base = f"{generate.TS_DIR}/"
        self.assertIn(base + "benchmark.ts", files, "no benchmark entry point")
        modules = self._closure(files, "benchmark") - {"_runtime"}
        self.assertEqual(modules, {"benchmark", "scorecard_view_v1"})
        names, members, lists = [], [], []
        for m in sorted(modules):
            text = files[base + m + ".ts"]
            names += re.findall(r"^export (?:interface|type) (\w+)", text, re.M)
            members += re.findall(r'^  "?([A-Za-z0-9_$]+)"?\??: ', text, re.M)
            lists += re.findall(r"^export type \w+ = string\[\];|^  \S+\??: string\[\];", text, re.M)
        self.assertIn("ScorecardViewV1", names)
        self.assertTrue({"failure_count", "failing_case_count", "incomplete_counts"} <= set(members), "the scan does not see the members it is meant to inspect")
        forbidden = ("caseid", "oracle", "heldout", "expected", "benchmarkcase", "failurelist", "failingcases", "structural", "evidence", "human", "tenant", "credential", "secret", "token", "role",
                     "writer", "writeaccess", "grant", "session")
        for n in names + members:
            flat = re.sub(r"[^a-z0-9]", "", n.lower())
            self.assertFalse([w for w in forbidden if w in flat], f"{n} names something INV-22 keeps out of the benchmark view")
        self.assertEqual(lists, [], "a list of free strings (a list of ids) would carry what the view leaves out")
        # nothing is imported but the view and the runtime: no client, no fetch, no other contract
        for m in modules:
            for imported in re.findall(r'from "\./([\w.]+)"', files[base + m + ".ts"]):
                self.assertIn(imported, {"_runtime.js", "scorecard_view_v1.js"}, f"{m} imports {imported}")

    # -- the approver entry point (noema#760, after #758; REQ-UI-001, REQ-TRU-009, INV-12, INV-25, A258) ------------------------------------------------------
    def test_ts_json_names_exactly_four_audiences_and_the_approver_one_holds_the_confirmation_answer_only(self):
        # noema#760: confirmation-answer.v1 (the closed {request_id, response} body an approver sends) gets its own audience, `approver`, as the scorecard view has `benchmark`. A433: a
        # customer may answer a SELF confirmation, so the customer entry point exports it too; no customer page imports the approver entry. The approver list is that one schema: an
        # operator or a customer contract listed under it would put that contract on the approver's path. The benchmark list never holds it.
        cfg = json.loads((HERE / "ts.json").read_text(encoding="utf-8"))
        self.assertEqual(sorted(cfg), ["approver", "benchmark", "comment", "customer", "operator"])
        self.assertEqual(cfg["approver"], ["confirmation-answer.v1.json"])
        self.assertIn("confirmation-answer.v1.json", cfg["customer"], "A433: a customer SELF confirmation uses the customer entry point")
        self.assertNotIn("confirmation-answer.v1.json", cfg["benchmark"])

    def test_the_approver_entry_point_reaches_that_schema_and_the_shared_definitions_only(self):
        files = generate.render_all(generate.load_schemas())
        base = f"{generate.TS_DIR}/"
        self.assertTrue(base + "approver.ts" in files, "no approver entry point")
        self.assertTrue(base + "confirmation_answer_v1.ts" in files, "no confirmation-answer module")
        self.assertEqual(set(re.findall(r'^  "([^"]+)": parse', files[base + "approver.ts"], re.M)), {"confirmation-answer.v1"})
        # the answer, the runtime and the two shared definition files it refers to (common.v1: the request id; enums.v1: the response): no entry point, no contract of another audience
        self.assertEqual(self._closure(files, "approver"), {"approver", "confirmation_answer_v1", "common_v1", "enums_v1", "_runtime"})

    def test_the_customer_entry_exports_the_answer_and_no_entry_point_reaches_the_approver_one(self):
        # A433: the customer entry point exports confirmation-answer.v1 (a customer answers a SELF confirmation); no customer page imports the approver entry, so no entry point other than
        # approver.ts reaches the approver entry; the benchmark entry reaches neither the answer nor the approver one.
        files = generate.render_all(generate.load_schemas())
        self.assertTrue(f"{generate.TS_DIR}/approver.ts" in files, "no approver entry point")
        approver = self._closure(files, "approver")
        others = {e: self._closure(files, e) for e in ("customer", "operator", "benchmark")}
        for e, reached in others.items():
            self.assertNotIn("approver", reached, f"{e} reaches the approver entry point: {reached}")
        self.assertIn("confirmation_answer_v1", others["customer"], "A433: the customer entry point exports the answer")
        self.assertIn("confirmation_answer_v1", others["operator"], "the operator entry point holds the customer set")
        self.assertNotIn("confirmation_answer_v1", others["benchmark"])
        self.assertEqual(set(re.findall(r'^  "([^"]+)": parse', files[f"{generate.TS_DIR}/customer.ts"], re.M)) & {"confirmation-answer.v1"}, {"confirmation-answer.v1"})
        # what the approver shares with the customer is the answer, the runtime and the two shared definition files, nothing that is a contract of another audience
        self.assertEqual(approver & others["customer"], {"_runtime", "common_v1", "enums_v1", "confirmation_answer_v1"})
        self.assertFalse(approver & {"customer", "operator", "benchmark", "reconstruction_v3", "record_event_v3", "confirmation_request_v1", "noema_resolution_v1", "customer_answer_v1", "errors_v2", "errors_v3",
                                    "overlay_card_v1", "sse_event_v1", "scorecard_view_v1"}, approver)
        self.assertEqual(approver & others["benchmark"], {"_runtime", "common_v1", "enums_v1"} & others["benchmark"])

    def test_a_union_names_its_type_so_that_the_alternatives_do_not_have_to_agree_by_inference(self):
        # REQ-UI-001: the four public contracts (noema#521) hold a oneOf of different object shapes. `union(v, p, [a, b])` lets TypeScript infer T from the first
        # alternative, so `tsc` refused the second one. The parser names the union's own type: `union<T>(...)`.
        files = generate.render_all(generate.load_schemas())
        for module, name in (("overlay_card_v1", "OverlayCardV1"), ("sse_event_v1", "SseEventV1")):
            text = files[f"{generate.TS_DIR}/{module}.ts"]
            self.assertIn(f"return union<{name}>(v, p, [", text, f"{module}: the union does not name its type")

    def test_a_root_that_is_not_a_contract_is_an_error(self):
        every = ir.build_schemas(generate.load_schemas())
        original = generate.HERE
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "ts.json").write_text(json.dumps({"customer": ["nothing.v1.json"], "operator": []}), encoding="utf-8")
            generate.HERE = Path(d)
            try:
                with self.assertRaisesRegex(ir.GenerationError, "nothing.v1.json"):
                    generate.render_ts(generate.load_schemas(), every)
            finally:
                generate.HERE = original

    def test_a_root_kept_out_of_the_services_is_an_error(self):
        every = ir.build_schemas(generate.load_schemas())
        original = generate.HERE
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "ts.json").write_text(json.dumps({"customer": ["benchmark-case.v1.json"], "operator": []}), encoding="utf-8")
            (Path(d) / "excluded.json").write_text((HERE / "excluded.json").read_text(encoding="utf-8"), encoding="utf-8")
            generate.HERE = Path(d)
            try:
                with self.assertRaisesRegex(ir.GenerationError, "keeps out"):
                    generate.render_ts(generate.load_schemas(), every)
            finally:
                generate.HERE = original

    def test_a_pattern_is_kept_as_written_because_ecma_262_dollar_is_whole_string(self):
        files = generate.render_all(generate.load_schemas())
        self.assertIn('new RegExp("^[0-9]+\\\\.[0-9]+$", "u")', files[f"{generate.TS_DIR}/common_v1.ts"])


class TypeScriptConditionals(unittest.TestCase):
    """noema#682 (REQ-UI-001, REQ-API-001, AGENT-15, INV-02): the TypeScript parser holds the conditional rules of a schema, and a keyword the emitter does not implement is an
    error, never silence. The behaviour of the emitted checks is tested on the real fixtures in ui/test/contracts.test.mjs."""

    def render(self, **body) -> str:
        built = ir.build_schemas({"t.v1.json": schema(**body)})
        return ts.Ts(built).render_schema(built["t.v1.json"])

    def prop(self, **node) -> dict:
        return {"type": "object", "properties": {"a": {"type": "object", "properties": {"x": {"type": "string"}}, **node}}}

    # One sample per validation or applicator keyword the generator reads: the property schema it sits on, and the keyword itself.
    SAMPLES = {
        "pattern": {"type": "string", "pattern": "^a$"},
        "minLength": {"type": "string", "minLength": 2},
        "maxLength": {"type": "string", "maxLength": 2},
        "format": {"type": "string", "format": "date-time"},
        "minimum": {"type": "integer", "minimum": 1},
        "maximum": {"type": "integer", "maximum": 1},
        "exclusiveMinimum": {"type": "number", "exclusiveMinimum": 0},
        "minItems": {"type": "array", "items": {"type": "string"}, "minItems": 1},
        "maxItems": {"type": "array", "items": {"type": "string"}, "maxItems": 1},
        "uniqueItems": {"type": "array", "items": {"type": "string"}, "uniqueItems": True},
        "contains": {"type": "array", "items": {"type": "string"}, "contains": {"const": "a"}},
        "prefixItems": {"type": "array", "prefixItems": [{"type": "string"}], "items": False},
        "minProperties": {"type": "object", "properties": {"x": {"type": "string"}}, "minProperties": 1},
        "maxProperties": {"type": "object", "properties": {"x": {"type": "string"}}, "maxProperties": 1},
        "propertyNames": {"type": "object", "additionalProperties": {"type": "string"}, "propertyNames": {"maxLength": 3}},
        "not": {"type": "string", "not": {"const": "a"}},
        "if": {"type": "string", "if": {"const": "a"}, "then": {"minLength": 2}},
        "then": {"type": "string", "then": {"minLength": 2}},
        "else": {"type": "string", "else": {"minLength": 2}},
        "anyOf": {"type": "string", "anyOf": [{"minLength": 2}, {"const": "a"}]},
        "allOf": {"type": "string", "allOf": [{"minLength": 2}]},
    }

    def test_no_keyword_is_silently_ignored(self):
        """For each keyword the IR reads: the emitter either writes a check for it (the output changes) or refuses (GenerationError). Output identical to the output without
        the keyword is the silent drop that let noema#615 ship a parser that accepted eight invalid fixtures."""
        for kw, node in self.SAMPLES.items():
            bare = {k: v for k, v in node.items() if k not in (kw, "then", "else")}
            if kw == "prefixItems":
                bare["items"] = {"type": "string"}  # `items: false` is not a schema without prefixItems
            try:
                with_kw = self.render(type="object", properties={"a": node})
            except ir.GenerationError:
                continue
            without = self.render(type="object", properties={"a": bare})
            self.assertNotEqual(with_kw, without, f"{kw} is read by the IR and written nowhere in the TypeScript: a silent ignore")

    def test_every_keyword_the_ir_reads_has_a_sample(self):
        self.assertEqual(sorted(ir.VALIDATION - set(self.SAMPLES)), [], "a keyword was added to ir.VALIDATION without a case in SAMPLES")

    def test_the_keywords_the_emitter_does_not_implement_are_refused(self):
        for kw in ("format", "then", "else"):
            with self.assertRaisesRegex(ir.GenerationError, kw, msg=kw):
                self.render(type="object", properties={"a": self.SAMPLES[kw]})

    def test_unique_items_is_written_as_a_check_and_false_is_nothing(self):
        """uniqueItems: true is enforced by the parser (cUnique); uniqueItems: false constrains nothing and writes nothing; any other value is refused."""
        on = self.render(type="object", properties={"a": self.SAMPLES["uniqueItems"]})
        self.assertIn("cUnique", on)
        off = self.render(type="object", properties={"a": {**self.SAMPLES["uniqueItems"], "uniqueItems": False}})
        self.assertNotIn("cUnique", off)
        with self.assertRaisesRegex(ir.GenerationError, "uniqueItems"):
            self.render(type="object", properties={"a": {**self.SAMPLES["uniqueItems"], "uniqueItems": "yes"}})

    def test_an_unknown_keyword_inside_a_conditional_is_refused(self):
        s = {"type": "object", "properties": {"a": {"type": "string"}}, "allOf": [{"if": {"properties": {"a": {"frobnicate": 1}}}, "then": {"required": ["a"]}}]}
        with self.assertRaisesRegex(ir.GenerationError, "frobnicate"):
            self.render(**s)

    def test_a_keyword_the_emitter_does_not_implement_in_a_real_contract_stops_generation(self):
        schemas = generate.load_schemas()
        schemas["reconstruction.v3.json"]["properties"]["events"]["format"] = "date-time"
        with self.assertRaisesRegex(ir.GenerationError, r"reconstruction\.v3\.json.*format"):
            generate.render_all(schemas)

    def test_prefix_items_and_a_conditional_are_written_as_checks(self):
        cell = lambda kind: {"allOf": [{"$ref": "#/$defs/Cell"}, {"properties": {"kind": {"const": kind}}, "required": ["kind"]}]}  # noqa: E731
        text = self.render(type="object", properties={
            "k": {"type": "string"},
            "pair": {"type": "array", "minItems": 2, "maxItems": 2, "items": False, "prefixItems": [cell("A"), cell("B")]},
        }, required=["k", "pair"], allOf=[{"if": {"properties": {"k": {"const": "x"}}, "required": ["k"]}, "then": {"required": ["pair"]}}],
            **{"$defs": {"Cell": {"type": "object", "properties": {"kind": {"type": "string"}}, "required": ["kind"]}}})
        self.assertIn("cPrefix(", text)
        self.assertIn("cIf(", text)

    def test_the_ir_keeps_the_rules_its_structure_does_not_carry(self):
        built = ir.build_schemas(generate.load_schemas())["reconstruction.v3.json"]
        section = built.named["ReconstructionV3Section"].type
        self.assertEqual([sorted(r) for r in section.rules], [["allOf"]])
        sections = next(f for f in built.root.type.fields if f.prop == "sections").type
        self.assertEqual([sorted(r) for r in sections.rules], [["items", "prefixItems"]])
        self.assertEqual(sections.item.rules, [], "a member's own rules hold at its position only: they stay with prefixItems")
        facts = next(f for f in built.named["ReconstructionV3Item"].type.fields if f.prop == "facts").type
        self.assertEqual([sorted(r) for r in facts.rules], [["maxProperties"]])

    def test_go_and_python_output_does_not_depend_on_the_rules(self):
        # The rules are for TypeScript. Dropping them all from the IR must not change a byte of the Go or the Pydantic output (noema#682 changed no file of either).
        schemas = generate.load_schemas()
        with_rules = generate.render_all(schemas)
        original = ir.Builder.rules_of
        ir.Builder.rules_of = lambda self, n, take, path: []
        try:
            without = generate.render_all(schemas)
        finally:
            ir.Builder.rules_of = original
        for rel, text in with_rules.items():
            if not rel.startswith(generate.TS_DIR):
                self.assertEqual(text, without[rel], rel)
