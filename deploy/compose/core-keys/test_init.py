"""The core's key-file init step (deploy/compose/core-keys/init.py; A506, A469; INV-29, AGENT-16, AGENT-13).

The core loads the orchestrator's signing key (`core-workflows.ed25519.1.key`), the gateway's (`core-edge.ed25519.1.key`) and the input hash key (one file) from three directories. Each file is a
64-hexadecimal-character value with at most one trailing newline, mode exactly 0400, owned by the core's user (A506); Compose cannot give a file secret that mode or owner. So a one-shot step copies
three file secrets into three volumes, and checks each by reading it as that user. These tests run the real script on real files (the target user is the current user, so no privilege is needed) and
check that it fails closed and never prints a value. SYNTHETIC values."""
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "init.py"
A, B, C = "a1" * 32, "b2" * 32, "c3" * 32


class InitCoreKeys(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.src = root / "src"
        self.src.mkdir()
        self.dirs = {n: root / n for n in ("orch", "edge", "hash")}
        for d in self.dirs.values():
            d.mkdir()
        self.files = {"core_workflows_ed25519_seed_1": ("orch", "core-workflows.ed25519.1.key"), "core_edge_ed25519_seed_1": ("edge", "core-edge.ed25519.1.key"), "input_hash_key": ("hash", "key")}
        self.good()

    def put(self, name: str, value: str) -> None:
        (self.src / name).write_text(value + "\n", encoding="utf-8")

    def good(self) -> None:
        self.put("core_workflows_ed25519_seed_1", A)
        self.put("core_edge_ed25519_seed_1", B)
        self.put("input_hash_key", C)

    def run_step(self, **extra: str) -> subprocess.CompletedProcess[str]:
        spec = " ".join(f"{name}={self.dirs[d]}/{fname}" for name, (d, fname) in self.files.items())
        env = {"PATH": os.environ.get("PATH", ""), "CORE_KEYS_SRC": str(self.src), "CORE_KEYS_FILES": spec, "CORE_KEYS_UID": str(os.getuid()), "CORE_KEYS_GID": str(os.getgid())}
        env.update(extra)
        return subprocess.run([sys.executable, "-I", str(SCRIPT)], env=env, capture_output=True, text=True, check=False)

    def clear(self) -> None:
        for d in self.dirs.values():
            for f in list(d.iterdir()):
                f.chmod(0o600)
                f.unlink()

    def test_each_value_is_written_to_its_own_directory_at_0400_owned_by_the_target_user_and_unchanged(self) -> None:
        r = self.run_step()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        for name, value in (("core_workflows_ed25519_seed_1", A), ("core_edge_ed25519_seed_1", B), ("input_hash_key", C)):
            d, fname = self.files[name]
            self.assertEqual([p.name for p in self.dirs[d].iterdir()], [fname])
            f = self.dirs[d] / fname
            st = f.stat()
            self.assertEqual(stat.S_IMODE(st.st_mode), 0o400, fname)
            self.assertEqual(st.st_uid, os.getuid(), fname)
            self.assertEqual(f.read_text(encoding="utf-8"), value + "\n", fname)

    def test_it_never_prints_a_value_and_names_the_files(self) -> None:
        r = self.run_step()
        for value in (A, B, C):
            self.assertNotIn(value, r.stdout + r.stderr)
        self.assertIn("core-edge.ed25519.1.key", r.stdout)

    def test_a_second_run_replaces_the_files_and_stays_at_0400(self) -> None:
        self.assertEqual(self.run_step().returncode, 0)
        self.put("input_hash_key", "d4" * 32)
        r = self.run_step()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        f = self.dirs["hash"] / "key"
        self.assertEqual(stat.S_IMODE(f.stat().st_mode), 0o400)
        self.assertEqual(f.read_text(encoding="utf-8"), "d4" * 32 + "\n")

    def test_it_fails_closed_and_leaves_no_file_in_any_directory(self) -> None:
        cases = {
            "a missing value": lambda: (self.src / "input_hash_key").unlink(),
            "a short value": lambda: self.put("core_edge_ed25519_seed_1", "ab" * 10),
            "a value that is not hexadecimal": lambda: self.put("core_edge_ed25519_seed_1", "zz" * 32),
            "an upper-case value": lambda: self.put("core_edge_ed25519_seed_1", "AB" * 32),
            "an empty value": lambda: self.put("input_hash_key", ""),
            "two values the same": lambda: self.put("core_edge_ed25519_seed_1", A),
            "the hash key the same as a signing seed": lambda: self.put("input_hash_key", B),
        }
        for name, make in cases.items():
            with self.subTest(name):
                self.clear()
                self.good()
                make()
                r = self.run_step()
                self.assertNotEqual(r.returncode, 0, name)
                for d in self.dirs.values():
                    self.assertEqual(list(d.iterdir()), [], f"{name}: a file was left behind")
                for value in (A, B, C):
                    self.assertNotIn(value, r.stdout + r.stderr)

    def test_a_directory_that_holds_anything_else_stops_the_step(self) -> None:
        # The loader reads every *.key file of its directory (a key of another version or identity is an error or a second active key), so a foreign entry is refused, never left beside the new key.
        for make in (lambda: (self.dirs["orch"] / "core-workflows.ed25519.2.key").write_text("x", encoding="utf-8"), lambda: (self.dirs["edge"] / "stray.txt").write_text("x", encoding="utf-8"),
                     lambda: (self.dirs["hash"] / "sub").mkdir()):
            with self.subTest():
                self.clear()
                for d in self.dirs.values():
                    for p in list(d.iterdir()):
                        if p.is_dir():
                            p.rmdir()
                make()
                r = self.run_step()
                self.assertNotEqual(r.returncode, 0)
                for value in (A, B, C):
                    self.assertNotIn(value, r.stdout + r.stderr)
                for d in self.dirs.values():
                    for p in list(d.iterdir()):
                        if p.is_dir():
                            p.rmdir()

    def test_a_bad_target_user_or_a_bad_file_spec_is_refused(self) -> None:
        for extra in ({"CORE_KEYS_UID": "0"}, {"CORE_KEYS_UID": "root"}, {"CORE_KEYS_FILES": ""}, {"CORE_KEYS_FILES": "input_hash_key=relative/key"}, {"CORE_KEYS_FILES": f"other={self.dirs['hash']}/key"},
                      {"CORE_KEYS_FILES": f"input_hash_key={self.dirs['hash']}/../key"}):
            with self.subTest(extra):
                self.assertNotEqual(self.run_step(**extra).returncode, 0, extra)
                for d in self.dirs.values():
                    self.assertEqual(list(d.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
