"""A604 (UND-18, run 7): the SYNTHETIC boundary recordings of the `requested_remedy` gate (`boundary-a604/`, made by `synthetic_boundary.py`).

What these tests hold: the committed recordings are exactly what the replay loader accepts; every one is labelled as synthetic (never as live output); each boundary's recording really carries the probability text
it claims; the expected outcome of each boundary follows from that text by the rule of A612 (checked here by hand-written cases, not by the generator's own function alone); the directory is scan-clean (REQ-SEC-003);
and the findings file lists every boundary, generated or skipped. The script itself runs in one test, into a temporary directory, for the three boundaries that cannot be committed."""
import importlib.util
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.compile_acceptance.replay import ReplayRefused, ReplayStore, load_recordings
from benchmark.compile_acceptance.synthetic_boundary import BOUNDARIES, LONG_RUN, expected_bp, expected_passes

ROOT = Path(__file__).resolve().parents[2]
DIR = ROOT / "benchmark" / "compile_acceptance" / "boundary-a604"
RECORDINGS = DIR / "fixture"  # the reduced run (fixtures.reduce_run), flat: manifest.json and rec-NNNN.json only; the findings file sits beside it, outside
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


class Replay(unittest.TestCase):
    """What the replay provider does with this directory (Codex on #1010): it serves a usable recording by the SHA-256 of its request, and refuses a recording of a failure as EVIDENCE_ONLY. The five malformed
    boundaries (B16 to B20) are recordings of a reply the harness refuses as a whole, so replay refuses them by design; their compile outcomes are exercised offline by the generator (scripted inner transport,
    through the real pipeline), whose agreement with A612 `Generated.test_the_three_long_digit_run_boundaries_are_generated_here_and_never_committed_a604` and the findings file check."""

    MALFORMED = ("B16", "B17", "B18", "B19", "B20")

    def gate_recordings(self) -> dict[str, str]:
        """boundary id -> the recording id of its gate question."""
        by_case = {f["case_id"]: f["boundary"] for f in findings()["boundaries"] if "case_id" in f}
        out: dict[str, str] = {}
        for path in sorted(RECORDINGS.glob("rec-*.json")):
            doc = json.loads(path.read_text(encoding="utf-8"))
            reply = json.dumps(doc["reply"])
            if "requested_remedy_gate" in reply and "probabilities" in reply:
                out[by_case[doc["case_id"]]] = path.stem
        return out

    def test_replay_serves_every_boundary_but_the_malformed_five_and_refuses_those_as_evidence_only(self) -> None:
        recordings = load_recordings(RECORDINGS)
        store = ReplayStore(recordings)
        by_id = {r.recording_id: r for r in recordings.recordings}
        gate = self.gate_recordings()
        self.assertEqual(len(gate), 19)
        for boundary, recording_id in gate.items():
            if boundary in self.MALFORMED:
                with self.assertRaises(ReplayRefused, msg=boundary) as caught:
                    store.lookup(by_id[recording_id].request_sha256)
                self.assertEqual(caught.exception.code, "EVIDENCE_ONLY", boundary)
            else:
                self.assertIsInstance(store.lookup(by_id[recording_id].request_sha256), dict, boundary)
        unusable = sorted(r.recording_id for r in recordings.recordings if not r.labels.usable_as_fixture)
        self.assertEqual(unusable, sorted(gate[b] for b in self.MALFORMED))  # nothing else in the directory is evidence-only

    def test_the_readme_names_the_five_and_says_where_to_exercise_them(self) -> None:
        text = (DIR / "README.md").read_text(encoding="utf-8")
        for boundary in self.MALFORMED:
            self.assertIn(boundary, text)
        self.assertIn("EVIDENCE_ONLY", text)
        self.assertIn("synthetic_boundary", text)


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

    def test_the_gate_agrees_with_a612_at_every_boundary_and_the_file_says_so(self) -> None:
        rows = [r for r in findings()["boundaries"] if "agree" in r]
        self.assertTrue(rows)
        self.assertEqual([r["boundary"] for r in rows if r["agree"] is False], [])  # A649: an exponent form is malformed and the gate abstains; no disagreement is left to hide
        text = (DIR / "README.md").read_text(encoding="utf-8")
        self.assertIn("0 disagreements", text)
        self.assertIn("A649", text)


class Fixture(unittest.TestCase):
    def test_the_directory_is_flat_and_every_file_says_synthetic_in_its_fixture_block(self) -> None:
        names = sorted(p.name for p in RECORDINGS.iterdir())
        self.assertTrue(all(n == "manifest.json" or re.fullmatch(r"rec-\d{4}\.json", n) for n in names), names)  # what ReplayTransport reads: no sub-directory, no findings file
        for name in names:
            doc = json.loads((RECORDINGS / name).read_text(encoding="utf-8"))
            self.assertIs(doc["fixture"]["synthetic"], True, name)
            self.assertEqual(doc["fixture"]["run_id"], "synthetic-boundary", name)
        self.assertFalse((RECORDINGS / "boundary-findings.json").exists())

    @unittest.skipIf(importlib.util.find_spec("pydantic") is None, "the model service's dependencies are not installed here (the benchmark job installs benchmark/requirements.lock only); run with services/model/requirements.lock too")
    def test_the_three_long_digit_run_boundaries_are_generated_here_and_never_committed_a604(self) -> None:
        """B02, B06 and B12 write five or more digits in a row, which the leak rule flags in any committed file, so they are generated into a temporary directory, here, and checked against A612's rule."""
        with tempfile.TemporaryDirectory() as tmp:
            done = subprocess.run([sys.executable, "-c", "import sys; from benchmark.compile_acceptance.synthetic_boundary import main; sys.exit(main(['--out', sys.argv[1], '--include-long-digit-runs']))", tmp],
                                  capture_output=True, text=True, check=False, cwd=ROOT, env={**os.environ, "PYTHONPATH": f"{ROOT / 'services' / 'model'}:{ROOT}"})
            self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
            self.assertIn("22 boundaries generated, 0 skipped, 0 disagreement(s)", done.stdout)
            rows = json.loads((Path(tmp) / "boundary-findings.json").read_text(encoding="utf-8"))["boundaries"]
            self.assertEqual({r["boundary"] for r in rows if r["boundary"] in ("B02", "B06", "B12")}, {"B02", "B06", "B12"})
            self.assertTrue(all(r["agree"] for r in rows))


if __name__ == "__main__":
    unittest.main()
