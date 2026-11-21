"""The public API response and SSE contracts (PERD §39.2, §40.1, §13; REQ-API-001, REQ-UI-001, REQ-UI-002, REQ-SEC-003; AGENT-15).

investigation-response.v1, customer-answer.v1, overlay-card.v1 and sse-event.v1 are new files: nothing earlier changes. These tests hold what the schemas alone cannot:
the client sees a small public stage and never an internal state (A187), the five event kinds are PERD §39.2's, every free-text member carries the digit-run pattern
of every script (A154, A184, A194), the public error body has one not-found shape (A187), and no member of the 'Never shown' list (PERD §40.1) or of an internal object can
be added by accident. Names are SYNTHETIC test data.

The digit-run pattern is built here from the Unicode decimal digits (category Nd, Python 3.13: Unicode 15.1) and the separator set of the model redactor (noema#468,
`noema_model/platform/redaction`) and the core (`record/reconstruct/project.go`), written as literal characters so that Python's `re`, Pydantic's Rust regex and a JavaScript
RegExp with the `u` flag read the same pattern (the digit shorthand is ASCII-only in the last two and Unicode in the first; the Unicode property class for decimal digits is not in
Python's `re`). A test recomputes the class, so a Python upgrade that adds digits fails here instead of leaving a digit unmasked. This file reads the pattern with Python's `re`;
the generated Pydantic models are held to the same fixtures by `shape_refusals.json` in CI, and a JavaScript validator must compile the pattern with the `u` flag (noema#523 does).
Go's regexp (RE2) has no look-ahead, so the generated Go types do not evaluate this pattern and no claim is made that they do: on the Go side the masking is enforced by
`record/payloadguard` on every outbound edge response (A194, A211), and the Go tests of that package are the check.
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


from digit_run import DIGIT, DIGIT_RUN, FACT_VALUE, NOT_DIGIT, ND, SEPARATOR_CLASS, SEPARATORS, _ranges  # noqa: E402  (one definition of the pattern, tools/contracts/digit_run.py; A258)

# A masked case reference shows at most four digits in all (A187), of any script, and carries a mask character (A207: the core masks it before it reaches the client).
MASK_CHARS = "*\u2022\u2026"
AT_MOST_FOUR_DIGITS = rf"^(?=[\s\S]*[{MASK_CHARS}])(?:{NOT_DIGIT}*{DIGIT}){{0,4}}{NOT_DIGIT}*$"
# option_id of an overlay card: no more than four digits in all (A211), and bounded. The pack version of a confirmation: at most four digits in all, name and numbers together (A214), and bounded (ASCII-only members, so the ASCII class is the whole alphabet).
OPTION_ID = r"^(?![a-z_]*(?:[0-9][a-z_]*){5})[a-z0-9_]{1,32}$"
POLICY_VERSION = r"^(?![^0-9]*(?:[0-9][^0-9]*){5})[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*@[0-9]{1,4}\.[0-9]{1,4}(\.[0-9]{1,4})?$"
# PERD §39.2: the five event kinds of the stream.
EVENT_KINDS = {"progress", "clarification", "confirmation", "answer", "error"}
# PERD §40.1 "Never shown", and the internal members of clarification.v1 and confirmation-request.v1 that the overlay projection leaves out.
NEVER_MEMBERS = {
    "confidence", "model_confidence", "reason_codes", "reason_code", "stack_trace", "stack", "threshold", "policy_threshold", "evidence", "evidence_payload", "payload",
    "binding_hash", "sig", "tenant_id", "decision_id", "candidate_set_id", "candidate_set_version", "question_key", "slot", "principal_id", "account_number",
}
# Strings that are not free text, each with its own pattern: an option key.
KEYED_STRINGS = {"option_id": OPTION_ID, "policy_version": POLICY_VERSION}


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


# Public stage (A187: six values or fewer, never the 21 internal states). The six values are A207's, adopted by A211; the owner of the plain-language strings
# is the frontend owner. The core maps its internal states to these (the investigation route is not built yet).
PUBLIC_STAGES = ["understanding", "needs_input", "needs_confirmation", "working", "answered", "unavailable"]

# A275: the core-minted id of "None of these" is an option key of the same form as option_id (KEYED_STRINGS above), added here so that its definition stays one statement.
KEYED_STRINGS["none_option_id"] = OPTION_ID


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
                    ok = pattern in (DIGIT_RUN, AT_MOST_FOUR_DIGITS) or (leaf in KEYED_STRINGS and pattern == KEYED_STRINGS[leaf])
                    self.assertTrue(ok, f"{name}: {'/'.join(path)} is a string with no digit-run pattern")
                    self.assertIn("maxLength", node, f"{name}: {'/'.join(path)} is a free-text string with no maxLength") if pattern in (DIGIT_RUN, AT_MOST_FOUR_DIGITS) else None

    def test_the_digit_class_is_every_decimal_digit_the_pinned_unicode_knows(self):
        digits = {chr(cp) for a, b in ND for cp in range(a, b + 1)}
        self.assertEqual(digits, {chr(cp) for cp in range(sys.maxunicode + 1) if unicodedata.category(chr(cp)) == "Nd"})
        for sample in ("0", "9", "\u0664", "\u0966", "\uff15", "\U0001d7d8"):
            self.assertIn(sample, digits)

    def test_the_digit_run_pattern_refuses_five_digits_of_any_script_across_any_separator_of_the_cores_set(self):
        refuse = [
            "12345", "\u0661\u0662\u0663\u0664\u0665", "\u0967\u0968\u0969\u096a\u096b", "\uff11\uff12\uff13\uff14\uff15", "\U0001d7d9\U0001d7da\U0001d7db\U0001d7dc\U0001d7dd",
            "12\u0663\u0664 5", "1234 5678 9012", "1234-5678", "1234\u00a05", "1234\u20115", "1234\u30005", "see 41111 and more",
            # the Security Reviewer's cases on noema#521: slash, dot, brackets, colon, underscore, tab, newline, the soft hyphen and zero-width characters, and several separators in a row
            "4111/1111/1111/1111", "4111.1111.1111.1111", "(4111)(1111)5", "4111:1111:1", "4111_1111_1", "1234\t5678", "1234\n5678", "1234\r\n5678", "1234\u00ad5678", "1234\u200b5678",
            "1234 - 5678 - 9012", "12 / 34 / 5", "12 ( 34 ) 5",
        ]
        accept = ["1234", "\u0661\u0662\u0663\u0664", "ending 4821", "1234 and 5678", "12 ab 34 cd 5", "20,000,000 naira", "1,234", "1234, 5678", "no digits at all"]
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
        for text in ("CMP-****0001", "****4821", "CMP-\u20260001", "CMP-\u2022\u2022\u20220001", "CMP-****"):
            self.assertFalse(refused(AT_MOST_FOUR_DIGITS, text), f"{text!r} is refused")
        # at most four digits in all, and the mask must be there: a reference with four digits and no mask character is not masked (review of noema#521, note 3)
        for text in ("CMP-2026-0001", "12345", "CMP-20-26-0001", "\u0661\u0662\u0663\u0664\u0665", "12 34 5", "CMP-1234", "CMP", "\u0661\u0662\u0663\u0664"):
            self.assertTrue(refused(AT_MOST_FOUR_DIGITS, text), f"{text!r} passes")

    def test_the_option_key_and_the_pack_version_cannot_carry_a_number(self):
        # review of noema#521, blocking 2: an option_id of 32 digits, or a policy_version with unbounded digits, would reach the customer as written.
        option = SCHEMAS["overlay-card.v1.json"]["$defs"]["ClarificationCard"]["properties"]["options"]["items"]["properties"]["option_id"]
        version = SCHEMAS["overlay-card.v1.json"]["$defs"]["ConfirmationCard"]["properties"]["policy_version"]
        self.assertEqual(option["pattern"], OPTION_ID)
        self.assertEqual(version["pattern"], POLICY_VERSION)
        self.assertIn("maxLength", version)
        for text in ("opt_1", "opt_2", "none", "o" * 32, "opt_1234", "opt_1_2_3_4"):
            self.assertFalse(refused(OPTION_ID, text), f"option {text!r} is refused")
        for text in ("opt_12345", "4" * 32, "opt_" + "1" * 20, "", "x" * 33, "Opt_1", "a1b2c3d4e5", "opt_1_2_3_4_5", "1a2b3c4d5"):
            self.assertTrue(refused(OPTION_ID, text), f"option {text!r} passes")
        for text in ("fintech.transfer_exceptions@1.0", "fin.pol@3.1.2", "fin@12.34", "fin.p1@1.2.3"):
            self.assertFalse(refused(POLICY_VERSION, text), f"version {text!r} is refused")
        for text in ("fin.pol@12345.0", "fin.pol@1." + "9" * 20, "fin.pol@1.0.12345", "fin@1234.1234.1234", "fin@123.45.6", "fin.p12@12.345", "fin.p1@1.2.3.4"):
            self.assertTrue(refused(POLICY_VERSION, text), f"version {text!r} passes")

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


class WidenedAndBounded(unittest.TestCase):
    """A258 (interim): the digit-run pattern uses the core's whole separator set (marks, brackets, dashes, the middle dot), and reconstruction.v3 is bounded."""

    def test_five_digits_joined_by_any_separator_of_the_cores_set_are_refused(self):
        joiners = ["\u0301", "(", ")", "[", "]", "{", "}", "\u00b7", "\u30fb", "\uff0e", "\uff0f", "\uff1a", "\uff3f", "\u2212", "\u2014", "\u0085", "\x0b", "\u2028", "\u200b", "-", "/", "_", ":", "."]
        for j in joiners:
            self.assertTrue(refused(DIGIT_RUN, j.join("12345")), f"digits joined by U+{ord(j[0]):04X} were accepted")
        self.assertFalse(refused(DIGIT_RUN, "1,2,3,4,5"), "a comma ends a run")
        self.assertFalse(refused(DIGIT_RUN, "1234 words 5678"))

    def test_a_fact_value_is_an_id_or_a_reference_with_a_letter_or_a_string_without_five_joined_digits(self):
        ok = ["recev_1005", "obj_01HX12345ABCDEFGHJKMNPQRST", "fin.transaction:TX88213", "held for review", "the amount ending 1234", "dec_01HXZ", "1,2,3,4,5", "usr_8821"]
        no = ["usr_88213", "4111 1111 1111 1111", "fin.account:0123456789012345", "41111111-1111-4111-8111-11111111111a", "card 4111 1111 1111 1111 expired", "4111_1111_1111_1111",
              "1\u03012\u03013\u03014\u03015", "rec_12345", "fin.transaction:12345", "ab_" + "1" * 30]
        # the second rule of the digit run (commas) holds for the free-text branch too
        no += ["4111,1111,1111,1111", "card 4111, 1111, 1111, 1111 expired", "1,234,567,890"]
        ok += ["20,000,000", "the amount 20,000,000 was held"]
        for v in ok:
            self.assertFalse(refused(FACT_VALUE, v), f"{v!r} was refused")
        for v in no:
            self.assertTrue(refused(FACT_VALUE, v), f"{v!r} was accepted")

    def test_the_minted_id_branch_is_the_exact_ulid_body_and_every_other_id_meets_the_five_digit_rule(self):
        ulid = "01HX12345ABCDEFGHJKMNPQRST"
        self.assertEqual(len(ulid), 26)
        ok = ["obj_" + ulid, "dec_01J9X3A4B5C6D7E8F9G0H1J2K3", "recev_1005", "usr_8821", "dec_01HXZ", "ten_AB1234", "int_01J9X3"]
        no = [
            "ab_" + "1" * 30 + "a",  # the Security Reviewer's first case on noema#664: 31 characters, a letter at the end
            "ten_4111111111111111a",  # a typed ID is no exemption for a number with a letter at its end
            "obj_81HX12345ABCDEFGHJKMNPQRST",  # first character 8: no ULID
            "obj_01HX12345ABCDEFGHJKMNPQRSI",  # I is not in the alphabet
            "obj_01hx12345abcdefghjkmnpqrst",  # lower case
            "obj_" + ulid[:-1], "obj_" + ulid + "V",  # 25 and 27 characters
            "obj_" + "0" * 26,  # no letter in the body
            "usr_88213", "rec_12345", "obj_12345A",
        ]
        for v in ok:
            self.assertFalse(refused(FACT_VALUE, v), f"{v!r} was refused")
        for v in no:
            self.assertTrue(refused(FACT_VALUE, v), f"{v!r} was accepted")

    def test_the_schemas_hold_exactly_the_pattern_of_this_module(self):
        for name in NAMES:
            for path, node in walk(SCHEMAS[name]):
                if node.get("pattern", "").startswith("^(?![\\s\\S]*[0-9"):
                    self.assertEqual(node["pattern"], DIGIT_RUN, f"{name}: {'/'.join(path)}")

    def test_reconstruction_v3_lists_are_bounded_and_its_text_carries_the_pattern(self):
        d = SCHEMAS["reconstruction.v3.json"]
        for node, what in ((d["properties"]["events"], "events"), (d["properties"]["timeline"], "timeline"), (d["$defs"]["Section"]["properties"]["items"], "items")):
            self.assertIn("maxItems", node, what)
        self.assertIn("maxProperties", d["$defs"]["Item"]["properties"]["facts"])
        self.assertEqual(d["$defs"]["TimelineEntry"]["properties"]["text"]["pattern"], DIGIT_RUN)
        facts = d["$defs"]["Item"]["properties"]["facts"]
        # A278: by value, under any key; no key is named
        self.assertNotIn("properties", facts)
        for alt in facts["additionalProperties"]["oneOf"]:
            if alt.get("type") == "string":
                self.assertEqual(alt["pattern"], FACT_VALUE)
            if alt.get("type") == "array":
                self.assertEqual(alt["items"]["pattern"], FACT_VALUE)
        reason = SCHEMAS["record-event.v3.json"]["$defs"]["RecordEvent"]["properties"]["reason"]
        self.assertEqual((reason["pattern"], reason["maxLength"]), ("^[A-Z_]{1,64}$", 64))


