"""The independent Python checker must RECOMPUTE the negative grant-signing vectors, not only compare them with the valid signature.

TRU-05 (ruling of 8 Nov 2026 on noema#292, A53); REQ-TRU-008, REQ-EXE-001; INV-03, INV-17. A stale or malformed `must_not_verify` value must make
the checker fail. All data SYNTHETIC (the key in the vectors is a public test key).
"""
import contextlib
import copy
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import check_vectors  # noqa: E402

FIXTURE = ROOT / "contracts" / "fixtures" / "golden_vectors.json"


def run_checker(doc) -> tuple[int, str]:
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "golden_vectors.json"
        path.write_text(json.dumps(doc), encoding="utf-8")
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = check_vectors.main(str(path))
    return code, out.getvalue()


class NegativeGrantSigningVectors(unittest.TestCase):
    def setUp(self):
        self.doc = json.loads(FIXTURE.read_text(encoding="utf-8"))
        self.vec = self.doc["grant_signing_vectors"][0]

    def test_the_committed_fixture_passes(self):
        code, out = run_checker(self.doc)
        self.assertEqual(code, 0, out)

    def test_a_stale_negative_value_fails_the_checker(self):
        for name, value in list(self.vec["must_not_verify"].items()):
            with self.subTest(name=name):
                doc = copy.deepcopy(self.doc)
                flipped = ("0" if value[0] != "0" else "1") + value[1:]
                doc["grant_signing_vectors"][0]["must_not_verify"][name] = flipped
                code, out = run_checker(doc)
                self.assertEqual(code, 1, "a corrupted negative vector passed the independent checker:\n" + out)

    def test_swapped_negative_values_fail_the_checker(self):
        doc = copy.deepcopy(self.doc)
        neg = doc["grant_signing_vectors"][0]["must_not_verify"]
        a, b = sorted(neg)
        neg[a], neg[b] = neg[b], neg[a]
        code, out = run_checker(doc)
        self.assertEqual(code, 1, out)

    def test_an_unknown_negative_name_fails_the_checker(self):
        doc = copy.deepcopy(self.doc)
        doc["grant_signing_vectors"][0]["must_not_verify"]["some_other_signature"] = "0" * 64
        code, out = run_checker(doc)
        self.assertEqual(code, 1, out)

    def test_a_missing_negative_fails_the_checker(self):
        doc = copy.deepcopy(self.doc)
        del doc["grant_signing_vectors"][0]["must_not_verify"]["bare_jcs_signature"]
        code, out = run_checker(doc)
        self.assertEqual(code, 1, out)


if __name__ == "__main__":
    unittest.main()
