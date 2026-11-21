"""Checks for conversation-response.v1, the success answer of POST /v1/conversations (CON-07, noema#807; ruling A234; PERD 39.2; REQ-API-001, REQ-UI-001, REQ-SEC-003, INV-12; AGENT-13, AGENT-15).

The answer is the PERD 39.2 shape `{investigation_id, conversation_ref, state, stream_url}` plus `correlation_id` (A234, interim, relayed, not yet committed). The core derives the first two from
the correlation ID it minted (`inv_` and its ULID), so a retry gets the same values (A168); `state` is `understanding`; `stream_url` is the route of the events of that investigation. The
Go test `services/core/cmd/core/conversations_contract_test.go` holds the handler's real answers against this schema. All values are SYNTHETIC.

Each clause is held twice: by the fixtures that `validate.py` runs and here, one clause at a time, so that a mutant that weakens exactly one clause fails exactly the test of that clause.
"""
import json
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "codegen"))
from anchors import fully_anchored  # noqa: E402
from validate import FIXTURES, conversation_response_mismatches, load_schemas, load_strict, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
NAME = "conversation-response.v1.json"
ULID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
MEMBERS = {"investigation_id", "conversation_ref", "state", "stream_url", "correlation_id"}
# PERD 32.3 and the ID rule of the Record guard (`typedIDs`, A317 to A328): `<prefix>_<ULID>`, the ULID's first character 0 to 7, the rest Crockford base 32, and a letter somewhere in it, so
# that a customer-facing ID can never be a run of digits (AGENT-13). A ULID may hold a run of five digits and still be an ID; that is why a free-text digit-run pattern is not used here.
ULID_BODY = r"(?=[0-9]*[A-Za-z])[0-7][0-9A-HJKMNP-TV-Z]{25}"
INVESTIGATION_ID = rf"^inv_{ULID_BODY}$"
CORRELATION_ID = rf"^corr_{ULID_BODY}$"
STREAM_URL = rf"^/v1/conversations/inv_{ULID_BODY}/events$"
# The UI limit of one response or frame (A446).
UI_LIMIT_BYTES = 65536
IDENTITY_IDEAS = {"tenant_id", "principal_id", "principal", "principal_ref", "session", "session_id", "role", "roles", "user", "user_id", "token", "sub", "iss"}


def schema():
    return SCHEMAS[NAME]


def errors(doc):
    return list(validator_for("conversation-response", SCHEMAS, REGISTRY).iter_errors(doc))


def ok(doc):
    return not errors(doc)


def good(**kw):
    doc = {
        "investigation_id": f"inv_{ULID}",
        "conversation_ref": f"inv_{ULID}",
        "state": "understanding",
        "stream_url": f"/v1/conversations/inv_{ULID}/events",
        "correlation_id": f"corr_{ULID}",
    }
    doc.update(kw)
    return doc


class Members(unittest.TestCase):
    def test_the_members_are_the_perd_shape_plus_the_correlation_id_and_no_other(self):
        self.assertEqual(set(schema()["properties"]), MEMBERS)
        self.assertEqual(set(schema()["required"]), MEMBERS)

    def test_the_answer_is_closed(self):
        self.assertIs(schema()["additionalProperties"], False)
        self.assertFalse(ok(good(event_id="recev_" + ULID)))

    def test_every_member_is_required(self):
        for name in sorted(MEMBERS):
            doc = good()
            del doc[name]
            with self.subTest(name):
                self.assertFalse(ok(doc))

    def test_the_answer_names_no_identity_and_no_internal_object(self):
        self.assertEqual(set(schema()["properties"]) & IDENTITY_IDEAS, set())
        for name in sorted(IDENTITY_IDEAS):
            with self.subTest(name):
                self.assertFalse(ok(good(**{name: "x"})))

    def test_a_valid_answer_validates(self):
        self.assertTrue(ok(good()))


