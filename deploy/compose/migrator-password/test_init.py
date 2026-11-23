"""The migrator password step (deploy/compose/migrator-password/init.py; ENV-04, A521, A527; INV-29).

cmd/migrate takes the migration login's password from a FILE and refuses one that is not mode 0400, owned by the user it runs as, holding one line. Compose cannot give a file secret that mode or
owner, so this one step writes it. These tests run the real script on real files (the target user is the current user, so no privilege is needed) and check that it fails closed. SYNTHETIC password."""
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "init.py"
PASSWORD = "synthetic" + "fedcba9876543210" * 2


def run(src: Path, out: Path, **extra: str) -> subprocess.CompletedProcess[str]:
    env = {"PATH": os.environ.get("PATH", ""), "MIGRATOR_PASSWORD_SRC": str(src), "MIGRATOR_PASSWORD_OUT": str(out), "MIGRATOR_PASSWORD_UID": str(os.getuid()), "MIGRATOR_PASSWORD_GID": str(os.getgid())}
    env.update(extra)
    return subprocess.run([sys.executable, "-I", str(SCRIPT)], env=env, capture_output=True, text=True, check=False)


class MigratorPassword(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.src, self.out = Path(self.tmp.name) / "src", Path(self.tmp.name) / "out"
        self.src.mkdir()
        self.out.mkdir()
        (self.src / "noema_migrator_password").write_text(PASSWORD + "\n", encoding="utf-8")

    def test_the_password_is_one_line_at_0400_owned_by_the_target_user(self) -> None:
        r = run(self.src, self.out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        f = self.out / "password"
        st = f.stat()
        self.assertEqual(stat.S_IMODE(st.st_mode), 0o400)
        self.assertEqual(st.st_uid, os.getuid())
        self.assertEqual(f.read_text(encoding="utf-8"), PASSWORD + "\n")

    def test_a_libpq_password_file_is_written_beside_it_for_the_provision_one_shot(self) -> None:
        # provision-tenants.sh runs as the migration login and passes no password: PGPASSFILE names this file (mode 0400, owner the target user, one line).
        self.assertEqual(run(self.src, self.out).returncode, 0)
        f = self.out / "pgpass"
        st = f.stat()
        self.assertEqual(stat.S_IMODE(st.st_mode), 0o400)
        self.assertEqual(st.st_uid, os.getuid())
        self.assertEqual(f.read_text(encoding="utf-8"), f"postgres:5432:noema:noema_migration_login:{PASSWORD}\n")

    def test_it_never_prints_the_password(self) -> None:
        r = run(self.src, self.out)
        self.assertNotIn(PASSWORD, r.stdout + r.stderr)
        self.assertIn("password", r.stdout)

    def test_a_second_run_replaces_the_file(self) -> None:
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.assertEqual(stat.S_IMODE((self.out / "password").stat().st_mode), 0o400)

    def test_a_missing_or_unusable_password_writes_nothing(self) -> None:
        (self.src / "noema_migrator_password").unlink()
        self.assertNotEqual(run(self.src, self.out).returncode, 0)
        self.assertEqual(list(self.out.iterdir()), [])
        for bad in ("short", "has space in it 0123456789", "line1-0123456789abcdef\nline2-0123456789abcdef", ""):
            (self.src / "noema_migrator_password").write_text(bad + "\n", encoding="utf-8")
            self.assertNotEqual(run(self.src, self.out).returncode, 0, repr(bad))
            self.assertEqual(list(self.out.iterdir()), [], repr(bad))

    def test_a_folder_that_holds_anything_else_is_refused_and_left_alone(self) -> None:
        (self.out / "other").write_text("x", encoding="utf-8")
        self.assertNotEqual(run(self.src, self.out).returncode, 0)
        self.assertTrue((self.out / "other").exists())
        self.assertFalse((self.out / "password").exists())

    def test_the_target_user_must_not_be_root(self) -> None:
        for uid in ("0", "", "abc"):
            self.assertNotEqual(run(self.src, self.out, MIGRATOR_PASSWORD_UID=uid).returncode, 0, uid)
        self.assertEqual(list(self.out.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
