"""The strict JSON schema every model request carries (INV-02, REQ-INT-002): no keyword beside a `$ref`, every object closed and fully required.

Run 44521380842 (gpt-6-luna) got HTTP 400 on all four `pipeline_slot_mentions` calls; each of its three slot properties was a `$ref` with a sibling
`description`, and `harness_decide` never puts a keyword beside a `$ref`. The 400 body was not recorded, so the cause is a hypothesis; these tests keep
the shape out of every schema the service sends, whatever the cause. No network, no key. The fixture is SYNTHETIC (a schema, no customer text)."""

import json
from pathlib import Path
from typing import Any

import pytest
from pydantic import BaseModel, ConfigDict, Field

from noema_model.harness import GenerateRequest, HarnessRejection, PromptTemplate, Reason
from noema_model.harness.strict_schema import strict_schema_problems
from noema_model.understanding.pipeline.catalogue import _TYPE_HELP, Mention
from tests.support import run
from tests.support_pipeline import GOOD_MENTIONS, NODE, FakeSource, ScriptedModel, candidate, harness_for, pipeline_config
from tests.test_pipeline import g_a_model, go

FIXTURE = json.loads((Path(__file__).parent / "fixtures" / "strict_schema" / "rec-0003-slot-mentions-format.json").read_text(encoding="utf-8"))
CATALOGUE = pipeline_config().catalogue


def sent_schemas() -> dict[str, list[dict[str, Any]]]:
    """Every schema the service sends: the pipeline's own calls (the decide task and the slot mentions), then one slot-mentions call per node
    of the real pack. Read from the requests the provider seam receives, which is what each adapter puts on the wire."""
    model = g_a_model()
    go(model, FakeSource((candidate("a"),)))
    sent: dict[str, list[dict[str, Any]]] = {}
    for request in model.requests:
        sent.setdefault(request.schema_name, []).append(dict(request.output_schema))
    for info in CATALOGUE.nodes.values():
        if info.mention_model is None:
            continue
        scripted = ScriptedModel(mentions=GOOD_MENTIONS)
        harness, _ = harness_for(scripted)
        run(harness.generate(GenerateRequest("slot_values", PromptTemplate("pipeline.slot_mentions", 1, "S", "I"), {"utterance": "x"}, info.mention_model, None, "cor_test")))
        (request,) = scripted.requests
        sent.setdefault(request.schema_name, []).append(dict(request.output_schema))
    return sent


def resolve(schema: dict[str, Any], node: dict[str, Any]) -> dict[str, Any]:
    ref = node["$ref"]
    assert ref.startswith("#/$defs/")
    target: dict[str, Any] = schema["$defs"][ref.removeprefix("#/$defs/")]
    return target


def descriptions(node: Any) -> list[str]:
    if isinstance(node, dict):
        own = [node["description"]] if isinstance(node.get("description"), str) else []
        return own + [d for v in node.values() for d in descriptions(v)]
    if isinstance(node, list):
        return [d for v in node for d in descriptions(v)]
    return []


# --- the checker itself: each rule can fail -----------------------------------------------------------------------------------------------------

CLOSED = {"type": "object", "properties": {"a": {"type": "string"}}, "required": ["a"], "additionalProperties": False}


def test_a_closed_fully_required_object_passes() -> None:
    assert strict_schema_problems(CLOSED) == []


@pytest.mark.parametrize("bad", [
    {**CLOSED, "required": []},
    {**CLOSED, "additionalProperties": True},
    {k: v for k, v in CLOSED.items() if k != "additionalProperties"},
    {"type": "object"},
    {"type": "array", "items": {"type": "string"}},
    {"type": "string"},
    {**CLOSED, "properties": {"a": {"type": "object", "properties": {"b": {"type": "string"}}, "required": ["b"]}}, "required": ["a"]},
    {**CLOSED, "properties": {"a": {"type": "array", "items": {"type": "object", "properties": {"b": {"type": "string"}}, "required": []}}}},
    {**CLOSED, "$defs": {"X": {"type": "object", "properties": {"b": {"type": "string"}}, "required": ["b"], "additionalProperties": True}}},
    {**CLOSED, "properties": {"a": {"type": ["object", "null"], "properties": {}, "required": []}}},
    {**CLOSED, "required": ["a", "b"]},
    {**CLOSED, "properties": {"a": {"type": "object"}}},
    {**CLOSED, "properties": {"a": {"type": ["object", "null"]}}},
    {**CLOSED, "properties": {"a": {"properties": {"b": {"type": "string"}}, "required": ["b"]}}},
    {**CLOSED, "properties": ["a"]},
    {**CLOSED, "properties": {"a": {"anyOf": [{"type": "object", "properties": {}, "required": []}]}}},
], ids=["unrequired", "open", "no-additional", "empty-open-object", "root-array", "root-string", "nested-open", "array-item-unrequired", "def-open", "typed-list-open", "extra-required", "nested-bare-object", "nested-typed-list-bare", "nested-typeless", "properties-not-a-mapping", "anyof-open"])
def test_each_strict_shape_rule_is_reported(bad: dict[str, Any]) -> None:
    assert strict_schema_problems(bad) != []