# Tightenings of the digit-run pattern (rulings A308 and A317 to A328, interim, relayed; AGENT-15, a minor change in place). Each is a narrowing: nothing that was refused is accepted now.
# An independent oracle, written here from the definitions and not from the pattern, gives the verdict the pattern must give on every string.
JOIN_CATEGORIES = {"Zs", "Zl", "Zp", "Cf", "Mn", "Mc", "Me", "Pd", "Ps", "Pe"}
JOIN_CODE_POINTS = {0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x1C, 0x1D, 0x1E, 0x1F, 0x85, 0x2E, 0x2D, 0x2F, 0x5F, 0x3A, 0x2212, 0xB7, 0x30FB, 0xFF0E, 0xFF0F, 0xFF1A, 0xFF3F}
FIVE, TEN = 5, 10


def longest_joined_run(text: str, commas: bool) -> int:
    """The most digits (any script, Nd) in one run: digits joined by any number of separators of the core's set and, when commas is true, by ASCII commas too."""
    best = count = 0
    for ch in text:
        if unicodedata.category(ch) == "Nd":
            count += 1
            best = max(best, count)
        elif ord(ch) in JOIN_CODE_POINTS or unicodedata.category(ch) in JOIN_CATEGORIES or (commas and ch == ","):
            continue
        else:
            count = 0
    return best


