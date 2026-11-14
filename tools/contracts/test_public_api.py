"""The public API response and SSE contracts (PERD §39.2, §40.1, §13; REQ-API-001, REQ-UI-001, REQ-UI-002, REQ-SEC-003; AGENT-15).

investigation-response.v1, customer-answer.v1, overlay-card.v1 and sse-event.v1 are new files: nothing earlier changes. These tests hold what the schemas alone cannot:
the client sees a small public stage and never an internal state (A187), the five event kinds are PERD §39.2's, every free-text member carries the digit-run pattern
of every script (A154, A184, A194), the public error body has one not-found shape (A187), and no member of the 'Never shown' list (PERD §40.1) or of an internal object can
be added by accident. Names are SYNTHETIC test data.

The digit-run pattern is built here from the Unicode decimal digits (category Nd, Python 3.13: Unicode 15.1) and the separator set of the model redactor (noema#468,
`noema_model/platform/redaction`), written as literal characters so that Python's `re`, Go's RE2, Pydantic's Rust regex and a JavaScript RegExp with the `u` flag read the
same pattern (the digit shorthand is ASCII-only in the last three and Unicode in the first; the Unicode property class for decimal digits is not in Python's `re`; unicode escapes are not in RE2). A test recomputes the class, so a
Python upgrade that adds digits fails here instead of leaving a digit unmasked.
"""
import json
import re
import sys
import unicodedata
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "codegen"))
from anchors import fully_anchored  # noqa: E402
from validate import load_schemas  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = load_schemas()
NAMES = ("investigation-response.v1.json", "customer-answer.v1.json", "overlay-card.v1.json", "sse-event.v1.json", "errors.v3.json")


def _ranges(predicate) -> list[tuple[int, int]]:
    out: list[tuple[int, int]] = []
    for cp in range(sys.maxunicode + 1):
        if predicate(cp):
            if out and out[-1][1] == cp - 1:
                out[-1] = (out[-1][0], cp)
            else:
                out.append((cp, cp))
    return out


def _class(ranges: list[tuple[int, int]], negate: bool = False) -> str:
    body = "".join(chr(a) if a == b else f"{chr(a)}-{chr(b)}" for a, b in ranges)
    return "[" + ("^" if negate else "") + body + "]"


ND = _ranges(lambda cp: unicodedata.category(chr(cp)) == "Nd")
# The separators the model redactor joins digits across: spaces (U+0020, U+00A0, U+1680, U+2000..U+200A, U+202F, U+205F, U+3000) and dashes (hyphen, U+2010..U+2015,
# U+2212, U+FE58, U+FE63, U+FF0D). The hyphen is escaped inside the class.
SEPARATORS = " \u00a0\u1680\u2000-\u200a\u202f\u205f\u3000\\-\u2010-\u2015\u2212\ufe58\ufe63\uff0d"
SEPARATOR_CLASS = "[" + SEPARATORS + "]"
DIGIT = _class(ND)
NOT_DIGIT = _class(ND, negate=True)
# Five digits of any script, with at most one separator between two digits: the run A154 forbids.
DIGIT_RUN = rf"^(?![\s\S]*{DIGIT}(?:{SEPARATOR_CLASS}?{DIGIT}){{4}})[\s\S]*$"
# A masked case reference shows at most four digits in all (A187), of any script.
AT_MOST_FOUR_DIGITS = rf"^(?:{NOT_DIGIT}*{DIGIT}){{0,4}}{NOT_DIGIT}*$"
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


# Public stage (A187: six values or fewer, never the 21 internal states). The values are the author's choice, stated in the pull request; the owner of the plain-language strings
# is the frontend owner. The core maps its internal states to these (the investigation route is not built yet).
PUBLIC_STAGES = ["UNDERSTANDING", "CHECKING", "NEEDS_YOU", "ACTING", "HANDED_TO_TEAM", "FINISHED"]


def internal_states() -> list[str]:
    text = (ROOT / "services" / "core" / "orchestrator" / "transitions.yaml").read_text(encoding="utf-8")
    return [x.strip() for x in re.search(r"^states:\s*\[(.*?)\]", text, flags=re.M).group(1).split(",")]


def refused(pattern: str, text: str) -> bool:
    """True when the contract pattern rejects the text, read as Python's re reads it (the validator and the generated models use the same anchoring)."""
    return re.search(fully_anchored(pattern), text) is None


