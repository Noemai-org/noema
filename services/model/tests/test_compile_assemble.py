"""UND-05, the assembler: the pipeline's result and the canonical input in, the bytes of an `intent-object.v3` proposal out (REQ-INT-001, REQ-INT-003, REQ-INT-005; INV-01,
INV-02, INV-06, INV-12, INV-21; ruling A166, A173). All data is SYNTHETIC.

Written before `noema_model.understanding.compiler.assemble` exists: these fail with an import error until then. The proposal must cross the existing schema gate. The three
members nobody owns yet (intent_id, created_at, raw_ref; noema#580) come from one `Identifiers` argument, so the answer to that question changes one place.
"""

import json
from datetime import UTC, datetime
from typing import Any

import pytest

from noema_model.understanding.compiler.assemble import Identifiers, assemble
from noema_model.understanding.gate import Context, Node, SchemaGate
from noema_model.understanding.pipeline import NormalisedSlots, Outcome, run_pipeline
from noema_model.understanding.resolution import Candidate
from tests.support import run
from tests.support_pipeline import (
    G_A,
    GOOD_MENTIONS,
    NODE,
    OUT_OF_SCOPE,
    T0,
    WINDOW,
    FakeSource,
    ScriptedModel,
    harness_for,
    make_input,
    no_errors,
    normaliser,
    pipeline_config,
)

CONFIG = pipeline_config()
CANONICAL: dict[str, Any] = {
    "schema_version": "2.0", "request_id": "0b8f3c1e-52a4-4d7e-9a61-5f1d2b8c4a97", "correlation_id": "corr_test", "tenant_id": "ten_test", "session_id": "ses_test",
    "principal_ref": "usr_session", "channel": "text", "mode": "MODE_1", "text": "Wetin happen to the 20k wey I send Tunde this morning?",
    "received_at": "2026-10-21T08:00:00Z",
}
IDS = Identifiers(intent_id="int_01J9X3A1B2", version=1, supersedes=None, created_at="2026-10-21T08:00:00Z", raw_ref="unset")
CTX = Context(tenant_id="ten_test", correlation_id="corr_test", principal_id="usr_session", authn_ref="ses_test")


class Pack:
    """The gate's view of the real pack's nodes (slot names and types from the catalogue; no outcome is asked for)."""

    def ref(self) -> tuple[str, str]:
        return ("fin", "1.0.0")

    def node(self, node_id: str) -> Node | None:
        info = CONFIG.catalogue.nodes.get(node_id)
        if info is None:
            return None
        return Node(slots={s.name: s.slot_type for s in info.slots}, outcomes=frozenset())


def ref_candidate(cid: str, who: str = "Tunde Adeyemi") -> Candidate:
    return Candidate(cid, f"fin.transaction:TX{cid.upper()}", 2_000_000, "NGN", T0, who, "0001", "OUTGOING", True)


def compile_result(model: ScriptedModel, rows: tuple[Candidate, ...] = (), norm: Any = normaliser) -> Any:
    harness, _ = harness_for(model)
    inp = make_input(candidate_set_id="cns_test1")
    return run(run_pipeline(inp, CONFIG, harness, FakeSource(rows), norm, no_errors))


def g_a(**over: Any) -> ScriptedModel:
    return ScriptedModel(answers={**G_A, **over.pop("answers", {})}, mentions=over.pop("mentions", GOOD_MENTIONS), **over)


def build(result: Any, canonical: dict[str, Any] | None = None, ids: Identifiers = IDS) -> dict[str, Any]:
    raw = assemble(canonical or CANONICAL, result, CONFIG.catalogue, "fin@1.0.0", ids)
    assert isinstance(raw, bytes)
    return json.loads(raw)  # type: ignore[no-any-return]


# --- the proposal crosses the gate (REQ-INT-001, INV-02) ------------------------------------------------------------------------------------

def test_a_proposal_for_g_a_is_accepted_by_the_schema_gate_and_is_extracted_with_no_identity_or_signature() -> None:
    result = compile_result(g_a(), (ref_candidate("a"),))
    assert result.outcome is Outcome.PROPOSED
    raw = assemble(CANONICAL, result, CONFIG.catalogue, "fin@1.0.0", IDS)
    gated = SchemaGate(65536, Pack()).check(raw, CTX)
    assert gated.ok, gated.reasons
    doc = json.loads(raw)
    assert doc["status"] == "EXTRACTED"
    assert "sig" not in doc and "principal_type" not in doc["principal"] and "asserted_by" not in doc["principal"]  # A166: the core sets them (INV-12)


