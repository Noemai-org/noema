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
        self.assertIn(f"{generate.TS_DIR}/index.ts", ts_files)
        roots = json.loads((HERE / "ts.json").read_text(encoding="utf-8"))["roots"]
        for r in roots:
            self.assertIn(f"{generate.TS_DIR}/{generate.py.module_name(r)}.ts", ts_files)
        self.assertNotIn(f"{generate.TS_DIR}/benchmark_case_v1.ts", ts_files)  # a contract the UI does not read does not reach the bundle

    def test_a_root_that_is_not_a_contract_is_an_error(self):
        every = ir.build_schemas(generate.load_schemas())
        original = generate.HERE
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "ts.json").write_text(json.dumps({"roots": ["nothing.v1.json"]}), encoding="utf-8")
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
            (Path(d) / "ts.json").write_text(json.dumps({"roots": ["benchmark-case.v1.json"]}), encoding="utf-8")
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
