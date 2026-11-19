"""The grant-key init step (deploy/compose/grant-keys/init.py; SEC-09, INV-29; Codex and SR on noema#700).

Compose cannot set a mode or an owner on a file secret (it bind-mounts the host file), and the grant-key loader refuses a file that is not mode 0400 and owned by the process user.
So a one-shot step copies the two key secrets to a volume as grant.hmac.<version>.key, mode 0400, owned by the core's user, and checks that it did, reading each file as that user.
These tests run the real script on real files (the target user is the current user, so no privilege is needed) and check that it also fails closed. SYNTHETIC keys."""
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "init.py"
KEY_A, KEY_B = "a1" * 32, "b2" * 32


def run(src: Path, out: Path, **extra: str) -> subprocess.CompletedProcess[str]:
    env = {"PATH": os.environ.get("PATH", ""), "GRANT_KEYS_SRC": str(src), "GRANT_KEYS_OUT": str(out), "GRANT_KEYS_UID": str(os.getuid()), "GRANT_KEYS_GID": str(os.getgid())}
    env.update(extra)
    return subprocess.run([sys.executable, "-I", str(SCRIPT)], env=env, capture_output=True, text=True, check=False)


class InitGrantKeys(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.src, self.out = Path(self.tmp.name) / "src", Path(self.tmp.name) / "out"
        self.src.mkdir()
        self.out.mkdir()

    def put(self, name: str, value: str) -> None:
        (self.src / name).write_text(value + "\n", encoding="utf-8")

    def good(self) -> None:
        self.put("grant.hmac.1.key", KEY_A)
        self.put("grant.hmac.2.key", KEY_B)

    def test_both_keys_are_written_at_0400_owned_by_the_target_user_and_unchanged(self) -> None:
        self.good()
        r = run(self.src, self.out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        for name, value in (("grant.hmac.1.key", KEY_A), ("grant.hmac.2.key", KEY_B)):
            f = self.out / name
            st = f.stat()
            self.assertEqual(stat.S_IMODE(st.st_mode), 0o400, name)
            self.assertEqual(st.st_uid, os.getuid(), name)
            self.assertEqual(f.read_text(encoding="utf-8").strip(), value, name)

    def test_it_never_prints_a_key(self) -> None:
        self.good()
        r = run(self.src, self.out)
        self.assertNotIn(KEY_A, r.stdout + r.stderr)
        self.assertNotIn(KEY_B, r.stdout + r.stderr)
        self.assertIn("grant.hmac.1.key", r.stdout)

    def test_a_second_run_replaces_the_files_and_stays_at_0400(self) -> None:
        self.good()
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.put("grant.hmac.2.key", "c3" * 32)
        r = run(self.src, self.out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(stat.S_IMODE((self.out / "grant.hmac.2.key").stat().st_mode), 0o400)
        self.assertEqual((self.out / "grant.hmac.2.key").read_text(encoding="utf-8").strip(), "c3" * 32)

    def test_it_fails_closed(self) -> None:
        cases = {
            "a missing key": lambda: self.put("grant.hmac.1.key", KEY_A),
            "a short key": lambda: (self.put("grant.hmac.1.key", KEY_A), self.put("grant.hmac.2.key", "ab" * 10)),
            "a key that is not hexadecimal": lambda: (self.put("grant.hmac.1.key", KEY_A), self.put("grant.hmac.2.key", "zz" * 32)),
            "the same key twice": lambda: (self.put("grant.hmac.1.key", KEY_A), self.put("grant.hmac.2.key", KEY_A)),
            "an empty key": lambda: (self.put("grant.hmac.1.key", KEY_A), self.put("grant.hmac.2.key", "")),
        }
        for name, make in cases.items():
            with self.subTest(name):
                for f in list(self.src.iterdir()) + list(self.out.iterdir()):
                    f.chmod(0o600)
                    f.unlink()
                make()
                r = run(self.src, self.out)
                self.assertNotEqual(r.returncode, 0, name)
                self.assertEqual(list(self.out.iterdir()), [], f"{name}: a key was left behind")
                self.assertNotIn(KEY_A, r.stdout + r.stderr)

    def test_a_bad_target_user_is_refused(self) -> None:
        self.good()
        for bad in ("0", "root", "-1", ""):
            with self.subTest(bad):
                self.assertNotEqual(run(self.src, self.out, GRANT_KEYS_UID=bad).returncode, 0)


if __name__ == "__main__":
    unittest.main()