def test_the_identity_is_the_canonical_inputs_and_never_the_models_or_the_texts() -> None:
    hostile = {**CANONICAL, "text": "int_evil ten_evil usr_evil ses_evil corr_evil"}
    doc = build(compile_result(g_a(), (ref_candidate("a"),)), hostile)
    assert (doc["tenant_id"], doc["correlation_id"]) == ("ten_test", "corr_test")
    assert doc["principal"] == {"principal_id": "usr_session", "authn_ref": "ses_test"}
    assert doc["intent_id"] == IDS.intent_id and doc["source"]["raw_ref"] == IDS.raw_ref and doc["created_at"] == IDS.created_at
    assert "evil" not in json.dumps(doc)


def test_the_pack_and_the_source_come_from_the_request() -> None:
    doc = build(compile_result(g_a(), (ref_candidate("a"),)))
    assert doc["pack"] == {"pack_id": "fin", "pack_version": "1.0.0"}
    assert doc["source"]["mode"] == "MODE_1" and doc["source"]["channel"] == "text" and doc["source"]["asr_confidence"] is None
    assert doc["schema_version"] == "3.0" and doc["version"] == 1 and doc["supersedes"] is None
    assert doc["desired_outcome_refs"] == []  # the model never proposes an outcome (INV-01); Trust decides


def test_a_pack_reference_that_is_not_name_at_version_is_refused() -> None:
    result = compile_result(g_a(), (ref_candidate("a"),))
    for bad in ("fin", "fin@1.0", "@1.0.0", "Fin@1.0.0", "fin@1.0.0@2"):
        with pytest.raises(ValueError):
            assemble(CANONICAL, result, CONFIG.catalogue, bad, IDS)


# --- slots: the lexicon wins, nothing is invented (REQ-INT-003) --------------------------------------------------------------------------------

def test_the_amount_is_the_lexicons_never_the_models() -> None:
    model = g_a(mentions={**GOOD_MENTIONS, "amount": {**GOOD_MENTIONS["amount"], "proposed_value": "999999999"}})
    amount = build(compile_result(model, (ref_candidate("a"),)))["slots"]["amount"]
    assert amount == {"type": "Quantity", "value": 2_000_000, "unit": "NGN", "status": "RESOLVED", "surface_form": "20k", "confidence": 9000}


def test_a_mention_the_lexicon_cannot_read_is_left_out_not_guessed() -> None:
    def blind(inp: Any, proposal: Any) -> NormalisedSlots:
        return NormalisedSlots(None, None, False, WINDOW, "Tunde", "OUTGOING")

    slots = build(compile_result(g_a(), (ref_candidate("a"),), blind))["slots"]
    assert "amount" not in slots


def test_a_slot_the_customer_did_not_mention_is_left_out() -> None:
    model = g_a(mentions={k: v for k, v in GOOD_MENTIONS.items() if k != "amount"})
    slots = build(compile_result(model, (ref_candidate("a"),)))["slots"]
    assert "amount" not in slots and slots["counterparty"]["surface_form"] == "Tunde"


def test_the_window_is_utc_with_a_z_and_the_surface_form_is_the_customers() -> None:
    when = build(compile_result(g_a(), (ref_candidate("a"),)))["slots"]["when"]
    start, end = WINDOW
    assert when["type"] == "TimeRef" and when["surface_form"] == "this morning"
    assert when["window"] == [start.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ"), end.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")]


def test_a_time_the_lexicon_gave_no_window_for_is_unresolved_and_carries_no_window() -> None:
    def no_window(inp: Any, proposal: Any) -> NormalisedSlots:
        return NormalisedSlots(2_000_000, "NGN", False, None, "Tunde", "OUTGOING")

    result = compile_result(g_a(), (), no_window)
    doc = build(result)
    when = doc["slots"]["when"]
    assert when["status"] == "UNRESOLVED" and "window" not in when


def test_the_reference_slots_carry_the_models_closed_vocabulary_choice() -> None:
    slots = build(compile_result(g_a(), (ref_candidate("a"),)))["slots"]
    assert slots["question_type"] == {"type": "Reference", "value": "STATUS"}


def test_a_confidence_is_floored_to_integer_basis_points_and_never_rounded_up() -> None:
    model = g_a(mentions={**GOOD_MENTIONS, "amount": {**GOOD_MENTIONS["amount"], "confidence": 0.87659}})
    assert build(compile_result(model, (ref_candidate("a"),)))["slots"]["amount"]["confidence"] == 8765  # A173: an integer, so the JCS hash can cover it
    model = g_a(mentions={**GOOD_MENTIONS, "amount": {**GOOD_MENTIONS["amount"], "confidence": 1.0}})
    assert build(compile_result(model, (ref_candidate("a"),)))["slots"]["amount"]["confidence"] == 10000