class PublicApi(unittest.TestCase):
    def test_the_client_gets_a_small_public_stage_and_never_an_internal_state(self):
        stage = SCHEMAS["investigation-response.v1.json"]["$defs"]["PublicStage"]["enum"]
        self.assertEqual(stage, PUBLIC_STAGES)
        self.assertLessEqual(len(stage), 6)
        self.assertEqual(SCHEMAS["investigation-response.v1.json"]["properties"]["state"]["$ref"], "#/$defs/PublicStage")
        self.assertNotIn("InvestigationState", SCHEMAS["investigation-response.v1.json"]["$defs"])
        internal = internal_states()
        self.assertEqual(len(internal), 21)
        self.assertFalse(set(stage) & set(internal), "a public stage is named like an internal state")
        for name in NAMES:
            text = json.dumps(SCHEMAS[name])
            for state in internal:
                self.assertNotIn(f'"{state}"', text, f"{name} names the internal state {state}")

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
                    ok = pattern in (DIGIT_RUN, AT_MOST_FOUR_DIGITS) or (leaf in KEYED_STRINGS and pattern)
                    self.assertTrue(ok, f"{name}: {'/'.join(path)} is a string with no digit-run pattern")
                    self.assertIn("maxLength", node, f"{name}: {'/'.join(path)} is a free-text string with no maxLength") if pattern in (DIGIT_RUN, AT_MOST_FOUR_DIGITS) else None

    def test_the_digit_class_is_every_decimal_digit_the_pinned_unicode_knows(self):
        digits = {chr(cp) for a, b in ND for cp in range(a, b + 1)}
        self.assertEqual(digits, {chr(cp) for cp in range(sys.maxunicode + 1) if unicodedata.category(chr(cp)) == "Nd"})
        for sample in ("0", "9", "\u0664", "\u0966", "\uff15", "\U0001d7d8"):
            self.assertIn(sample, digits)

    def test_the_digit_run_pattern_refuses_five_digits_of_any_script_and_with_one_separator(self):
        refuse = [
            "12345", "\u0661\u0662\u0663\u0664\u0665", "\u0967\u0968\u0969\u096a\u096b", "\uff11\uff12\uff13\uff14\uff15", "\U0001d7d9\U0001d7da\U0001d7db\U0001d7dc\U0001d7dd",
            "12\u0663\u0664 5", "1234 5678 9012", "1234-5678", "1234\u00a05", "1234\u20115", "1234\u30005", "see 41111 and more",
        ]
        accept = ["1234", "\u0661\u0662\u0663\u0664", "ending 4821", "1234 and 5678", "12 ab 34 cd 5", "1234\n5678", "no digits at all"]
        for text in refuse:
            self.assertTrue(refused(DIGIT_RUN, text), f"{text!r} passes the digit-run pattern")
        for text in accept:
            self.assertFalse(refused(DIGIT_RUN, text), f"{text!r} is refused by the digit-run pattern")

    def test_a_masked_case_reference_shows_at_most_four_digits_in_all(self):
        for name, path in (("investigation-response.v1.json", ("properties", "case_ref_masked")), ("customer-answer.v1.json", ("properties", "case_ref_masked"))):
            node = SCHEMAS[name]
            for key in path:
                node = node[key]
            self.assertEqual(node["pattern"], AT_MOST_FOUR_DIGITS, name)
        for text in ("CMP-****0001", "****4821", "CMP", "\u0661\u0662\u0663\u0664"):
            self.assertFalse(refused(AT_MOST_FOUR_DIGITS, text), f"{text!r} is refused")
        for text in ("CMP-2026-0001", "12345", "CMP-20-26-0001", "\u0661\u0662\u0663\u0664\u0665", "12 34 5"):
            self.assertTrue(refused(AT_MOST_FOUR_DIGITS, text), f"{text!r} passes")

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

    def test_the_public_error_body_is_errors_v2_with_one_not_found_shape_and_a_guarded_field(self):
        v2, v3 = SCHEMAS["errors.v2.json"]["$defs"], SCHEMAS["errors.v3.json"]["$defs"]
        self.assertEqual(set(v2["ErrorCode"]["enum"]) - set(v3["ErrorCode"]["enum"]), {"TENANT_SCOPE_MISMATCH"})
        self.assertEqual(set(v3["ErrorCode"]["enum"]) - set(v2["ErrorCode"]["enum"]), set())
        self.assertIn("NOT_FOUND_OR_NOT_AUTHORIZED", v3["ErrorCode"]["enum"])
        retry, non, every = (set(v3[k]["enum"]) for k in ("RetryableErrorCode", "NonRetryableErrorCode", "ErrorCode"))
        self.assertFalse(retry & non)
        self.assertEqual(retry | non, every)
        self.assertEqual(retry, set(v2["RetryableErrorCode"]["enum"]))
        field = v3["ErrorBody"]["properties"]["details"]["items"]["properties"]["field"]
        self.assertEqual(field["pattern"], DIGIT_RUN)
        self.assertIn("maxLength", field)
        self.assertIn("maxItems", v3["ErrorBody"]["properties"]["details"])
        for member in ("message",):
            self.assertEqual(v3["ErrorBody"]["properties"][member]["pattern"], DIGIT_RUN)
        self.assertEqual(v3["ErrorBody"]["properties"]["details"]["items"]["properties"]["issue"]["pattern"], DIGIT_RUN)

    def test_the_error_event_uses_the_errors_v3_body(self):
        error = SCHEMAS["sse-event.v1.json"]["$defs"]["Error"]["properties"]["error"]
        self.assertEqual(error["$ref"], "https://contracts.noema.invalid/errors.v3.json#/$defs/ErrorBody")

    def test_the_earlier_contracts_are_not_edited(self):
        for name in ("clarification.v1.json", "confirmation-request.v1.json", "errors.v2.json", "noema-resolution.v1.json"):
            self.assertIn(name, SCHEMAS)  # the new set only references them; the drift and fixture checks hold their content


if __name__ == "__main__":
    unittest.main()