def oracle_refuses(text: str) -> bool:
    return longest_joined_run(text, commas=False) >= FIVE or longest_joined_run(text, commas=True) >= TEN


class DigitRunTightenings(unittest.TestCase):
    def test_u001c_to_u001f_join_digits(self):
        for cp in (0x1C, 0x1D, 0x1E, 0x1F):
            sep = chr(cp)
            self.assertTrue(refused(DIGIT_RUN, sep.join("12345")), f"digits joined by U+{cp:04X} were accepted")
            self.assertTrue(refused(DIGIT_RUN, f"1234{sep}5"), f"1234 U+{cp:04X} 5 was accepted")
            self.assertTrue(refused(DIGIT_RUN, f"card 4111{sep}1111{sep}1111{sep}1111 used"), f"a card joined by U+{cp:04X} was accepted")
            self.assertFalse(refused(DIGIT_RUN, f"1234{sep}abcd{sep}5678"), f"U+{cp:04X} between letters and short runs is refused")
        self.assertFalse(refused(DIGIT_RUN, "12\x1c34"), "four digits in all, however joined")
        self.assertFalse(refused(DIGIT_RUN, "\x1c\x1d\x1e\x1f1234\x1f"), "separators with no digit beyond four")

    def test_ten_digits_joined_by_commas_and_separators_are_refused_and_short_numbers_are_not(self):
        refuse = [
            "1,234,567,890", "NGN 1,234,567,890", "NGN 1,234,567,890.00", "12,345,678,901", "1234,5678,90", "1234,5678,9012,3456", "4111,1111,1111,1111", "4111, 1111, 1111, 1111",
            "5,000, 3,000, 2,000", "1,2,3,4,5,6,7,8,9,0", "1,2,3,4,5,6,7,8,9,10", "1234-5678,90", "1234, 5678,​90", "١٢٣٤,٥٦٧٨,٩٠",
            "card 4111,1111,1111,1111 used", "x 12,345,678 ,,, 90 y", "1234\x1c5678,90", "NGN 1,000,000,000",
        ]
        accept = [
            "12,345", "NGN 12,345", "NGN 123,456,789", "20,000,000 naira", "1,234", "1234, 5678", "1,2,3,4,5", "1,2,3,4,5,6,7,8,9", "5,000, 3,000 and 2,000", "123,456,789 and 123,456,789",
            "1234 words 5678, 9012 words", "no digits, at all", "1234,5678 then 9012", "12,345,67 and 89", "1,000,000 and 2,000,000", "12,abc,345,def,678,ghi,90",
        ]
        for text in refuse:
            self.assertTrue(refused(DIGIT_RUN, text), f"{text!r} passes the digit-run pattern")
        for text in accept:
            self.assertFalse(refused(DIGIT_RUN, text), f"{text!r} is refused by the digit-run pattern")

    def test_the_pattern_gives_the_verdict_of_the_oracle_on_random_strings(self):
        import random
        rng = random.Random(20261118)
        alphabet = ["1", "2", "3", "7", "٣", "４", ",", ",", " ", ".", "-", "\x1c", "\x1d", "\x1e", "\x1f", "́", "(", ")", "_", "a", "x", "\n", "​", "·", ";", "'", ":", "/", "，", "،"]
        weights = [8, 8, 8, 8, 2, 2, 5, 5, 3, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1]
        both = [0, 0]
        for _ in range(6000):
            text = "".join(rng.choices(alphabet, weights, k=rng.randint(0, 36)))
            expected = oracle_refuses(text)
            both[expected] += 1
            self.assertEqual(refused(DIGIT_RUN, text), expected, f"{text!r}")
        self.assertGreater(min(both), 500, "the sample must hold both verdicts in number")

    def test_the_money_clause_of_the_comma_rule_changes_nothing_in_the_schema(self):
        # A money string of at most nine integer digits that holds ten digits or more also holds five joined by the point (333.00), which the first rule refuses already; with no decimals it holds nine or fewer.
        five_only = rf"^(?![\s\S]*{DIGIT}(?:{SEPARATOR_CLASS}*{DIGIT}){{4}})[\s\S]*$"
        for groups in range(0, 3):
            for decimals in ("", ".00"):
                text = "NGN 123" + ",456" * groups + decimals
                digits = longest_joined_run(text, commas=True)
                if digits >= TEN:
                    self.assertTrue(refused(five_only, text), f"{text!r}: ten digits and not refused by the first rule")
                self.assertEqual(refused(DIGIT_RUN, text), refused(five_only, text), text)
        self.assertTrue(refused(DIGIT_RUN, "NGN 1,234,567,890"), "ten integer digits are over the money limit")
        self.assertFalse(refused(DIGIT_RUN, "NGN 123,456,789"), "nine integer digits")


if __name__ == "__main__":
    unittest.main()
