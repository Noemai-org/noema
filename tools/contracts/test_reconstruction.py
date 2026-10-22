"""REQ-AUD-003: what JSON Schema cannot say about the reconstruction body, checked over its valid fixtures."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
VALID = ROOT / "contracts" / "fixtures" / "valid" / "reconstruction"
SCHEMA = json.loads((ROOT / "contracts" / "schemas" / "reconstruction.v1.json").read_text())
ORDER = ["WHO", "REQUEST", "AMBIGUITY", "RESOLUTION", "OBSERVED_STATE", "POLICY", "AUTHORITY", "DECISION", "ACTION",
         "DOWNSTREAM_RESULT", "VERIFICATION", "EXPLANATION"]


def bodies():
    return [(p.name, json.loads(p.read_text())) for p in sorted(VALID.glob("*.json"))]


class Reconstruction(unittest.TestCase):
    def test_the_order_in_the_schema_is_the_order_of_the_perd(self):
        kinds = [e["allOf"][1]["properties"]["kind"]["const"] for e in SCHEMA["properties"]["sections"]["prefixItems"]]
        self.assertEqual(kinds, ORDER)

    def test_every_item_and_timeline_entry_cites_an_event_that_is_in_the_body(self):
        for name, body in bodies():
            ids = {e["event_id"] for e in body["events"]}
            for section in body["sections"]:
                for item in section["items"]:
                    self.assertIn(item["record_event_id"], ids, name)
                    event = next(e for e in body["events"] if e["event_id"] == item["record_event_id"])
                    self.assertEqual(item["object_ref"], event["object_ref"], name)
            self.assertEqual([t["record_event_id"] for t in body["timeline"]], [e["event_id"] for e in body["events"]], name)

    def test_the_chain_head_is_the_last_event_and_events_are_in_time_order(self):
        for name, body in bodies():
            if body["events"]:
                self.assertEqual(body["chain"]["head_event_id"], body["events"][-1]["event_id"], name)
                self.assertEqual(body["chain"]["head_hash"], body["events"][-1]["hash"], name)
            times = [e["at"] for e in body["events"]]
            self.assertEqual(times, sorted(times), name)

    def test_an_event_appears_in_at_most_one_item_per_section(self):
        for name, body in bodies():
            for section in body["sections"]:
                ids = [i["record_event_id"] for i in section["items"]]
                self.assertEqual(len(ids), len(set(ids)), name)

    def test_the_response_carries_no_provider_cost(self):
        names = []

        def walk(node):
            if isinstance(node, dict):
                names.extend(node.get("properties", {}).keys())
                for v in node.values():
                    walk(v)
            elif isinstance(node, list):
                for v in node:
                    walk(v)

        walk(SCHEMA)
        self.assertFalse([n for n in names if "cost" in n], names)


if __name__ == "__main__":
    unittest.main()