@pytest.mark.parametrize("sibling", ["description", "title", "default", "type"])
def test_a_ref_with_any_sibling_keyword_is_reported(sibling: str) -> None:
    schema = {**CLOSED, "properties": {"a": {"$ref": "#/$defs/X", sibling: "x"}}, "required": ["a"], "$defs": {"X": {"type": "string"}}}
    (problem,) = strict_schema_problems(schema)
    assert "$.properties.a" in problem and "$ref" in problem and sibling in problem


def test_a_root_ref_with_a_sibling_is_reported_at_the_root() -> None:
    problems = strict_schema_problems({"$ref": "#/$defs/X", "description": "d", "$defs": {"X": CLOSED}})
    assert any(p.startswith("$: $ref has sibling keywords") for p in problems)


def test_a_ref_alone_passes_and_a_ref_in_an_array_item_or_a_def_is_still_checked() -> None:
    ok = {**CLOSED, "properties": {"a": {"$ref": "#/$defs/X"}}, "$defs": {"X": {"type": "string"}}}
    assert strict_schema_problems(ok) == []
    in_items = {**CLOSED, "properties": {"a": {"type": "array", "items": {"$ref": "#/$defs/X", "description": "d"}}}, "$defs": {"X": {"type": "string"}}}
    in_def = {**CLOSED, "$defs": {"X": {"type": "array", "items": {"$ref": "#/$defs/Y", "description": "d"}}, "Y": {"type": "string"}}}
    assert "$.properties.a.items" in strict_schema_problems(in_items)[0]
    assert "$.$defs.X.items" in strict_schema_problems(in_def)[0]


def test_a_property_named_like_a_keyword_is_not_read_as_one() -> None:
    schema = {
        "type": "object",
        "properties": {"$ref": {"type": "string"}, "type": {"type": "string"}, "properties": {"type": "string"}},
        "required": ["$ref", "type", "properties"],
        "additionalProperties": False,
    }
    assert strict_schema_problems(schema) == []


@pytest.mark.parametrize("table", ["$defs", "definitions"])
def test_a_definition_named_like_a_keyword_is_not_read_as_one(table: str) -> None:
    schema = {**CLOSED, table: {"$ref": {"type": "string"}, "type": {"type": "string"}}}
    assert strict_schema_problems(schema) == []


def test_a_problem_inside_a_list_reports_its_index() -> None:
    schema = {**CLOSED, "properties": {"a": {"anyOf": [{"type": "string"}, {"type": "object", "properties": {}, "required": []}]}}}
    (problem,) = strict_schema_problems(schema)
    assert problem.startswith("$.properties.a.anyOf[1]: ")


def test_a_non_mapping_schema_is_reported_not_raised() -> None:
    assert strict_schema_problems(None) != [] and strict_schema_problems([CLOSED]) != []


# --- every schema the service sends ---------------------------------------------------------------------------------------------------------------

def test_the_named_schemas_the_service_sends_are_exactly_the_two_known_ones() -> None:
    """A new named schema fails here, so it cannot be sent unchecked."""
    assert set(sent_schemas()) == {"harness_decide", "pipeline_slot_mentions"}


def test_no_schema_the_service_sends_breaks_a_strict_shape_rule() -> None:
    """REQ-INT-002, INV-02: the whole strict schema, for every send, has no keyword beside a $ref and every object closed."""
    sent = sent_schemas()
    assert len(sent["harness_decide"]) == 6 and len(sent["pipeline_slot_mentions"]) >= 2
    problems = {name: strict_schema_problems(s) for name, schemas in sent.items() for s in schemas}
    assert all(strict_schema_problems(s) == [] for schemas in sent.values() for s in schemas), problems


