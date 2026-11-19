"""Checks for message-request.v1, the body of a customer's message (PERD 39.2; REQ-API-001, REQ-INP-001, REQ-INP-003, REQ-AMB-005, REQ-UI-002, INV-12, INV-02; A275; AGENT-15).

The body of `POST /v1/conversations` (the first message) and of `POST /v1/conversations/{conversation_ref}/messages` (a follow-up: a new turn, or the answer to a
clarification). PERD 39.2 lists `{request_id, text, locale_hint?}` for the first and `{request_id, text?, option_id?, clarification_id?}` for the second. "None of these" travels
as `option_id`, which then carries the core-minted `none_option_id` of the clarification (A275); the UI never invents a sentinel, so a boolean or a flag for it is refused here
(`additionalProperties`). The body names no identity (INV-12): the Go test `services/core/edge/session/messagerequest_test.go` holds the schema's member names against the
edge's body guard. All values are SYNTHETIC.

Each clause is held twice: by the fixtures that `validate.py` runs (valid and invalid, each with the keyword that refuses it) and here, one clause at a time, so that a mutant
that weakens exactly one clause fails exactly the test of that clause.
"""
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "codegen"))
from anchors import fully_anchored  # noqa: E402
from validate import load_schemas, make_registry, validator_for  # noqa: E402

import re  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
NAME = "message-request.v1.json"
RID = "7c1d4e52-9b3a-4f60-8e17-2a5c6d0b9f34"
CLR = "clr_01J9X5"
# PERD 39.2, the members of the two bodies together, and no other.
MEMBERS = {"request_id", "text", "locale_hint", "option_id", "clarification_id"}
# Identity and session members: none may be a member of the body (INV-12, A1). The edge's list is `identityKeys` in services/core/edge/session/bodyidentity.go; the Go test
# reads the schema against that list. This is a second, independent statement of the idea, not a copy of the list.
IDENTITY_IDEAS = {"tenant_id", "principal_id", "principal", "principal_ref", "session", "session_id", "role", "roles", "user", "user_id", "token", "correlation_id", "sub", "iss"}


def schema():
    return SCHEMAS[NAME]


def errors(doc):
    return list(validator_for("message-request", SCHEMAS, REGISTRY).iter_errors(doc))


def ok(doc):
    return not errors(doc)


def base(**kw):
    doc = {"request_id": RID}
    doc.update(kw)
    return doc


class Members(unittest.TestCase):
    def test_the_members_are_exactly_those_of_perd_39_2(self):
        self.assertEqual(set(schema()["properties"]), MEMBERS)

    def test_the_body_is_closed(self):
        self.assertIs(schema()["additionalProperties"], False)
        self.assertFalse(ok(base(text="Hello", channel="text")))

    def test_none_of_these_has_no_flag_of_its_own(self):
        # A275: "None of these" is an option_id, the core's none_option_id. A boolean or any other member would be a sentinel the UI invented.
        self.assertFalse(ok(base(clarification_id=CLR, none_of_these=True)))
        self.assertFalse(ok(base(clarification_id=CLR, none=True)))

    def test_the_body_names_no_identity(self):
        self.assertEqual(set(schema()["properties"]) & IDENTITY_IDEAS, set())
        for name in sorted(IDENTITY_IDEAS):
            with self.subTest(name):
                self.assertFalse(ok(base(text="Hello", **{name: "x"})))


class Required(unittest.TestCase):
    def test_only_the_request_id_is_always_required(self):
        self.assertEqual(schema()["required"], ["request_id"])
        self.assertFalse(ok({"text": "Hello"}))


class RequestId(unittest.TestCase):
    def test_it_is_the_clients_lower_case_uuid_by_reference(self):
        self.assertEqual(schema()["properties"]["request_id"]["$ref"], "https://contracts.noema.invalid/common.v1.json#/$defs/ClientRequestId")
        self.assertTrue(ok(base(text="Hello")))
        for bad in (RID.upper(), "req_01J9X3AB", RID[:-1], RID + "0", 7, None, ""):
            with self.subTest(bad):
                self.assertFalse(ok({"request_id": bad, "text": "Hello"}))


class Text(unittest.TestCase):
    def test_its_bounds_are_those_of_the_canonical_input_it_becomes(self):
        # REQ-INP-001: 1 to 2,000 characters. Reused from canonical-input.v2, not restated.
        mine, theirs = schema()["properties"]["text"], SCHEMAS["canonical-input.v2.json"]["properties"]["text"]
        self.assertEqual((mine["minLength"], mine["maxLength"]), (theirs["minLength"], theirs["maxLength"]))
        self.assertEqual((mine["minLength"], mine["maxLength"]), (1, 2000))

    def test_the_boundaries(self):
        self.assertTrue(ok(base(text="a")))
        self.assertTrue(ok(base(text="a" * 2000)))
        self.assertFalse(ok(base(text="")))
        self.assertFalse(ok(base(text="a" * 2001)))

    def test_length_counts_code_points(self):
        self.assertTrue(ok(base(text="\U0001F600" * 2000)))
        self.assertFalse(ok(base(text="\U0001F600" * 2001)))

    def test_it_must_be_a_string(self):
        for bad in (5, None, True, ["a"], {"a": 1}):
            with self.subTest(repr(bad)):
                self.assertFalse(ok(base(text=bad)))

    def test_the_customers_own_digits_are_allowed(self):
        # Inbound text is the customer's own words ("I was debited 50000 naira"); the Gateway masks it (REQ-SEC-003) and nothing here echoes it. The digit-run rule of the
        # outbound surfaces (A154) would refuse the commonest report a customer makes, so the contract carries none, as canonical-input.v2 carries none.
        self.assertTrue(ok(base(text="I was debited 50000 naira, account 0123456789")))


