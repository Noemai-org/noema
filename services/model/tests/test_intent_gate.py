"""UND-04, the model service's side of the schema gate: REQ-INT-001 (every proposal crosses the gate), REQ-INT-002 (a failure is a rejection, never a repair),
REQ-INP-002 and INV-12 (identity is the session's, never the model's), INV-02 (model output is untrusted data).

All data is SYNTHETIC (a pack named `test`). Written before the gate: these fail with an import error until noema_model.understanding.gate exists.
The gate takes the compiler's proposal (status EXTRACTED, no `sig`) and returns it as VALIDATED, or the closed list of reasons it was refused. It signs nothing.
"""

import copy
import json
from typing import Any

import pytest
from hypothesis import HealthCheck, given, settings
from hypothesis import strategies as st

from noema_model.contracts.intent_object_v3 import IntentObjectV3
from noema_model.understanding.gate import UNSET, UNSIGNED, Context, GateResult, Node, Reason, SchemaGate

CTX = Context(tenant_id="ten_a1", correlation_id="corr_c1", principal_id="usr_88213", authn_ref="ses_7a1b")
NODE = "test.intent.investigate"
OUTCOME = "test.outcome.case_opened"


class Pack:
    def ref(self) -> tuple[str, str]:
        return ("test", "1.0.0")

    def node(self, node_id: str) -> Node | None:
        if node_id != NODE:
            return None
        return Node(
            slots={"subject_resource": "Resource", "amount": "Quantity", "when": "TimeRef", "counterparty": "Party", "requested_remedy": "Reference"},
            outcomes=frozenset({OUTCOME, "test.outcome.complaint_logged"}),
        )


def gate(max_bytes: int = 65536) -> SchemaGate:
    return SchemaGate(max_bytes=max_bytes, pack=Pack())


def proposal() -> dict[str, Any]:
    return {
        "schema_version": "3.0", "intent_id": "int_01J9X3", "version": 1, "supersedes": None, "tenant_id": "ten_a1", "correlation_id": "corr_c1",
        "created_at": "2026-11-12T10:00:00Z",
        "principal": {"principal_id": "usr_88213", "authn_ref": "ses_7a1b"},  # ruling A166: the model sends no principal_type and no asserted_by; the core sets them
        "source": {"mode": "MODE_1", "channel": "text", "locale_detected": ["en-NG"], "asr_confidence": None, "raw_ref": "inp_abc123"},
        "pack": {"pack_id": "test", "pack_version": "1.0.0"}, "intent_node": NODE,
        "slots": {
            "subject_resource": {"type": "Resource", "status": "RESOLVED", "resolved_ref": "test.transaction:TX88213", "confidence": 9000},
            "amount": {"type": "Quantity", "value": 2000000, "unit": "NGN", "surface_form": "20k"},
            "when": {"type": "TimeRef", "surface_form": "yesterday", "window": ["2026-11-11T00:00:00Z", "2026-11-11T23:59:59Z"]},
            "counterparty": {"type": "Party", "surface_form": "Ada"},
            "requested_remedy": {"type": "Reference", "value": "test.capability.log_complaint"},
        },
        "ambiguity_flags": [], "desired_outcome_refs": [OUTCOME], "status": "EXTRACTED",
    }


def raw(doc: dict[str, Any]) -> bytes:
    return json.dumps(doc).encode()


def check(doc: dict[str, Any], ctx: Context = CTX) -> GateResult:
    return gate().check(raw(doc), ctx)


def reasons(doc: dict[str, Any], ctx: Context = CTX) -> set[Reason]:
    return set(check(doc, ctx).reasons)


def test_a_correct_proposal_is_validated_and_nothing_else_changes() -> None:
    result = check(proposal())
    assert result.ok and result.reasons == () and result.intent is not None
    assert result.intent.status.value == "VALIDATED"
    assert result.intent.sig == UNSIGNED, "the gate signs nothing: the signature is added after the gate by the model service"
    expected = proposal()
    expected["status"] = "VALIDATED"
    expected["sig"] = {"key_id": UNSIGNED.key_id, "value": UNSIGNED.value}
    expected["principal"]["principal_type"] = UNSET  # placeholders the contract requires; the core overwrites them from the session (A166)
    expected["principal"]["asserted_by"] = UNSET
    assert result.intent.to_json_obj() == json.loads(json.dumps(expected))


