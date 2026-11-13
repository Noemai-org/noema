"""intent-object.v3 and intent-slot.v2 (ruling A166 (2) of the Architect, recorded in docs#159; task A173): confidence as integer basis points.

Why. `crypto.CanonicalJSON` refuses a fractional number, so no hash or signature can be computed over an Intent Object that holds `0.93`. A166 (2) says the fix is in the data,
not in the canonical rule. v3 carries every confidence as `BasisPoints`: an integer from 0 to 10000 (1 = 0.01 percent). It appears in two places: the `confidence` of the slots
(`intent-slot.v2`) and `source.asr_confidence` (`intent-object.v3`). `common.v1` (and its `Ratio`) is not edited; `canonical-input.v2` keeps `Ratio` (it is never hashed).

This file checks that v1 and v2 and the slot v1 were not edited, that a v2 object is not a valid v3 object and the reverse, that every valid v2 fixture has a v3 twin that
validates and differs only where the bump says, that exactly the intended places differ in the schemas, and the edges of BasisPoints.
"""
import copy
import hashlib
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import FIXTURES, SCHEMAS as SCHEMA_DIR, load_schemas, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
PINNED = {
    "intent-object.v1.json": "472e89bab3602dad03d4cb4ac95840a6b1cc0bec8a796f28fad0067c1d22b66d",
    "intent-object.v2.json": "cb9e534f4b14f82c035bf40db19ea89cc8be93d5612edd6ee3e8ed7f973b17a5",
    "intent-slot.v1.json": "33e54108f2b4dba0c65505891d9d9ad1ab52a3be306098a58901ced29964e611",
    "common.v1.json": "c2e465911abdbe1f26f59b7460ef51c010c472b533daaf82a8601109337a7ae2",
}


def check(doc, version):
    return list(validator_for(version, SCHEMAS, REGISTRY).iter_errors(doc))


def valid_v3(name="sot_example.json"):
    return json.loads((FIXTURES / "valid" / "intent-object.v3" / name).read_text())


def diff(a, b, path=""):
    """The set of JSON paths at which two schema documents differ."""
    if isinstance(a, dict) and isinstance(b, dict):
        out = set()
        for k in set(a) | set(b):
            if k not in a or k not in b:
                out.add(f"{path}/{k}")
            else:
                out |= diff(a[k], b[k], f"{path}/{k}")
        return out
    return set() if a == b else {path}


def confidences(doc):
    """(path, value) of every confidence in an Intent Object: the slots' `confidence` and `source.asr_confidence`."""
    out = []
    for name, slot in doc.get("slots", {}).items():
        if "confidence" in slot:
            out.append((f"slots.{name}.confidence", slot["confidence"]))
    if "asr_confidence" in doc.get("source", {}):
        out.append(("source.asr_confidence", doc["source"]["asr_confidence"]))
    return out


def scaled_back(doc):
    """A v3 document with its basis points turned back into ratios and its version into 2.0: what its v2 twin must equal."""
    doc = copy.deepcopy(doc)
    doc["schema_version"] = "2.0"
    for slot in doc.get("slots", {}).values():
        if isinstance(slot.get("confidence"), int):
            slot["confidence"] = slot["confidence"] / 10000
    src = doc.get("source", {})
    if isinstance(src.get("asr_confidence"), int):
        src["asr_confidence"] = src["asr_confidence"] / 10000
    return doc


