"""The operator DSN step (deploy/compose/operator-dsn/init.py; ENV-04, A445, A489, A527; INV-29).

tenantinit registers the core-trust public key with `key_register`, which only its owner and the superuser may call (migration 0013). The login is the superuser's (the migration login's password
belongs to the migrate one-shot alone, A527). The DSN holds a password, so it is written by this one step as a file the tenant-init step can read and nothing else can. These tests run the real script
on real files (the target user is the current user, so no privilege is needed) and check that it fails closed. SYNTHETIC password."""
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "init.py"
PASSWORD = "synthetic" + "0123456789abcdef" * 2


def run(src: Path, out: Path, **extra: str) -> subprocess.CompletedProcess[str]:
    env = {"PATH": os.environ.get("PATH", ""), "OPERATOR_DSN_SRC": str(src), "OPERATOR_DSN_OUT": str(out), "OPERATOR_DSN_UID": str(os.getuid()), "OPERATOR_DSN_GID": str(os.getgid())}
    env.update(extra)
    return subprocess.run([sys.executable, "-I", str(SCRIPT)], env=env, capture_output=True, text=True, check=False)


class OperatorDSN(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.src, self.out = Path(self.tmp.name) / "src", Path(self.tmp.name) / "out"
        self.src.mkdir()
        self.out.mkdir()
        (self.src / "postgres_superuser_password").write_text(PASSWORD + "\n", encoding="utf-8")

    def test_the_dsn_is_written_at_0400_owned_by_the_target_user_and_names_the_superuser(self) -> None:
        r = run(self.src, self.out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        f = self.out / "dsn"
        st = f.stat()
        self.assertEqual(stat.S_IMODE(st.st_mode), 0o400)
        self.assertEqual(st.st_uid, os.getuid())
        self.assertEqual(f.read_text(encoding="utf-8"), f"postgres://postgres:{PASSWORD}@postgres:5432/noema\n")

    def test_it_never_prints_the_password(self) -> None:
        r = run(self.src, self.out)
        self.assertNotIn(PASSWORD, r.stdout + r.stderr)
        self.assertIn("dsn", r.stdout)

    def test_a_second_run_replaces_the_file(self) -> None:
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.assertEqual(stat.S_IMODE((self.out / "dsn").stat().st_mode), 0o400)

    def test_a_missing_or_unusable_password_writes_nothing(self) -> None:
        (self.src / "postgres_superuser_password").unlink()
        r = run(self.src, self.out)
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(list(self.out.iterdir()), [])
        for bad in ("short", "has space in it 0123456789", "has/slash/0123456789abcdef", "has@at-sign-0123456789ab", ""):
            (self.src / "postgres_superuser_password").write_text(bad + "\n", encoding="utf-8")
            r = run(self.src, self.out)
            self.assertNotEqual(r.returncode, 0, repr(bad))
            self.assertEqual(list(self.out.iterdir()), [], repr(bad))
            self.assertNotIn(bad.strip() or "x" * 40, r.stdout + r.stderr if bad.strip() else "")

    def test_a_folder_that_holds_anything_else_is_refused_and_left_alone(self) -> None:
        (self.out / "other").write_text("x", encoding="utf-8")
        r = run(self.src, self.out)
        self.assertNotEqual(r.returncode, 0)
        self.assertTrue((self.out / "other").exists())
        self.assertFalse((self.out / "dsn").exists())

    def test_the_target_user_must_not_be_root(self) -> None:
        for uid in ("0", "", "abc"):
            self.assertNotEqual(run(self.src, self.out, OPERATOR_DSN_UID=uid).returncode, 0, uid)
        self.assertEqual(list(self.out.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