class LocaleHint(unittest.TestCase):
    def test_its_pattern_is_that_of_the_canonical_input_it_becomes(self):
        mine, theirs = schema()["properties"]["locale_hint"], SCHEMAS["canonical-input.v2.json"]["properties"]["locale_hint"]
        self.assertEqual(mine["pattern"], theirs["pattern"])
        self.assertEqual(mine["type"], "string")

    def test_a_tag_validates_and_a_sentence_does_not(self):
        for good in ("en", "en-NG", "ha-NG", "yo", "pcm-NG"):
            self.assertTrue(ok(base(text="Hello", locale_hint=good)), good)
        for bad in ("English (Nigeria)", "EN", "e", "en_NG", "", "en-", 5, None):
            self.assertFalse(ok(base(text="Hello", locale_hint=bad)), repr(bad))


class OptionId(unittest.TestCase):
    OPTION_ID = r"^(?![a-z_]*(?:[0-9][a-z_]*){5})[a-z0-9_]{1,32}$"

    def test_its_pattern_is_that_of_an_option_on_the_overlay_card(self):
        # The UI submits the id it was shown (REQ-UI-002), so the two must accept the same strings, the core's none_option_id among them (A275).
        card = SCHEMAS["overlay-card.v1.json"]["$defs"]["ClarificationCard"]["properties"]["options"]["items"]["properties"]["option_id"]
        self.assertEqual(schema()["properties"]["option_id"]["pattern"], card["pattern"])
        self.assertEqual(card["pattern"], self.OPTION_ID)

    def test_shapes(self):
        for good in ("opt_2", "a", "a" * 32, "opt_none_k2f8", "o1_2_3_4"):
            self.assertTrue(ok(base(option_id=good, clarification_id=CLR)), good)
        for bad in ("", "a" * 33, "OPT_2", "opt-2", "opt 2", "opt_12345", "1_2_3_4_5", "opt_2\n", "١٢٣٤٥", 3, None):
            self.assertFalse(ok(base(option_id=bad, clarification_id=CLR)), repr(bad))

    def test_the_pattern_is_whole_string(self):
        self.assertIsNone(re.search(fully_anchored(self.OPTION_ID), "opt_2\n"))


class OptionNeedsItsClarification(unittest.TestCase):
    def test_an_option_without_the_clarification_it_answers_is_refused(self):
        # A275 and the Frontend Engineer's recommendation on noema#656: the representation is tied to the server-issued clarification id.
        self.assertTrue(ok(base(option_id="opt_2", clarification_id=CLR)))
        self.assertFalse(ok(base(option_id="opt_2")))
        self.assertFalse(ok(base(option_id="opt_none_k2f8")))

    def test_the_clarification_id_is_a_clarification_id(self):
        self.assertEqual(schema()["properties"]["clarification_id"]["$ref"], "https://contracts.noema.invalid/common.v1.json#/$defs/ClarificationId")
        for bad in ("cns_01J9X3", "clr_", "CLR_01", "clr_" + "a" * 33, "", None, 3):
            self.assertFalse(ok(base(option_id="opt_2", clarification_id=bad)), repr(bad))


class WhatAMessageSays(unittest.TestCase):
    """The clauses PERD 39.2 does not state (reported to the Architect, nothing here is the PERD's text): the safer reading, which a later version can loosen without breaking anyone."""

    def test_a_message_says_something(self):
        for doc in (base(), base(clarification_id=CLR), base(locale_hint="en-NG")):
            self.assertFalse(ok(doc), doc)
        self.assertTrue(ok(base(text="Hello")))
        self.assertTrue(ok(base(option_id="opt_2", clarification_id=CLR)))

    def test_a_click_and_text_do_not_travel_together(self):
        self.assertFalse(ok(base(text="The first one", option_id="opt_2", clarification_id=CLR)))

    def test_free_text_may_answer_a_clarification(self):
        # REQ-AMB-005: the answer may be an option_id (a click) or free text.
        self.assertTrue(ok(base(text="The 8 o'clock one.", clarification_id=CLR)))


class Description(unittest.TestCase):
    def test_it_cites_what_it_serves(self):
        text = schema()["description"]
        for cite in ("PERD", "39.2", "REQ-API-001", "A275", "INV-12", "AGENT-15"):
            self.assertIn(cite, text)

    def test_it_is_a_new_file_beside_no_other(self):
        self.assertEqual(schema()["$id"], "https://contracts.noema.invalid/message-request.v1.json")
        self.assertEqual([n for n in SCHEMAS if n.startswith("message-request")], [NAME])


class Fixtures(unittest.TestCase):
    def test_the_valid_fixtures_cover_every_flow_of_perd_39_2(self):
        root = Path(__file__).resolve().parents[2] / "contracts" / "fixtures" / "valid" / "message-request"
        docs = [json.loads(p.read_text(encoding="utf-8")) for p in sorted(root.glob("*.json"))]
        shapes = {tuple(sorted(d)) for d in docs}
        for flow in (("request_id", "text"), ("locale_hint", "request_id", "text"), ("clarification_id", "option_id", "request_id"), ("clarification_id", "request_id", "text")):
            self.assertIn(flow, shapes)


if __name__ == "__main__":
    unittest.main()