def test_every_slot_mentions_property_is_a_bare_ref_to_a_closed_mention() -> None:
    for schema in sent_schemas()["pipeline_slot_mentions"]:
        assert schema["properties"], "a mention model has at least one slot"
        for name, prop in schema["properties"].items():
            assert set(prop) == {"$ref"}, name
            target = resolve(schema, prop)
            assert target["additionalProperties"] is False and set(target["required"]) == {"present", "surface_form", "proposed_value", "confidence"}


def test_the_meaning_each_slot_description_gave_is_still_in_the_schema_the_model_reads() -> None:
    """Moving the text must not drop it: every slot type's help text, and the Mention definition's text, reach the model, and each slot reaches its own."""
    schema = next(s for s in sent_schemas()["pipeline_slot_mentions"] if "counterparty" in s["properties"])
    help_by_slot = {"amount": _TYPE_HELP["Quantity"], "counterparty": _TYPE_HELP["Party"], "when": _TYPE_HELP["TimeRef"]}
    for slot, help_text in help_by_slot.items():
        reachable = descriptions(resolve(schema, schema["properties"][slot]))
        assert any(help_text in d for d in reachable), slot
        assert any(Mention.__doc__ and Mention.__doc__ in d for d in reachable), slot
        assert not any(other in d for other in help_by_slot.values() if other != help_text for d in reachable), slot
    assert any("True only if the customer actually mentioned it." in d for d in descriptions(schema))


# --- the real recording ----------------------------------------------------------------------------------------------------------------------------

def test_the_schema_of_recording_rec_0003_is_flagged_by_the_checker() -> None:
    """The checker catches the very shape that drew the 400 (SYNTHETIC fixture, copied as sent)."""
    schema = FIXTURE["schema"]
    problems = strict_schema_problems(schema)
    assert len(problems) == 3 and all("$ref has sibling keywords ['description']" in p for p in problems)


def test_the_schema_the_code_builds_for_the_fixture_node_keeps_its_names_and_texts_and_has_no_ref_sibling() -> None:
    """The same node as rec-0003: same name, same slots, same title, every text of the fixture still present, and the checker passes."""
    info = CATALOGUE.nodes[NODE]
    assert info.mention_model is not None
    same_node = [s for s in sent_schemas()["pipeline_slot_mentions"] if s.get("title") == FIXTURE["schema"]["title"]]
    assert len(same_node) == 2 and same_node[0] == same_node[1], "the pipeline's call and the direct call send one schema"
    schema = same_node[0]
    assert strict_schema_problems(schema) == []
    old = FIXTURE["schema"]
    assert list(schema["properties"]) == list(old["properties"]) and schema["required"] == old["required"]
    assert schema["additionalProperties"] is False and schema["type"] == "object"
    new_texts = descriptions(schema)
    for text in descriptions(old):
        assert any(text in n for n in new_texts), text
    for slot, old_prop in old["properties"].items():
        new_target = resolve(schema, schema["properties"][slot])
        assert any(old_prop["description"] in d for d in descriptions(new_target)), slot
        assert new_target["required"] == resolve(old, old_prop)["required"]


def test_the_pydantic_default_for_a_described_model_field_is_what_the_checker_refuses() -> None:
    """A guard on the cause: a described field of a model type is a $ref with a sibling, so a build like the old one is flagged."""
    class Inner(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
        x: str

    class Outer(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
        slot: Inner = Field(description="what the slot means")

    assert strict_schema_problems(Outer.model_json_schema()) != []


# --- the harness refuses such an output type before any call ----------------------------------------------------------------------------------------

def test_the_harness_refuses_an_output_type_whose_schema_puts_a_keyword_beside_a_ref() -> None:
    """INV-02: a schema a provider would refuse is refused locally, as OUTPUT_TYPE_UNSAFE, and nothing is sent."""
    class Inner(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
        x: str

    class Outer(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
        slot: Inner = Field(description="what the slot means")

    model = ScriptedModel()
    harness, _ = harness_for(model)
    with pytest.raises(HarnessRejection) as caught:
        run(harness.generate(GenerateRequest("slot_values", PromptTemplate("pipeline.slot_mentions", 1, "S", "I"), {"utterance": "x"}, Outer, None, "cor_test")))
    assert caught.value.reason is Reason.OUTPUT_TYPE_UNSAFE
    assert model.requests == []