def mutate(path: tuple[str | int, ...], value: Any) -> dict[str, Any]:
    doc = proposal()
    node: Any = doc
    for step in path[:-1]:
        node = node[step]
    node[path[-1]] = value
    return doc


def without(path: tuple[str, ...]) -> dict[str, Any]:
    doc = proposal()
    node: Any = doc
    for step in path[:-1]:
        node = node[step]
    del node[path[-1]]
    return doc


# The table: one rule each. A rejection names reasons, never the object's text.
REJECTIONS: dict[str, tuple[dict[str, Any], Reason]] = {
    "already VALIDATED (the gate sets that, the model does not)": (mutate(("status",), "VALIDATED"), Reason.STATUS_NOT_EXTRACTED),
    "REJECTED_AT_GATE from the model": (mutate(("status",), "REJECTED_AT_GATE"), Reason.STATUS_NOT_EXTRACTED),
    "schema version 3.1": (mutate(("schema_version",), "3.1"), Reason.SCHEMA_VERSION),  # a v3 object of a minor this gate does not know; v1 and v2 objects are not v3 (shape)
    "version 1 that supersedes": (mutate(("supersedes",), "int_01J9X3@0"), Reason.SUPERSEDES),
    "version 2 without supersedes": (mutate(("version",), 2), Reason.SUPERSEDES),
    "version 2 superseding another intent": (mutate(("version",), 2) | {"supersedes": "int_other@1"}, Reason.SUPERSEDES),
    "version 3 superseding version 1": (mutate(("version",), 3) | {"supersedes": "int_01J9X3@1"}, Reason.SUPERSEDES),
    "created_at that is not a calendar date": (mutate(("created_at",), "2026-13-45T10:00:00Z"), Reason.CREATED_AT),
    "another tenant": (mutate(("tenant_id",), "ten_b2"), Reason.IDENTITY_MISMATCH),
    "another principal": (mutate(("principal", "principal_id"), "usr_99999"), Reason.IDENTITY_MISMATCH),
    "another session": (mutate(("principal", "authn_ref"), "ses_other"), Reason.IDENTITY_MISMATCH),
    "another correlation": (mutate(("correlation_id",), "corr_zz"), Reason.CORRELATION_MISMATCH),
    "Mode 2": (mutate(("source", "mode"), "MODE_2"), Reason.MODE_NOT_ENABLED),
    "an oversize raw_ref": (mutate(("source", "raw_ref"), "x" * 257), Reason.SOURCE),
    "another pack": (mutate(("pack", "pack_id"), "other"), Reason.PACK_MISMATCH),
    "another pack version": (mutate(("pack", "pack_version"), "1.0.1"), Reason.PACK_MISMATCH),
    "a signature set by the model": (mutate(("sig",), {"key_id": "k", "value": "v"}), Reason.SIG_SET_BY_MODEL),
    "a repeated ambiguity flag": (mutate(("ambiguity_flags",), ["COUNTERPARTY_MULTIPLE_MATCH", "COUNTERPARTY_MULTIPLE_MATCH"]), Reason.AMBIGUITY_FLAG),
    "a node the pack does not define": (mutate(("intent_node",), "test.intent.other"), Reason.UNKNOWN_NODE),
    "a slot name the pack does not define": (mutate(("slots", "mystery"), {"type": "Party", "surface_form": "x"}), Reason.UNKNOWN_SLOT),
    "an upper-case slot name": (mutate(("slots", "Counterparty"), {"type": "Party", "surface_form": "x"}), Reason.SLOT_NAME),
    "a slot of another type than the node says": (mutate(("slots", "counterparty"), {"type": "Reference", "value": "x"}), Reason.SLOT_TYPE),
    "a RESOLVED resource without a resolved_ref": (mutate(("slots", "subject_resource"), {"type": "Resource", "status": "RESOLVED"}), Reason.SLOT_INVALID),
    "a resolved_ref on an UNRESOLVED resource": (
        mutate(("slots", "subject_resource"), {"type": "Resource", "status": "UNRESOLVED", "resolved_ref": "test.transaction:TX1"}), Reason.SLOT_INVALID),
    "a window of three instants": (
        mutate(("slots", "when", "window"), ["2026-11-11T00:00:00Z", "2026-11-11T12:00:00Z", "2026-11-11T23:59:59Z"]), Reason.SLOT_INVALID),
    "a window instant that is not a date": (mutate(("slots", "when", "window"), ["2026-11-31T00:00:00Z", "2026-11-11T23:59:59Z"]), Reason.SLOT_INVALID),
    "an outcome the node does not define": (mutate(("desired_outcome_refs",), ["test.outcome.other"]), Reason.OUTCOME_NOT_IN_NODE),
    "a repeated outcome": (mutate(("desired_outcome_refs",), [OUTCOME, OUTCOME]), Reason.OUTCOME_NOT_IN_NODE),
}