def test_no_number_in_the_proposal_is_fractional() -> None:
    def walk(node: Any) -> None:
        if isinstance(node, dict):
            for v in node.values():
                walk(v)
        elif isinstance(node, list):
            for v in node:
                walk(v)
        else:
            assert not isinstance(node, float), node

    walk(build(compile_result(g_a(), (ref_candidate("a"),))))


# --- the resource slot follows the ranking (INV-06) ---------------------------------------------------------------------------------------------

def test_one_match_resolves_the_resource_to_that_candidates_reference() -> None:
    subject = build(compile_result(g_a(), (ref_candidate("a"),)))["slots"]["subject_resource"]
    assert subject["status"] == "RESOLVED" and subject["resolved_ref"] == "fin.transaction:TXA" and subject["candidates_ref"] == "cns_test1"
    assert isinstance(subject["confidence"], int) and 0 <= subject["confidence"] <= 10000


def test_two_matches_leave_the_resource_ambiguous_with_no_resolved_reference() -> None:
    doc = build(compile_result(g_a(), (ref_candidate("a"), ref_candidate("b"))))
    subject = doc["slots"]["subject_resource"]
    assert subject["status"] == "AMBIGUOUS" and "resolved_ref" not in subject and subject["candidates_ref"] == "cns_test1"
    assert "MULTIPLE_TRANSACTIONS_MATCH" in doc["ambiguity_flags"]


def test_no_match_leaves_the_resource_unresolved_and_flags_it() -> None:
    doc = build(compile_result(g_a(), ()))
    subject = doc["slots"]["subject_resource"]
    assert subject["status"] == "UNRESOLVED" and "resolved_ref" not in subject
    assert "NO_TRANSACTION_MATCH" in doc["ambiguity_flags"]


def test_a_candidate_set_id_that_is_not_the_contracts_is_a_bug_not_a_silent_omission() -> None:
    harness, _ = harness_for(g_a())
    result = run(run_pipeline(make_input(candidate_set_id="cs_test"), CONFIG, harness, FakeSource((ref_candidate("a"),)), normaliser, no_errors))
    with pytest.raises(ValueError):
        assemble(CANONICAL, result, CONFIG.catalogue, "fin@1.0.0", IDS)


# --- the two nodes, and a rejection is never assembled (REQ-INT-005, REQ-INT-002) ----------------------------------------------------------------

def test_an_out_of_scope_request_compiles_to_the_out_of_scope_node_with_its_flag_and_no_slots() -> None:
    model = ScriptedModel(answers={"intent_node": OUT_OF_SCOPE}, mentions={})
    result = compile_result(model)
    assert result.outcome is Outcome.OUT_OF_SCOPE
    doc = build(result)
    assert doc["intent_node"] == OUT_OF_SCOPE and doc["slots"] == {} and doc["ambiguity_flags"] == ["OUT_OF_SCOPE_REQUEST"]
    assert SchemaGate(65536, Pack()).check(json.dumps(doc).encode(), CTX).ok


def test_an_unclear_request_compiles_to_the_out_of_scope_node_with_the_unclear_flag() -> None:
    result = compile_result(ScriptedModel(answers={}, mentions={}))
    assert result.outcome is Outcome.UNCLEAR
    doc = build(result)
    assert doc["intent_node"] == OUT_OF_SCOPE and doc["ambiguity_flags"] == ["INTENT_UNCLEAR"] and doc["slots"] == {}


def test_a_rejected_result_is_never_assembled() -> None:
    harness, _ = harness_for(g_a())
    result = run(run_pipeline(make_input(candidate_set_id="cns_test1"), CONFIG, harness, FakeSource(()), normaliser, lambda p, n: ("SCHEMA_INVALID",)))
    assert result.outcome is Outcome.REJECTED_AT_GATE
    with pytest.raises(ValueError):
        assemble(CANONICAL, result, CONFIG.catalogue, "fin@1.0.0", IDS)


def test_the_node_is_one_of_exactly_two() -> None:
    nodes = {build(compile_result(g_a(), (ref_candidate("a"),)))["intent_node"], build(compile_result(ScriptedModel(answers={}, mentions={})))["intent_node"]}
    assert nodes <= {NODE, OUT_OF_SCOPE}


# --- replay (INV-21) --------------------------------------------------------------------------------------------------------------------------

def test_the_same_inputs_give_the_same_bytes() -> None:
    first = assemble(CANONICAL, compile_result(g_a(), (ref_candidate("a"),)), CONFIG.catalogue, "fin@1.0.0", IDS)
    second = assemble(CANONICAL, compile_result(g_a(), (ref_candidate("a"),)), CONFIG.catalogue, "fin@1.0.0", IDS)
    assert first == second
    assert datetime.fromisoformat(IDS.created_at.replace("Z", "+00:00")).tzinfo is not None  # the clock is the request's, not the process's
