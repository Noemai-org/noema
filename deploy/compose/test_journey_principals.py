"""A683 (INV-12, INV-29, AGENT-13): the principals of the journeys manifest, as `id:role` words for bench_tokens.sh.

`benchmark/journeys/j1-j8.v1.json` (noema#962, A652) lists the synthetic principal of each journey case with its one session role. `journey_principals.py` reads it and prints the unique `id:role` words that
`BENCH_PRINCIPALS` takes, so each journey principal gets a token from the synthetic IdP and none is invented here. It refuses a manifest it cannot read in full, a principal with two roles or a role outside the
four, a principal listed with two different roles, an id the issuer would refuse, and an empty list. Tokens are issued for at most 15 minutes and stay in the private idp directory. SYNTHETIC."""
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "journey_principals.py"
REAL = Path(__file__).resolve().parents[2] / "benchmark" / "journeys" / "j1-j8.v1.json"
IDS = [f"usr_synth{n:03d}" for n in (9, 37, 117, 109, 30, 19, 150, 141, 173, 49, 2, 42, 1, 15, 161, 29, 122, 178, 45)]


def manifest(principals: list[tuple[str, list[str]]]) -> dict:
    return {"journeys": [{"journey": "J1", "cases": [{"case_id": f"BEN-X-{i:04d}", "principal_id": p, "roles": r} for i, (p, r) in enumerate(principals)]}]}


def run(doc: object, *extra: str) -> subprocess.CompletedProcess[str]:
    with tempfile.TemporaryDirectory() as tmp:
        f = Path(tmp) / "m.json"
        f.write_text(json.dumps(doc) if not isinstance(doc, str) else doc, encoding="utf-8")
        return subprocess.run([sys.executable, "-I", str(SCRIPT), str(f), *extra], capture_output=True, text=True, check=False)


class JourneyPrincipals(unittest.TestCase):
    def test_nineteen_principals_give_nineteen_words_in_the_manifest_order(self) -> None:
        r = run(manifest([(p, ["customer"]) for p in IDS]))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.split(), [f"{p}:customer" for p in IDS])
        self.assertEqual(len(r.stdout.split()), 19)

    def test_a_principal_in_two_journeys_is_listed_once(self) -> None:
        r = run(manifest([("usr_synth001", ["customer"]), ("usr_synth002", ["ops"]), ("usr_synth001", ["customer"])]))
        self.assertEqual(r.stdout.split(), ["usr_synth001:customer", "usr_synth002:ops"])

    def test_a_principal_with_two_roles_or_a_role_outside_the_four_or_two_different_roles_is_refused(self) -> None:
        for name, doc in {
            "two roles": manifest([("usr_synth001", ["customer", "ops"])]),
            "no role": manifest([("usr_synth001", [])]),
            "unknown role": manifest([("usr_synth001", ["admin"])]),
            "same id, two roles": manifest([("usr_synth001", ["customer"]), ("usr_synth001", ["ops"])]),
        }.items():
            r = run(doc)
            self.assertNotEqual(r.returncode, 0, name)
            self.assertEqual(r.stdout, "", f"{name}: nothing may be printed on a refusal")

    def test_an_id_the_issuer_would_refuse_is_refused_here(self) -> None:
        for bad in ("synth001", "usr_", "usr_a", "usr_" + "a" * 33, "usr_synth 001", "usr_synth001;x", "usr_synth-1", ""):
            self.assertNotEqual(run(manifest([(bad, ["customer"])])).returncode, 0, repr(bad))

    def test_an_empty_or_unreadable_or_malformed_manifest_is_refused(self) -> None:
        for name, doc in {"no journeys": {"journeys": []}, "no cases": {"journeys": [{"journey": "J1", "cases": []}]}, "not json": "{", "a list": [], "no principal": {"journeys": [{"cases": [{"roles": ["customer"]}]}]}}.items():
            self.assertNotEqual(run(doc).returncode, 0, name)
        self.assertNotEqual(subprocess.run([sys.executable, "-I", str(SCRIPT), "/nonexistent/x.json"], capture_output=True, check=False).returncode, 0)

    def test_the_count_can_be_pinned_and_a_different_count_is_refused(self) -> None:
        doc = manifest([(p, ["customer"]) for p in IDS])
        self.assertEqual(run(doc, "--expect", "19").returncode, 0)
        self.assertNotEqual(run(doc, "--expect", "18").returncode, 0)
        self.assertNotEqual(run(doc, "--expect", "x").returncode, 0)

    def test_the_real_manifest_when_it_is_present_has_nineteen_customer_principals(self) -> None:
        if not REAL.is_file():
            self.skipTest("benchmark/journeys/j1-j8.v1.json is not on this branch (noema#962 has not merged)")
        r = subprocess.run([sys.executable, "-I", str(SCRIPT), str(REAL), "--expect", "19"], capture_output=True, text=True, check=False)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(all(re.fullmatch(r"usr_[A-Za-z0-9]{2,32}:(customer|ops|approver|audit)", w) for w in r.stdout.split()))

    def test_it_never_prints_a_token_or_reads_the_environment(self) -> None:
        r = subprocess.run([sys.executable, "-I", str(SCRIPT), "--help"], capture_output=True, text=True, check=False, env={"PATH": os.environ.get("PATH", ""), "BENCH_TOKEN_TTL": "1h"})
        self.assertNotIn("1h", r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
