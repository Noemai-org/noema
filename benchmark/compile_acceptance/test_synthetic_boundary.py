"""A604 (UND-18, run 7): the SYNTHETIC boundary recordings of the `requested_remedy` gate (`boundary-a604/`, made by `synthetic_boundary.py`).

What these tests hold: the committed recordings are exactly what the replay loader accepts; every one is labelled as synthetic (never as live output); each boundary's recording really carries the probability text
it claims; the expected outcome of each boundary follows from that text by the rule of A612 (checked here by hand-written cases, not by the generator's own function alone); the directory is scan-clean (REQ-SEC-003);
and the findings file lists every boundary, generated or skipped. The script itself needs the gate (UND-18) on the model service's path, so it is not run here."""
import json
import re
import subprocess
import sys
import unittest
from pathlib import Path
from typing import Any

from benchmark.compile_acceptance.replay import load_recordings
from benchmark.compile_acceptance.synthetic_boundary import BOUNDARIES, LONG_RUN, expected_bp, expected_passes

ROOT = Path(__file__).resolve().parents[2]
DIR = ROOT / "benchmark" / "compile_acceptance" / "boundary-a604"
RECORDINGS = DIR / "recordings"
FINDINGS = DIR / "boundary-findings.json"


def findings() -> dict[str, Any]:
    doc: dict[str, Any] = json.loads(FINDINGS.read_text(encoding="utf-8"))
    return doc


def gate_replies() -> dict[str, str]:
    """case id -> the text of the model reply that answers the gate question, from the committed recordings."""
    out: dict[str, str] = {}
    for path in sorted(RECORDINGS.glob("rec-*.json")):
        doc = json.loads(path.read_text(encoding="utf-8"))
        reply = json.dumps(doc["reply"])
        if "requested_remedy_gate" in reply and "probabilities" in reply:
            text = doc["reply"]["output"][-1]["content"][0]["text"]
            out[doc["case_id"]] = text
    return out


class Expected(unittest.TestCase):
    """The rule of A612, by hand: a plain decimal in [0, 1] floors to whole basis points and is compared with 9000; anything else, a missing value included, abstains."""

    def test_the_expected_basis_points_of_the_written_text(self) -> None:
        for text, bp in (("0.8999", 8999), ("0.89999", 8999), ("0.9", 9000), ("0.90", 9000), ("0.9000", 9000), ("0.90001", 9000), ("0.9001", 9001), ("0.5", 5000), ("0", 0), ("1", 10_000), ("1.0", 10_000)):
            self.assertEqual(expected_bp(text), bp, text)
        bad: tuple[str | None, ...] = ("1.00009", "1e-05", "9e-1", "-0.1", "NaN", "Infinity", "null", '"0.95"', "", ".9", "0.9 ", None)
        for written in bad:
            self.assertIsNone(expected_bp(written), repr(written))

    def test_a_no_is_a_no_and_doubt_abstains_at_the_threshold(self) -> None:
        by_id = {b.boundary_id: b for b in BOUNDARIES}
        self.assertFalse(expected_passes(by_id["B01"]))   # one basis point below
        self.assertTrue(expected_passes(by_id["B03"]))    # exactly at it
        self.assertTrue(expected_passes(by_id["B07"]))    # one above
        self.assertFalse(expected_passes(by_id["B21"]))   # the model's own no, whatever the probability says
        self.assertFalse(expected_passes(by_id["B22"]))   # a yes at 0.6 is doubt

    def test_every_boundary_has_its_own_id_and_a_reason(self) -> None:
        ids = [b.boundary_id for b in BOUNDARIES]
        self.assertEqual(len(ids), len(set(ids)))
        self.assertTrue(all(b.why.strip() for b in BOUNDARIES))


class Recordings(unittest.TestCase):
    def test_the_replay_loader_accepts_the_directory(self) -> None:
        recordings = load_recordings(RECORDINGS)
        self.assertGreater(len(recordings.recordings), 100)

    def test_every_recording_and_the_findings_say_synthetic_and_none_says_live(self) -> None:
        for path in sorted(RECORDINGS.glob("rec-*.json")):
            doc = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(doc["run_id"], "synthetic-boundary", path.name)
            self.assertEqual(doc["recorded_at"], "2026-11-24T00:00:00Z", path.name)
        self.assertEqual(json.loads((RECORDINGS / "manifest.json").read_text(encoding="utf-8"))["run_id"], "synthetic-boundary")
        self.assertIn("SYNTHETIC", findings()["label"])
        self.assertIn("not live", findings()["label"])
        readme = (DIR / "README.md").read_text(encoding="utf-8")
        self.assertIn("SYNTHETIC", readme)
        self.assertIn("not live", readme)

    def test_each_generated_boundary_recorded_the_probability_text_it_claims(self) -> None:
        replies = gate_replies()
        by_id = {b.boundary_id: b for b in BOUNDARIES}
        generated = [f for f in findings()["boundaries"] if "agree" in f]
        self.assertEqual(len(replies), len(generated))
        for f in generated:
            b = by_id[f["boundary"]]
            text = replies[f["case_id"]]
            if b.written is None:
                self.assertNotIn('"p"', text.split('"no"')[0].split('"yes"')[-1], b.boundary_id)  # the yes entry carries no p
            else:
                self.assertIn(f'"option": "yes", "p": {b.written}}}', text, b.boundary_id)
            self.assertIn(f'"selected": "{b.selected}"', text, b.boundary_id)

    def test_the_directory_is_scan_clean(self) -> None:
        done = subprocess.run([sys.executable, "-I", str(ROOT / "tools" / "ci" / "leakscan.py"), "--allow", str(ROOT / "benchmark" / "compile_acceptance" / "leakscan.allow"), str(DIR)],
                              capture_output=True, text=True, check=False)
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)


class Findings(unittest.TestCase):
    def test_every_boundary_is_listed_once_as_generated_or_skipped(self) -> None:
        rows = findings()["boundaries"]
        self.assertEqual([r["boundary"] for r in rows], [b.boundary_id for b in BOUNDARIES])
        for r, b in zip(rows, BOUNDARIES, strict=True):
            if "skipped" in r:
                self.assertTrue(b.written is not None and LONG_RUN.search(b.written), b.boundary_id)  # only a long digit run is left out
            else:
                self.assertEqual(r["written"], b.written)
                self.assertEqual(r["expected"], "PRESENT" if expected_passes(b) else "ABSENT", b.boundary_id)
                self.assertEqual(r["agree"], r["implementation"] == r["expected"], b.boundary_id)

    def test_the_disagreements_are_stated_not_hidden(self) -> None:
        rows = [r for r in findings()["boundaries"] if r.get("agree") is False]
        self.assertTrue(rows)  # run 7's gate refuses a malformed reply as a whole and reads an exponent as the float it parses to; the file says so
        text = (DIR / "README.md").read_text(encoding="utf-8")
        for r in rows:
            self.assertIn(r["boundary"], text, r["boundary"])
        self.assertTrue(re.search(r"disagree", text))


if __name__ == "__main__":
    unittest.main()