@pytest.mark.parametrize("name", sorted(REJECTIONS))
def test_each_rule_refuses_and_names_its_reason(name: str) -> None:
    doc, reason = REJECTIONS[name]
    result = check(doc)
    assert not result.ok and result.intent is None, f"{name}: admitted"
    assert reason in result.reasons, f"{name}: {result.reasons}"
    assert list(result.reasons) == sorted(set(result.reasons), key=lambda r: r.value), "reasons are sorted and unique"


@pytest.mark.parametrize(
    "path",
    [("schema_version",), ("intent_id",), ("version",), ("supersedes",), ("tenant_id",), ("correlation_id",), ("created_at",), ("principal",), ("source",), ("pack",),
     ("intent_node",), ("slots",), ("ambiguity_flags",), ("desired_outcome_refs",), ("status",), ("principal", "principal_id"), ("source", "raw_ref"), ("pack", "pack_id")],
)
def test_a_missing_member_is_a_shape_refusal(path: tuple[str, ...]) -> None:
    assert reasons(without(path)) == {Reason.SHAPE}


@pytest.mark.parametrize("path", [("principal",), ("source",), ("pack",), ("slots", "counterparty"), ("slots", "amount")])
def test_an_unknown_member_at_any_depth_is_a_shape_refusal(path: tuple[str, ...]) -> None:
    doc = proposal()
    node: Any = doc
    for step in path:
        node = node[step]
    node["smuggled"] = "x"
    assert reasons(doc) == {Reason.SHAPE}


def test_a_confidence_that_is_not_an_integer_from_0_to_10000_is_a_shape_refusal() -> None:
    """Ruling A173: a confidence is an integer number of basis points, because a fractional number cannot be hashed or signed. The generated contract model bounds it, so the closed
    shape refuses a fraction, a value over 10000, a negative, a boolean or a string before any rule, and never rounds one (the Go gate refuses by the same contract)."""
    for where in (("source", "asr_confidence"), ("slots", "subject_resource", "confidence")):
        for bad in (0.93, 10001, -1, True, "9300", 1.0e4 + 0.5):
            assert reasons(mutate(where, bad)) == {Reason.SHAPE}, (where, bad)
        for good in (0, 1, 9300, 10000):
            assert reasons(mutate(where, good)) == set(), (where, good)


def test_a_null_where_the_contract_has_none_is_a_shape_refusal() -> None:
    assert reasons(mutate(("intent_node",), None)) == {Reason.SHAPE}
    assert reasons(mutate(("principal", "principal_id"), None)) == {Reason.SHAPE}