class BasisPoints(unittest.TestCase):
    def test_earlier_versions_were_not_edited(self):
        for name, sha in PINNED.items():
            self.assertEqual(hashlib.sha256((SCHEMA_DIR / name).read_bytes()).hexdigest(), sha, f"{name} was edited (AGENT-15)")

    def test_a_v2_object_is_not_a_valid_v3_object_and_the_reverse(self):
        v2 = json.loads((FIXTURES / "valid" / "intent-object.v2" / "sot_example.json").read_text())
        self.assertEqual(check(v2, "intent-object.v2"), [])
        self.assertNotEqual(check(v2, "intent-object.v3"), [], "a v2 object must not validate as v3")
        v3 = valid_v3()
        self.assertEqual(check(v3, "intent-object.v3"), [])
        self.assertNotEqual(check(v3, "intent-object.v2"), [], "a v3 object (basis points over 1) must not validate as v2")

    def test_every_valid_v2_fixture_has_a_v3_twin_that_validates_and_differs_only_in_scale(self):
        v2_dir, v3_dir = FIXTURES / "valid" / "intent-object.v2", FIXTURES / "valid" / "intent-object.v3"
        v2 = {p.name for p in v2_dir.glob("*.json")}
        v3 = {p.name for p in v3_dir.glob("*.json")}
        self.assertTrue(v2 <= v3, f"missing twins: {sorted(v2 - v3)}")
        for name in sorted(v2):
            twin = json.loads((v3_dir / name).read_text())
            self.assertTrue(twin["schema_version"].startswith("3."), name)
            self.assertEqual(check(twin, "intent-object.v3"), [], name)
            original = json.loads((v2_dir / name).read_text())
            self.assertEqual(scaled_back(twin), original, f"{name}: the v3 twin differs from v2 in more than the version and the scale")

    def test_every_invalid_v2_fixture_has_a_v3_twin_that_is_refused(self):
        v2 = {p.name for p in (FIXTURES / "invalid" / "intent-object.v2").glob("*.json") if p.name != "expectations.json"}
        v3 = {p.name for p in (FIXTURES / "invalid" / "intent-object.v3").glob("*.json") if p.name != "expectations.json"}
        self.assertTrue(v2 <= v3, f"missing twins: {sorted(v2 - v3)}")
        for name in sorted(v3):
            doc = json.loads((FIXTURES / "invalid" / "intent-object.v3" / name).read_text())
            self.assertNotEqual(check(doc, "intent-object.v3"), [], name)

    def test_no_v3_fixture_holds_a_fractional_number_in_a_confidence(self):
        """The point of the bump: a valid v3 object has no float where a confidence is, so CanonicalJSON can hash it."""
        for p in sorted((FIXTURES / "valid" / "intent-object.v3").glob("*.json")):
            for path, value in confidences(json.loads(p.read_text())):
                self.assertTrue(value is None or (isinstance(value, int) and not isinstance(value, bool)), f"{p.name} {path} = {value!r}")

    def test_exactly_the_intended_places_differ_between_the_schemas(self):
        obj = diff(SCHEMAS["intent-object.v2.json"], SCHEMAS["intent-object.v3.json"])
        allowed_obj = ("/$id", "/description", "/properties/schema_version", "/properties/source/properties/asr_confidence", "/properties/slots/additionalProperties")
        stray = sorted(p for p in obj if not p.startswith(allowed_obj))
        self.assertEqual(stray, [], f"intent-object.v3 changes more than A166 (2) allows: {stray}")
        slot = diff(SCHEMAS["intent-slot.v1.json"], SCHEMAS["intent-slot.v2.json"])
        allowed_slot = ("/$id", "/description", "/$defs/BasisPoints") + tuple(
            f"/$defs/{s}/properties/confidence" for s in ("QuantitySlot", "PartySlot", "TimeRefSlot", "ReferenceSlot", "ResourceSlot"))
        stray = sorted(p for p in slot if not p.startswith(allowed_slot))
        self.assertEqual(stray, [], f"intent-slot.v2 changes more than A166 (2) allows: {stray}")

    def test_neither_new_schema_refers_to_ratio(self):
        """Ratio is a fractional number and must not come back through a $ref."""
        for name in ("intent-object.v3.json", "intent-slot.v2.json"):
            self.assertNotIn("Ratio", json.dumps(SCHEMAS[name]), name)

    def test_the_basis_points_edges(self):
        for good in (0, 1, 9300, 9999, 10000):
            doc = valid_v3()
            doc["slots"]["amount"]["confidence"] = good
            self.assertEqual(check(doc, "intent-object.v3"), [], good)
            doc = valid_v3()
            doc["source"]["asr_confidence"] = good
            self.assertEqual(check(doc, "intent-object.v3"), [], good)
        for bad in (-1, 10001, 0.93, 1.5, "9300", True, False, [], {}):
            doc = valid_v3()
            doc["slots"]["amount"]["confidence"] = bad
            self.assertNotEqual(check(doc, "intent-object.v3"), [], repr(bad))
            doc = valid_v3()
            doc["source"]["asr_confidence"] = bad
            self.assertNotEqual(check(doc, "intent-object.v3"), [], repr(bad))
        doc = valid_v3()
        doc["source"]["asr_confidence"] = None
        self.assertEqual(check(doc, "intent-object.v3"), [], "a text turn has no ASR confidence: null stays legal")

    def test_every_slot_shape_takes_basis_points(self):
        """All five slot shapes that carry a confidence take BasisPoints and refuse a ratio."""
        shapes = {
            "Resource": {"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX88213", "confidence": 9500},
            "Quantity": {"type": "Quantity", "value": 2000000, "unit": "NGN", "confidence": 9500},
            "Party": {"type": "Party", "surface_form": "Tunde", "confidence": 9500},
            "TimeRef": {"type": "TimeRef", "surface_form": "this morning", "confidence": 9500},
            "Reference": {"type": "Reference", "surface_form": "the last one", "confidence": 9500},
        }
        for kind, slot in shapes.items():
            doc = valid_v3()
            doc["slots"] = {"subject": slot}
            self.assertEqual(check(doc, "intent-object.v3"), [], f"{kind} with basis points: {check(doc, 'intent-object.v3')[:1]}")
            bad = copy.deepcopy(slot)
            bad["confidence"] = 0.95
            doc["slots"] = {"subject": bad}
            self.assertNotEqual(check(doc, "intent-object.v3"), [], f"{kind} with a ratio must be refused")


if __name__ == "__main__":
    unittest.main()
