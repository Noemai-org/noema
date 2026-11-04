"""REQ-AUD-003 (REC-03): the reconstructions the core builds, checked in a second language.

The Go tests of services/core/record/reconstruct build the US-01 and US-06 reconstructions from a real Record (real appender,
real PostgreSQL) and commit them as testdata/*.golden.json; the Go tests fail if a build differs from the committed file by one
byte. Here each committed body is validated against contracts/schemas/reconstruction.v3.json, its citations are checked, and
every event hash is recomputed with the Python reference of PERD v1.17 §32.1 (tools/record_chain_ref_v3.py), so a Go and a
Python implementation agree on the chain the body claims verified."""
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from validate import ROOT, load_schemas, load_strict, make_registry, validator_for  # noqa: E402
from record_chain_ref_v3 import event_hash, genesis_hash  # noqa: E402

GOLDEN = ROOT / "services" / "core" / "record" / "reconstruct" / "testdata"
ORDER = ["WHO", "REQUEST", "AMBIGUITY", "RESOLUTION", "OBSERVED_STATE", "POLICY", "AUTHORITY", "DECISION", "ACTION",
         "DOWNSTREAM_RESULT", "VERIFICATION", "EXPLANATION"]


def bodies():
    files = sorted(GOLDEN.glob("*.golden.json"))
    if not files:
        raise AssertionError(f"no golden reconstruction in {GOLDEN}")
    return [(p.name, load_strict(p)) for p in files]


class GoldenReconstruction(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        schemas = load_schemas()
        cls.validator = validator_for("reconstruction.v3", schemas, make_registry(schemas))

    def test_each_body_is_valid_under_reconstruction_v3(self):
        for name, body in bodies():
            errors = sorted(self.validator.iter_errors(body), key=lambda e: list(e.path))
            self.assertEqual([], [f"{list(e.path)}: {e.message}" for e in errors], name)

    def test_twelve_sections_in_order_and_every_item_cites_an_event_of_the_body(self):
        for name, body in bodies():
            self.assertEqual([s["kind"] for s in body["sections"]], ORDER, name)
            events = {e["event_id"]: e for e in body["events"]}
            for section in body["sections"]:
                for item in section["items"]:
                    self.assertIn(item["record_event_id"], events, name)
                    self.assertEqual(item["object_ref"], events[item["record_event_id"]]["object_ref"], name)
            self.assertEqual([t["record_event_id"] for t in body["timeline"]], list(events), name)

    def test_every_event_hash_recomputes_in_python_from_genesis(self):
        for name, body in bodies():
            prev = genesis_hash(body["tenant_id"])
            for event in body["events"]:
                self.assertEqual(event["prev_hash"], prev, f"{name}: {event['event_id']}")
                unhashed = {k: v for k, v in event.items() if k != "hash"}
                self.assertEqual(event_hash(prev, unhashed), event["hash"], f"{name}: {event['event_id']}")
                prev = event["hash"]
            self.assertEqual(body["chain"]["head_hash"], prev, name)
            self.assertEqual(body["chain"]["head_event_id"], body["events"][-1]["event_id"], name)


if __name__ == "__main__":
    unittest.main()