def test_what_is_not_one_json_object_is_refused_before_any_rule() -> None:
    g = gate()
    for body, expected in ((b"", Reason.SIZE), (b" ", Reason.NOT_JSON), (b"{", Reason.NOT_JSON), (b"[]", Reason.SHAPE), (b"null", Reason.SHAPE), (b'"x"', Reason.SHAPE),
                           (raw(proposal()) + b" {}", Reason.NOT_JSON), (raw(proposal()) + b"x", Reason.NOT_JSON), (b'{"a": NaN}', Reason.NOT_JSON),
                           (b'{"a": Infinity}', Reason.NOT_JSON), (b'{"a": 1, "a": 2}', Reason.SHAPE)):
        result = g.check(body, CTX)
        assert result.reasons == (expected,), f"{body!r}: {result.reasons}"
        assert result.intent is None


def test_pathological_input_is_refused_not_raised() -> None:
    """Total on any bytes: deep nesting, a bad UTF-8 sequence and an integer too long to read are NOT_JSON, and nothing raises."""
    g = gate(max_bytes=10_000_000)
    for body in (b"[" * 200_000, b'{"a":' * 100_000, b"\xff\xfe\x00", b'{"a": ' + b"9" * 5000 + b"}"):
        result = g.check(body, CTX)
        assert result.reasons == (Reason.NOT_JSON,), (body[:20], result.reasons)
        assert result.intent is None


def test_a_bad_utf8_byte_inside_a_valid_document_is_refused_and_never_replaced() -> None:
    """A lenient decode would turn the byte into U+FFFD and admit a text the model never wrote: that is a repair (REQ-INT-002)."""
    body = raw(proposal()).replace(b'"Ada"', b'"A\xffda"', 1)
    assert b"\xff" in body
    result = gate().check(body, CTX)
    assert result.reasons == (Reason.NOT_JSON,) and result.intent is None


def test_a_number_too_large_for_a_float_is_refused_not_raised() -> None:
    body = raw(proposal()).replace(b'"confidence": 9000', b'"confidence": 1e400', 1)
    assert b"1e400" in body
    result = gate().check(body, CTX)
    assert result.reasons == (Reason.SHAPE,) and result.intent is None


def test_a_supersedes_with_an_enormous_version_is_refused_not_raised() -> None:
    """Review P2 on noema#489: the contract's IntentRef pattern allows any number of digits after the `@`, and Python refuses to read an integer of more than 4,300
    digits with a ValueError. The gate is total: that is a refused chain (SUPERSEDES), never an exception."""
    for digits in (20, 4_300, 4_301, 60_000):  # the test gate reads at most 64 KiB; a larger body is refused earlier as SIZE
        doc = mutate(("version",), 2) | {"supersedes": "int_01J9X3@" + "9" * digits}
        result = check(doc)
        assert result.reasons == (Reason.SUPERSEDES,), (digits, result.reasons)
        assert result.intent is None


def test_the_largest_version_the_gate_reads_is_a_safe_integer() -> None:
    """The version of an intent is at most 2^53 - 1 (JCS, M3); a supersedes that names a version above it is a refused chain."""
    ok = mutate(("version",), 9007199254740991) | {"supersedes": "int_01J9X3@9007199254740990"}
    assert check(ok).ok
    too_big = mutate(("version",), 9007199254740991) | {"supersedes": "int_01J9X3@9007199254740992"}
    assert check(too_big).reasons == (Reason.SUPERSEDES,)


def test_a_model_that_sends_principal_type_or_asserted_by_is_refused_as_a_shape_error() -> None:
    """Ruling A166 item 3: the model's output does not contain principal_type or asserted_by; the core sets them from the session (INV-12). If the model sends them,
    even a value that equals the session's, the gate refuses it as a shape error. No security event is raised on a normal compile."""
    for member, value in (("principal_type", "end_user"), ("asserted_by", "noema-poc-idp"), ("asserted_by", "enterprise_idp"), ("principal_type", UNSET), ("asserted_by", UNSET)):
        doc = proposal()
        doc["principal"][member] = value
        result = check(doc)
        assert result.reasons == (Reason.SHAPE,) and result.intent is None, (member, value, result.reasons)
    doc = proposal()
    doc["principal"].update({"principal_type": "end_user", "asserted_by": "noema-poc-idp"})
    assert check(doc).reasons == (Reason.SHAPE,)