class CrossMemberRelations(unittest.TestCase):
    """The schema checks each member alone. The relations between members are the CONSUMER's to check (FE integration read, noema#815): the schema accepts an answer whose
    members are individually valid but name different investigations, so no reader may take shape validation as proof of consistency. The three checks the consumer makes before
    it polls or shows any state: investigation_id equals conversation_ref; stream_url is `/v1/conversations/<investigation_id>/events`; correlation_id is `corr_` and the ULID of
    investigation_id. This test pins that the schema does NOT state them (so nobody relies on it) and that the consistent answer does satisfy all three."""

    OTHER = "01ARZ3NDEKTSV4RRFFQ69G5FAW"

    def test_the_schema_accepts_an_answer_whose_members_name_different_investigations(self):
        doc = good(investigation_id=f"inv_{ULID}", conversation_ref=f"inv_{self.OTHER}", stream_url=f"/v1/conversations/inv_{self.OTHER}/events", correlation_id=f"corr_{self.OTHER}")
        self.assertTrue(ok(doc), "the schema now states a cross-member relation: update the README and the consumer checks")
        self.assertNotEqual(doc["investigation_id"], doc["conversation_ref"])

    def test_the_consistent_answer_satisfies_the_three_consumer_checks(self):
        doc = good()
        ulid = doc["investigation_id"].removeprefix("inv_")
        self.assertEqual(doc["investigation_id"], doc["conversation_ref"])
        self.assertEqual(doc["stream_url"], f"/v1/conversations/{doc['investigation_id']}/events")
        self.assertEqual(doc["correlation_id"], f"corr_{ulid}")


class RelationCheck(unittest.TestCase):
    """The relations a schema cannot state, checked by `validate.py` (`conversation_response_mismatches`; A479, the follow-up of noema#815): investigation_id equals conversation_ref;
    stream_url is `/v1/conversations/<investigation_id>/events`; correlation_id is `corr_` and the ULID of investigation_id. Each is held by its own test, so a mutant that drops one
    check fails exactly its test. The core's writer refuses the same answers (`cmd/core/conversations_relations_test.go`) and reads the same fixtures. All values are SYNTHETIC."""

    OTHER = "01ARZ3NDEKTSV4RRFFQ69G5FAW"

    def test_the_consistent_answer_has_no_mismatch(self):
        self.assertEqual(conversation_response_mismatches(good()), [])

    def test_a_conversation_ref_that_is_not_the_investigation_id_is_a_mismatch(self):
        doc = good(conversation_ref=f"inv_{self.OTHER}")
        self.assertTrue(ok(doc))
        self.assertEqual(conversation_response_mismatches(doc), ["conversation_ref"])

    def test_a_stream_url_that_names_another_investigation_is_a_mismatch(self):
        doc = good(stream_url=f"/v1/conversations/inv_{self.OTHER}/events")
        self.assertTrue(ok(doc))
        self.assertEqual(conversation_response_mismatches(doc), ["stream_url"])

    def test_a_correlation_id_of_another_ulid_is_a_mismatch(self):
        doc = good(correlation_id=f"corr_{self.OTHER}")
        self.assertTrue(ok(doc))
        self.assertEqual(conversation_response_mismatches(doc), ["correlation_id"])

    def test_an_investigation_id_that_differs_from_the_three_others_names_all_three(self):
        doc = good(investigation_id=f"inv_{self.OTHER}")
        self.assertTrue(ok(doc))
        self.assertEqual(conversation_response_mismatches(doc), ["conversation_ref", "stream_url", "correlation_id"])

    def test_a_document_that_is_not_an_object_or_lacks_a_member_is_refused_not_crashed(self):
        for bad in (None, [], "x", 5, {}, {"investigation_id": f"inv_{ULID}"}, good(stream_url=None), good(investigation_id=5)):
            with self.subTest(repr(bad)):
                self.assertNotEqual(conversation_response_mismatches(bad), [])

    def test_the_fixtures_hold_the_relations(self):
        for path in sorted((FIXTURES / "valid" / "conversation-response").glob("*.json")):
            with self.subTest(path.name):
                self.assertEqual(conversation_response_mismatches(load_strict(path)), [])
        expectations = load_strict(FIXTURES / "invalid" / "conversation-response" / "expectations.json")
        named = sorted(name for name, why in expectations.items() if why == "conversation_id_mismatch")
        self.assertEqual(len(named), 4, named)
        for name in named:
            doc = load_strict(FIXTURES / "invalid" / "conversation-response" / name)
            with self.subTest(name):
                self.assertTrue(ok(doc), "a relation fixture must be well formed: the schema alone accepts it")
                self.assertNotEqual(conversation_response_mismatches(doc), [])


