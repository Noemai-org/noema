"""The core's model-client key init step (deploy/compose/core-tls/init.py; A667, SEC-09, INV-29).

The core refuses its model-client private key unless the file is a regular file owned by the process user, mode 0400, no link (keyfiles.ReadFile; the reader is not changed). The key is a bind
of the host file `secrets/tls/core-client.key` (mode 0600, the host user), which Compose cannot change, so the 24 Nov stack-check ended with the core exiting at start. A one-shot step copies the
key to a volume owned by the core's user at mode 0400 and checks it by reading as that user. These tests run the real script on real files (the target user is the current user, so no privilege
is needed) and check that it fails closed. SYNTHETIC key text, never a real key."""
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "init.py"
PEM = "-----BEGIN PRIVATE KEY-----\nU1lOVEhFVElDLU5PVC1BLUtFWQ==\n-----END PRIVATE KEY-----\n"


def run(src: Path, out: Path, **extra: str) -> subprocess.CompletedProcess[str]:
    env = {"PATH": os.environ.get("PATH", ""), "CORE_TLS_SRC": str(src), "CORE_TLS_OUT": str(out), "CORE_TLS_UID": str(os.getuid()), "CORE_TLS_GID": str(os.getgid())}
    env.update(extra)
    return subprocess.run([sys.executable, "-I", str(SCRIPT)], env=env, capture_output=True, text=True, check=False)


class InitCoreTls(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.src, self.out = self.root / "core-client.key", self.root / "out"
        self.out.mkdir()
        self.src.write_text(PEM, encoding="utf-8")
        self.src.chmod(0o600)  # as on the host: the file the Compose bind shows

    def test_the_key_is_written_at_0400_owned_by_the_target_user_and_unchanged(self) -> None:
        r = run(self.src, self.out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        f = self.out / "core-client.key"
        st = f.stat()
        self.assertEqual(stat.S_IMODE(st.st_mode), 0o400)
        self.assertEqual(st.st_uid, os.getuid())
        self.assertTrue(stat.S_ISREG(st.st_mode) and not f.is_symlink())
        self.assertEqual(f.read_text(encoding="utf-8"), PEM)

    def test_it_never_prints_the_key(self) -> None:
        r = run(self.src, self.out)
        self.assertNotIn("U1lOVEhFVElDLU5PVC1BLUtFWQ", r.stdout + r.stderr)
        self.assertIn("core-client.key", r.stdout)

    def test_a_second_run_replaces_the_file_and_stays_at_0400(self) -> None:
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.src.write_text(PEM.replace("U1lO", "VkVSU"), encoding="utf-8")
        self.assertEqual(run(self.src, self.out).returncode, 0)
        f = self.out / "core-client.key"
        self.assertEqual(stat.S_IMODE(f.stat().st_mode), 0o400)
        self.assertIn("VkVSU", f.read_text(encoding="utf-8"))

    def test_a_source_that_is_not_a_private_key_pem_is_refused_and_leaves_nothing(self) -> None:
        for name, text in {"empty": "", "a certificate": "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n", "text": "not a key\n",
                           "two blocks": PEM + PEM,
                           "oversize": "-----BEGIN PRIVATE KEY-----\n" + ("QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB\n" * 400) + "-----END PRIVATE KEY-----\n"}.items():
            self.src.write_text(text, encoding="utf-8")
            r = run(self.src, self.out)
            self.assertNotEqual(r.returncode, 0, name)
            self.assertEqual(list(self.out.iterdir()), [], f"{name}: no file may be left behind")

    def test_a_missing_source_or_a_link_is_refused(self) -> None:
        self.src.unlink()
        self.assertNotEqual(run(self.src, self.out).returncode, 0)
        real = self.root / "real.key"
        real.write_text(PEM, encoding="utf-8")
        self.src.symlink_to(real)
        self.assertNotEqual(run(self.src, self.out).returncode, 0, "a symbolic link as the source is refused")
        self.assertEqual(list(self.out.iterdir()), [])

    def test_the_target_user_must_be_given_and_must_not_be_root(self) -> None:
        for bad in ("", "0", "root", "-1"):
            self.assertNotEqual(run(self.src, self.out, CORE_TLS_UID=bad).returncode, 0, bad)
        self.assertEqual(list(self.out.iterdir()), [])

    def test_the_output_directory_holds_only_the_key_this_step_writes(self) -> None:
        (self.out / "other.key").write_text("x", encoding="utf-8")
        self.assertNotEqual(run(self.src, self.out).returncode, 0, "a directory with another file is refused")

    def test_the_target_user_check_refuses_root_in_the_script_itself(self) -> None:
        # run as an unprivileged user a chown to uid 0 fails anyway, so this reads the function: a user id of 0 must be refused before anything is written
        import importlib.util
        for bad in ("0", "", "x"):
            os.environ["CORE_TLS_UID"] = bad
            spec = importlib.util.spec_from_file_location("core_tls_init", SCRIPT)
            assert spec and spec.loader
            mod = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(mod)
            with self.assertRaises(SystemExit):
                mod.ident("CORE_TLS_UID")
        del os.environ["CORE_TLS_UID"]


if __name__ == "__main__":
    unittest.main()