def test_a_proposal_without_them_is_valid_and_carries_the_neutral_markers() -> None:
    result = check(proposal())
    assert result.ok and result.intent is not None
    assert result.intent.principal.principal_type == UNSET and result.intent.principal.asserted_by == UNSET


def test_a_repeated_member_is_refused_even_when_both_values_are_valid() -> None:
    text = raw(proposal()).decode().replace('"version": 1', '"version": 1, "version": 1', 1)
    assert gate().check(text.encode(), CTX).reasons == (Reason.SHAPE,)


def test_the_size_bound_is_the_configured_one() -> None:
    body = raw(proposal())
    assert gate(max_bytes=len(body)).check(body, CTX).ok
    assert gate(max_bytes=len(body) - 1).check(body, CTX).reasons == (Reason.SIZE,)
    with pytest.raises(ValueError):
        SchemaGate(max_bytes=0, pack=Pack())


def test_a_context_that_is_not_in_order_is_refused_and_not_blamed_on_the_model() -> None:
    for bad in (Context("", "corr_c1", "usr_88213", "ses_7a1b"), Context("ten_a1", "bad", "usr_88213", "ses_7a1b"), Context("ten_a1", "corr_c1", "x", "ses_7a1b"),
                Context("ten_a1", "corr_c1", "usr_88213", "")):
        assert check(proposal(), bad).reasons == (Reason.CONTEXT_INVALID,)


def test_the_gate_never_repairs_and_never_mutates_its_input() -> None:
    doc = proposal()
    doc["ambiguity_flags"] = ["lowercase_flag"]
    before = copy.deepcopy(doc)
    result = check(doc)
    assert not result.ok and result.intent is None
    assert doc == before
    assert reasons(doc) == {Reason.SHAPE}, "a flag the contract's pattern refuses is a refusal, never normalised"


def test_the_signature_slot_of_a_validated_intent_is_the_unsigned_marker_and_never_the_models() -> None:
    result = check(proposal())
    assert result.intent is not None
    assert (result.intent.sig.key_id, result.intent.sig.value) == ("unsigned", "unsigned")
    assert isinstance(result.intent, IntentObjectV3)


def test_no_reason_carries_text_of_the_object() -> None:
    doc = proposal()
    doc["slots"]["counterparty"]["surface_form"] = "SECRET-NAME"
    doc["intent_node"] = "test.intent.SECRET"
    for result in (check(doc), check(mutate(("slots", "mystery"), {"type": "Party", "surface_form": "SECRET-NAME"}))):
        assert "SECRET" not in repr(result)


@settings(max_examples=10_000, derandomize=True, deadline=None, suppress_health_check=[HealthCheck.too_slow])
@given(
    where=st.sampled_from([(), ("principal",), ("source",), ("pack",), ("slots", "subject_resource"), ("slots", "amount"), ("slots", "when"), ("slots", "counterparty"),
                           ("slots", "requested_remedy")]),
    name=st.text(min_size=1, max_size=12).filter(lambda s: s not in proposal() and s != "sig"),  # `sig` is a contract member: a model-set one has its own reason (SIG_SET_BY_MODEL)
    value=st.one_of(st.none(), st.booleans(), st.integers(), st.text(max_size=8)),
)
def test_fuzz_an_unknown_member_anywhere_is_always_refused(where: tuple[str, ...], name: str, value: Any) -> None:
    doc = proposal()
    node: Any = doc
    for step in where:
        node = node[step]
    if name in node:
        return
    node[name] = value
    result = check(doc)
    assert not result.ok and result.reasons == (Reason.SHAPE,)


@settings(max_examples=2_000, derandomize=True, deadline=None)
@given(st.binary(max_size=300))
def test_the_gate_is_total_and_deterministic_on_any_bytes(body: bytes) -> None:
    first = gate().check(body, CTX)
    assert first == gate().check(body, CTX)
    assert (first.ok and first.intent is not None and first.reasons == ()) or (not first.ok and first.intent is None and len(first.reasons) >= 1)