class State(unittest.TestCase):
    def test_it_is_the_one_public_stage_a234_names(self):
        self.assertEqual(schema()["properties"]["state"].get("const"), "understanding")

    def test_no_other_value_validates(self):
        for bad in ("working", "answered", "UNDERSTANDING", "", None, 1):
            with self.subTest(repr(bad)):
                self.assertFalse(ok(good(state=bad)))

    def test_it_is_a_value_of_the_public_stage_of_the_investigation_route(self):
        stages = SCHEMAS["investigation-response.v1.json"]["$defs"]["PublicStage"]["enum"]
        self.assertIn(schema()["properties"]["state"].get("const"), stages)


class Ids(unittest.TestCase):
    def test_the_patterns_are_exactly_these(self):
        props = schema()["properties"]
        self.assertEqual(props["investigation_id"].get("pattern"), INVESTIGATION_ID)
        self.assertEqual(props["conversation_ref"].get("pattern"), INVESTIGATION_ID)
        self.assertEqual(props["correlation_id"].get("pattern"), CORRELATION_ID)
        self.assertEqual(props["stream_url"].get("pattern"), STREAM_URL)

    def test_a_member_is_a_string_with_a_pattern_and_never_a_free_text(self):
        for name in sorted(MEMBERS - {"state"}):
            with self.subTest(name):
                self.assertEqual(schema()["properties"][name]["type"], "string")
                self.assertIn("pattern", schema()["properties"][name])

    def test_a_ulid_with_a_run_of_digits_and_a_letter_is_an_id(self):
        u = "01234567890123456789ABCDEF"
        self.assertTrue(ok(good(investigation_id=f"inv_{u}", conversation_ref=f"inv_{u}", correlation_id=f"corr_{u}", stream_url=f"/v1/conversations/inv_{u}/events")))

    def test_the_first_character_of_the_ulid_is_0_to_7(self):
        self.assertTrue(ok(good(investigation_id="inv_7" + ULID[1:])))
        self.assertFalse(ok(good(investigation_id="inv_8" + ULID[1:])))
        self.assertFalse(ok(good(correlation_id="corr_8" + ULID[1:])))
        self.assertFalse(ok(good(stream_url="/v1/conversations/inv_8" + ULID[1:] + "/events")))

    def test_an_id_of_digits_only_is_refused_everywhere(self):
        digits = "0" * 26
        self.assertFalse(ok(good(investigation_id=f"inv_{digits}")))
        self.assertFalse(ok(good(conversation_ref=f"inv_{digits}")))
        self.assertFalse(ok(good(correlation_id=f"corr_{digits}")))
        self.assertFalse(ok(good(stream_url=f"/v1/conversations/inv_{digits}/events")))

    def test_the_prefixes(self):
        self.assertFalse(ok(good(investigation_id=f"corr_{ULID}")))
        self.assertFalse(ok(good(conversation_ref=f"conv_{ULID}")))
        self.assertFalse(ok(good(correlation_id=f"inv_{ULID}")))

    def test_the_length_and_the_alphabet_of_the_ulid(self):
        for bad in (ULID[:-1], ULID + "A", ULID.lower(), ULID.replace("A", "U", 1), ULID.replace("A", "I", 1), ULID.replace("A", "L", 1), ULID.replace("A", "O", 1)):
            with self.subTest(bad):
                self.assertFalse(ok(good(investigation_id=f"inv_{bad}")))

    def test_a_trailing_newline_is_not_an_id(self):
        self.assertFalse(ok(good(investigation_id=f"inv_{ULID}\n")))
        self.assertFalse(ok(good(stream_url=f"/v1/conversations/inv_{ULID}/events\n")))

    def test_a_non_string_is_refused(self):
        for name in ("investigation_id", "conversation_ref", "correlation_id", "stream_url"):
            for bad in (None, 5, ["x"], {"a": 1}):
                with self.subTest(name=name, bad=repr(bad)):
                    self.assertFalse(ok(good(**{name: bad})))

    def test_no_member_can_hold_a_run_of_five_digits_that_is_not_inside_a_ulid(self):
        # The one string a member can hold is an ID or the route made of one; each of them is a prefix, a ULID and a fixed suffix. Enumerate what is around the ULID.
        for bad in (f"inv_{ULID}12345", f"12345inv_{ULID}", f"inv_12345_{ULID}"):
            with self.subTest(bad):
                self.assertFalse(ok(good(investigation_id=bad)))


