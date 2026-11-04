"""CON-03: tools/codegen/shape_refusals.json is what the validator says it is, and names only fixtures that exist."""
import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools" / "codegen"))

import shape_refusals  # noqa: E402


class ShapeRefusals(unittest.TestCase):
    def test_the_committed_list_is_current(self):
        self.assertEqual(shape_refusals.classify(), json.loads(shape_refusals.OUT.read_text()),
                         "run: .venv-contracts/bin/python tools/codegen/shape_refusals.py --write")

    def test_the_list_is_not_trivially_small(self):
        listed = json.loads(shape_refusals.OUT.read_text())
        self.assertGreaterEqual(len(listed), 150)
        self.assertTrue(set(listed.values()) <= shape_refusals.SHAPE)

    def test_every_listed_fixture_exists(self):
        for ident in json.loads(shape_refusals.OUT.read_text()):
            folder, name = ident.split("/")
            self.assertTrue((ROOT / "contracts" / "fixtures" / "invalid" / folder / name).is_file(), ident)


if __name__ == "__main__":
    unittest.main()
