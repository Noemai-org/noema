"""The public API response and SSE contracts (PERD §39.2, §40.1, §13; REQ-API-001, REQ-UI-001, REQ-UI-002, REQ-SEC-003; AGENT-15).

investigation-response.v1, customer-answer.v1, overlay-card.v1 and sse-event.v1 are new files: nothing earlier changes. These tests hold what the schemas alone cannot:
the state list is the orchestrator's, the five event kinds are PERD §39.2's, every free-text member carries the digit-run pattern (A154), and no member of the
'Never shown' list (PERD §40.1) or of an internal object can be added by accident. Names are SYNTHETIC test data.
"""
import json
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = load_schemas()
NAMES = ("investigation-response.v1.json", "customer-answer.v1.json", "overlay-card.v1.json", "sse-event.v1.json")
DIGIT_RUN = r"^(?![\s\S]*[0-9]{5})[\s\S]*$"
# PERD §39.2: the five event kinds of the stream.
EVENT_KINDS = {"progress", "clarification", "confirmation", "answer", "error"}
# PERD §40.1 "Never shown", and the internal members of clarification.v1 and confirmation-request.v1 that the overlay projection leaves out.
NEVER_MEMBERS = {
    "confidence", "model_confidence", "reason_codes", "reason_code", "stack_trace", "stack", "threshold", "policy_threshold", "evidence", "evidence_payload", "payload",
    "binding_hash", "sig", "tenant_id", "decision_id", "candidate_set_id", "candidate_set_version", "question_key", "slot", "principal_id", "account_number",
}
# Strings that are not free text, each with its own pattern: an option key.
KEYED_STRINGS = {"option_id", "policy_version"}


def walk(node, path=()):
    """Every (path, schema) pair below node, through properties, items and $defs."""
    if isinstance(node, dict):
        yield path, node
        for key in ("properties", "$defs"):
            for name, sub in (node.get(key) or {}).items():
                yield from walk(sub, path + (name,))
        if isinstance(node.get("items"), dict):
            yield from walk(node["items"], path + ("[]",))
        for key in ("oneOf", "anyOf", "allOf"):
            for i, sub in enumerate(node.get(key) or []):
                yield from walk(sub, path + (f"{key}{i}",))


class PublicApi(unittest.TestCase):
    def test_the_investigation_states_are_the_orchestrators(self):
        text = (ROOT / "services" / "core" / "orchestrator" / "transitions.yaml").read_text(encoding="utf-8")
        listed = re.search(r"^states:\s*\[(.*?)\]", text, flags=re.M).group(1).split(",")
        states = SCHEMAS["investigation-response.v1.json"]["$defs"]["InvestigationState"]["enum"]
        self.assertEqual(states, [s.strip() for s in listed])
        self.assertEqual(len(states), len(set(states)))

    def test_the_stream_has_exactly_the_five_event_kinds_of_perd_39_2(self):
        defs = SCHEMAS["sse-event.v1.json"]["$defs"]
        self.assertEqual({d["properties"]["event"]["const"] for d in defs.values()}, EVENT_KINDS)
        self.assertEqual(len(SCHEMAS["sse-event.v1.json"]["oneOf"]), len(EVENT_KINDS))

    def test_the_overlay_has_exactly_the_two_cards_and_each_is_bound_to_one_id(self):
        defs = SCHEMAS["overlay-card.v1.json"]["$defs"]
        self.assertEqual({d["properties"]["card_type"]["const"] for d in defs.values()}, {"CLARIFICATION", "CONFIRMATION"})
        self.assertIn("clarification_id", defs["ClarificationCard"]["required"])
        self.assertIn("confirmation_request_id", defs["ConfirmationCard"]["required"])
        self.assertNotIn("confirmation_request_id", defs["ClarificationCard"]["properties"])
        self.assertNotIn("clarification_id", defs["ConfirmationCard"]["properties"])

    def test_every_free_text_member_carries_the_digit_run_pattern(self):
        for name in NAMES:
            for path, node in walk(SCHEMAS[name]):
                if node.get("type") == "string" and "enum" not in node and "const" not in node and "$ref" not in node and path:
                    leaf = path[-1]
                    pattern = node.get("pattern")
                    ok = pattern == DIGIT_RUN or (leaf in KEYED_STRINGS and pattern)
                    self.assertTrue(ok, f"{name}: {'/'.join(path)} is a string with no digit-run pattern")
                    self.assertIn("maxLength", node, f"{name}: {'/'.join(path)} is a free-text string with no maxLength") if pattern == DIGIT_RUN else None

    def test_no_member_of_the_never_shown_list_or_an_internal_object_exists(self):
        for name in NAMES:
            for path, node in walk(SCHEMAS[name]):
                for member in node.get("properties") or {}:
                    self.assertNotIn(member, NEVER_MEMBERS, f"{name}: {'/'.join(path)} has the member {member}")

    def test_every_object_refuses_unknown_members(self):
        for name in NAMES:
            for path, node in walk(SCHEMAS[name]):
                if node.get("type") == "object" and "properties" in node:
                    self.assertIs(node.get("additionalProperties"), False, f"{name}: {'/'.join(path)} accepts unknown members")

    def test_lists_are_bounded(self):
        for name in NAMES:
            for path, node in walk(SCHEMAS[name]):
                if node.get("type") == "array":
                    self.assertIn("maxItems", node, f"{name}: {'/'.join(path)} is an unbounded list")

    def test_the_error_event_uses_the_errors_v2_body_and_no_new_code(self):
        error = SCHEMAS["sse-event.v1.json"]["$defs"]["Error"]["properties"]["error"]
        self.assertEqual(error["$ref"], "https://contracts.noema.invalid/errors.v2.json#/$defs/ErrorBody")

    def test_the_earlier_contracts_are_not_edited(self):
        for name in ("clarification.v1.json", "confirmation-request.v1.json", "errors.v2.json", "noema-resolution.v1.json"):
            self.assertIn(name, SCHEMAS)  # the new set only references them; the drift and fixture checks hold their content


if __name__ == "__main__":
    unittest.main()
