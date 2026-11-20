"""record-event.v3 SecurityKind and the vendored PERD §32.2 (REQ-EXE-008, REQ-AUD-001, SoT §43.6; ruling A114 of DEC-2026-11-10, relayed order of 19 Nov 2026).

PERD v1.22 to v1.24 §32.2 (Security row) names `KILL_SWITCH_CLEARED` and `KILL_SWITCH_CLEAR_REFUSED` as KINDS of `SECURITY_EVENT`, not as event types:
`SECURITY_EVENT{kind: ..., KILL_SWITCH_CLEARED, KILL_SWITCH_CLEAR_REFUSED}`. `tools/contracts/drift.py` strips the `{kind: ...}` part, so it compares event types
only; this file compares the kinds. The vendored copy is `contracts/perd-32-2-event-types.md`; the PERD original wins. All fixture data is SYNTHETIC."""
import json
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import FIXTURES, ROOT, load_schemas, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
V2, V3 = SCHEMAS["record-event.v2.json"]["$defs"], SCHEMAS["record-event.v3.json"]["$defs"]
VALID = FIXTURES / "valid" / "record-event.v3"
TWO = ("KILL_SWITCH_CLEARED", "KILL_SWITCH_CLEAR_REFUSED")


def perd_security_kinds() -> list[str]:
    text = (ROOT / "contracts" / "perd-32-2-event-types.md").read_text(encoding="utf-8")
    row = next(line for line in text.splitlines() if line.startswith("| Security |"))
    return [k.strip() for k in re.search(r"SECURITY_EVENT\{kind: ([^}]*)\}", row).group(1).split(",")]


def check(doc):
    return list(validator_for("record-event.v3", SCHEMAS, REGISTRY).iter_errors(doc))


class SecurityKindsMatchPerd(unittest.TestCase):
    def test_the_vendored_row_names_the_two_kill_switch_kinds_among_its_kinds(self):
        kinds = perd_security_kinds()
        self.assertIn("CHAIN_BREAK", kinds, "the scan sees the kinds of the row")
        for k in TWO:
            self.assertIn(k, kinds)

    def test_every_kind_the_perd_row_names_is_a_security_kind_of_the_contract(self):
        have = V3["SecurityKind"]["enum"]
        missing = [k for k in perd_security_kinds() if k not in have]
        self.assertEqual(missing, [], f"PERD §32.2 names these SECURITY_EVENT kinds, record-event.v3 does not list them: {missing}")

    def test_the_kinds_are_added_to_the_kinds_and_never_to_the_event_types(self):
        # the PERD lists them under SECURITY_EVENT: as event types they would be a Record event the PERD does not have (and drift.py would fail)
        for k in TWO:
            self.assertNotIn(k, V3["RecordEventType"]["enum"])
        self.assertEqual(V3["RecordEventType"], V2["RecordEventType"])
        base = json.loads((VALID / "security_event.json").read_text(encoding="utf-8"))
        for k in TWO:
            self.assertTrue(check({**{n: v for n, v in base.items() if n != "kind"}, "event_type": k}), f"{k} used as an event type")

    def test_the_change_is_additive_v3_keeps_every_kind_v2_has_in_the_same_order(self):
        v2, v3 = V2["SecurityKind"]["enum"], V3["SecurityKind"]["enum"]
        self.assertEqual(v3[: len(v2)], v2)
        self.assertEqual(len(v3), len(set(v3)))


def fixture(kind: str) -> dict:
    path = VALID / f"security_event_{kind.lower()}.json"
    return json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}


class KillSwitchKindFixtures(unittest.TestCase):
    def test_each_kind_has_a_valid_security_event_fixture(self):
        for kind in TWO:
            doc = fixture(kind)
            self.assertTrue(doc, f"no valid fixture security_event_{kind.lower()}.json")
            self.assertEqual((doc["event_type"], doc["kind"]), ("SECURITY_EVENT", kind))
            self.assertEqual(check(doc), [], kind)

    def test_a_security_event_with_a_kind_outside_the_list_is_still_refused(self):
        doc = json.loads((VALID / "security_event.json").read_text(encoding="utf-8"))
        self.assertEqual(check(doc), [], "positive control")
        for bad in ("KILL_SWITCH_SET", "KILL_SWITCH_CHANGED", "kill_switch_cleared", "KILL_SWITCH_CLEARED "):
            self.assertTrue(check({**doc, "kind": bad}), bad)


if __name__ == "__main__":
    unittest.main()