class StreamUrl(unittest.TestCase):
    def test_it_is_the_events_route_of_the_investigation_and_nothing_else(self):
        self.assertTrue(ok(good()))
        for bad in (
            f"https://example.invalid/v1/conversations/inv_{ULID}/events",
            f"//example.invalid/v1/conversations/inv_{ULID}/events",
            f"/v1/conversations/inv_{ULID}/messages",
            f"/v1/conversations/inv_{ULID}/events?token=abc",
            f"/v1/conversations/inv_{ULID}/events#x",
            f"/v1/conversations/inv_{ULID}/events/",
            f"/v1/conversations/corr_{ULID}/events",
            f"/v2/conversations/inv_{ULID}/events",
            f"v1/conversations/inv_{ULID}/events",
            "",
        ):
            with self.subTest(bad):
                self.assertFalse(ok(good(stream_url=bad)))

    def test_the_route_is_the_one_of_the_stream_in_perd_39_2(self):
        # sse-event.v1 describes GET /v1/conversations/{conversation_ref}/events; the URL is that route with the conversation_ref filled in.
        self.assertIn("GET /v1/conversations/{conversation_ref}/events", SCHEMAS["sse-event.v1.json"]["description"])


class Size(unittest.TestCase):
    def test_the_largest_answer_the_schema_admits_is_far_below_the_ui_limit(self):
        # A446: no single response or frame above 65,536 bytes. Every member is a fixed-form string, so the largest answer is that of the longest ID and a four-byte escape per character is a
        # safe upper bound of its JSON text. The schema must not admit a member of an unbounded size.
        longest = {
            "investigation_id": "inv_" + "Z" * 26,
            "conversation_ref": "inv_" + "Z" * 26,
            "state": "understanding",
            "stream_url": "/v1/conversations/inv_" + "Z" * 26 + "/events",
            "correlation_id": "corr_" + "Z" * 26,
        }
        size = len(json.dumps(longest, separators=(",", ":")).encode("utf-8"))
        self.assertLess(size, 512)
        self.assertLess(size, UI_LIMIT_BYTES)

    def test_every_pattern_fixes_the_length(self):
        # A pattern with an unbounded repetition would let a member grow past the limit; the only repetitions allowed are the fixed {25} and the digit scan of the look-ahead.
        for name in sorted(MEMBERS - {"state"}):
            pattern = schema()["properties"][name].get("pattern", "")
            body = pattern.replace("(?=[0-9]*[A-Za-z])", "")
            with self.subTest(name):
                self.assertNotIn("*", body)
                self.assertNotIn("+", body)
                self.assertNotIn("{25,", body)

    def test_a_longer_member_is_refused(self):
        self.assertFalse(ok(good(investigation_id="inv_" + ULID * 2)))
        self.assertFalse(ok(good(stream_url="/v1/conversations/inv_" + ULID * 2 + "/events")))

    def test_the_description_states_the_limit(self):
        self.assertIn("65,536", schema()["description"])


class Fixtures(unittest.TestCase):
    def test_the_digit_run_guard_is_the_one_of_the_record_guard(self):
        # The Record guard's ID branch (tools/contracts/digit_run.py `_ID`) is the same ULID body; this schema does not restate a different ID rule.
        text = (Path(__file__).resolve().parent / "digit_run.py").read_text(encoding="utf-8")
        self.assertIn(r"[0-7][0-9A-HJKMNP-TV-Z]{25}", text)
        self.assertIn(r"(?=[0-9]*[A-Za-z])", text)
        self.assertTrue(re.search(fully_anchored(INVESTIGATION_ID), f"inv_{ULID}"))


if __name__ == "__main__":
    unittest.main()
