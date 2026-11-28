"""The Trust login DSN init step (deploy/compose/trust-dsn/init.py; A356, INV-29, AGENT-16; noema#730 follow-up).

The core reads NOEMA_TRUST_DSN_DIR: one regular file per tenant, named by the tenant ID, holding that tenant's `trust_rw__<tenant>` login as a DSN. A DSN holds a password, so the files are
secrets: this one-shot step builds them from the existing file secret `noema_tenant_login_password` and the tenant list, at mode 0400 owned by the core's user, and checks that it did.
These tests run the real script on real files (the target user is the current user, so no privilege is needed) and check that it fails closed and never prints a password. SYNTHETIC values."""
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "init.py"
PASSWORD = "c0ffee" * 8  # SYNTHETIC


def run(src: Path, out: Path, **extra: str) -> subprocess.CompletedProcess[str]:
    env = {"PATH": os.environ.get("PATH", ""), "TRUST_DSN_SRC": str(src), "TRUST_DSN_OUT": str(out), "TRUST_DSN_UID": str(os.getuid()), "TRUST_DSN_GID": str(os.getgid()), "TRUST_DSN_TENANTS": "ten_aa ten_bb"}
    env.update(extra)
    return subprocess.run([sys.executable, "-I", str(SCRIPT)], env=env, capture_output=True, text=True, check=False)


class InitTrustDsn(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.src, self.out = Path(self.tmp.name) / "src", Path(self.tmp.name) / "out"
        self.src.mkdir()
        self.out.mkdir()
        self.put(PASSWORD)

    def put(self, value: str) -> None:
        (self.src / "noema_tenant_login_password").write_text(value + "\n", encoding="utf-8")

    def clear(self) -> None:
        for f in self.out.iterdir():
            f.chmod(0o600)
            f.unlink()

    def test_one_file_per_tenant_named_by_the_tenant_holding_that_tenants_trust_login_at_0400(self) -> None:
        r = run(self.src, self.out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(sorted(p.name for p in self.out.iterdir()), ["ten_aa", "ten_bb"])
        for tenant in ("ten_aa", "ten_bb"):
            f = self.out / tenant
            st = f.stat()
            self.assertEqual(stat.S_IMODE(st.st_mode), 0o400, tenant)
            self.assertEqual(st.st_uid, os.getuid(), tenant)
            self.assertEqual(f.read_text(encoding="utf-8").strip(), f"postgres://trust_rw__{tenant}:{PASSWORD}@postgres:5432/noema", tenant)

    def test_each_file_names_its_own_tenants_login_and_no_other(self) -> None:
        run(self.src, self.out)
        a, b = (self.out / "ten_aa").read_text(encoding="utf-8"), (self.out / "ten_bb").read_text(encoding="utf-8")
        self.assertIn("trust_rw__ten_aa", a)
        self.assertNotIn("ten_bb", a)
        self.assertIn("trust_rw__ten_bb", b)
        self.assertNotIn("ten_aa", b)

    def test_it_never_prints_the_password_and_names_the_files(self) -> None:
        r = run(self.src, self.out)
        self.assertNotIn(PASSWORD, r.stdout + r.stderr)
        self.assertIn("ten_aa", r.stdout)

    def test_a_second_run_replaces_the_files_and_stays_at_0400(self) -> None:
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.put("f00dfeed" * 6)
        r = run(self.src, self.out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(stat.S_IMODE((self.out / "ten_aa").stat().st_mode), 0o400)
        self.assertIn("f00dfeed" * 6, (self.out / "ten_aa").read_text(encoding="utf-8"))

    def test_it_fails_closed_and_leaves_nothing_behind(self) -> None:
        cases = {
            "no tenant": {"TRUST_DSN_TENANTS": ""},
            "a tenant that is not a tenant ID": {"TRUST_DSN_TENANTS": "ten_aa bad"},
            "a tenant ID that is too short": {"TRUST_DSN_TENANTS": "ten_a"},
            "a tenant with a path in it": {"TRUST_DSN_TENANTS": "ten_aa ../ten_bb"},
            "the same tenant twice": {"TRUST_DSN_TENANTS": "ten_aa ten_aa"},
            "a bad target user": {"TRUST_DSN_UID": "0"},
            "a target user that is not a number": {"TRUST_DSN_UID": "root"},
        }
        for name, extra in cases.items():
            with self.subTest(name):
                self.clear()
                r = run(self.src, self.out, **extra)
                self.assertNotEqual(r.returncode, 0, name)
                self.assertEqual(list(self.out.iterdir()), [], f"{name}: a file was left behind")
                self.assertNotIn(PASSWORD, r.stdout + r.stderr)

    def test_a_tenant_dropped_from_the_list_leaves_no_file_behind(self) -> None:
        # C1 on noema#790 (P3): the volume is persistent and the core builds a pool for every file in it, so a tenant removed from the list must not keep its DSN file.
        self.assertEqual(run(self.src, self.out).returncode, 0)
        self.assertEqual(sorted(p.name for p in self.out.iterdir()), ["ten_aa", "ten_bb"])
        r = run(self.src, self.out, TRUST_DSN_TENANTS="ten_aa")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(sorted(p.name for p in self.out.iterdir()), ["ten_aa"], "ten_bb's DSN file was kept")
        self.assertNotIn("ten_bb", r.stdout + r.stderr)

    def test_an_entry_that_is_not_a_tenant_file_stops_the_step(self) -> None:
        for make in (lambda: (self.out / "stray.txt").write_text("x", encoding="utf-8"), lambda: (self.out / "ten_a").write_text("x", encoding="utf-8"), lambda: (self.out / "sub").mkdir()):
            with self.subTest():
                self.clear()
                make()
                r = run(self.src, self.out)
                self.assertNotEqual(r.returncode, 0, "the volume holds an entry that is not a tenant file")
                self.assertNotIn(PASSWORD, r.stdout + r.stderr)
                for p in list(self.out.iterdir()):
                    if p.is_dir():
                        p.rmdir()

    # A506 (interim, from the code): four DSN directories, one file per tenant named by the tenant id, each holding a different login: `trust_rw__<t>`, `orchestrator_rw__<t>` and `record_read__<t>`
    # (the shared tenant login password) and `record_emitter__<t>__core-edge` (its own per-login secret, emitter_logins/<t>__core-edge). One job per output directory.
    EDGE_PASSWORD = "synthetic-edge-login-secret"  # SYNTHETIC: readable, so no scanner reads it as a credential

    def jobs_env(self) -> dict[str, str]:
        emitters = Path(self.tmp.name) / "emitters"
        emitters.mkdir(exist_ok=True)
        for t in ("ten_aa", "ten_bb"):
            (emitters / f"{t}__core-edge").write_text(self.EDGE_PASSWORD + "\n", encoding="utf-8")
        dirs = {n: Path(self.tmp.name) / n for n in ("trust", "orch", "rr", "edge", "exec", "out", "evid")}
        for d in dirs.values():
            d.mkdir(exist_ok=True)
        self.dirs = dirs
        return {"TRUST_DSN_EMITTERS": str(emitters), "TRUST_DSN_JOBS": f"trust_rw:{dirs['trust']} orchestrator_rw:{dirs['orch']} record_read:{dirs['rr']} edge:{dirs['edge']} execution_rw:{dirs['exec']} outcome_rw:{dirs['out']} evidence_rw:{dirs['evid']}"}

    def test_seven_directories_each_with_its_own_login_and_password(self) -> None:
        env = self.jobs_env()
        r = run(self.src, self.out, **env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        want = {"trust": "trust_rw__{t}:" + PASSWORD, "orch": "orchestrator_rw__{t}:" + PASSWORD, "rr": "record_read__{t}:" + PASSWORD, "edge": "record_emitter__{t}__core-edge:" + self.EDGE_PASSWORD,
                "exec": "execution_rw__{t}:" + PASSWORD, "out": "outcome_rw__{t}:" + PASSWORD, "evid": "evidence_rw__{t}:" + PASSWORD}  # A554 (lane 5): the Execution side's two logins, the per-tenant execution_rw and outcome_rw
        for key, login in want.items():
            self.assertEqual(sorted(p.name for p in self.dirs[key].iterdir()), ["ten_aa", "ten_bb"], key)
            for t in ("ten_aa", "ten_bb"):
                f = self.dirs[key] / t
                self.assertEqual(stat.S_IMODE(f.stat().st_mode), 0o400, (key, t))
                self.assertEqual(f.read_text(encoding="utf-8").strip(), "postgres://" + login.format(t=t) + "@postgres:5432/noema", (key, t))
        self.assertNotIn(PASSWORD, r.stdout + r.stderr)
        self.assertNotIn(self.EDGE_PASSWORD, r.stdout + r.stderr)

    def test_each_tenants_file_names_only_that_tenant_in_every_directory(self) -> None:
        env = self.jobs_env()
        self.assertEqual(run(self.src, self.out, **env).returncode, 0)
        for key, d in self.dirs.items():
            self.assertNotIn("ten_bb", (d / "ten_aa").read_text(encoding="utf-8"), key)

    def test_a_missing_per_login_secret_stops_everything_and_leaves_no_file_in_any_directory(self) -> None:
        env = self.jobs_env()
        (Path(env["TRUST_DSN_EMITTERS"]) / "ten_bb__core-edge").unlink()
        r = run(self.src, self.out, **env)
        self.assertNotEqual(r.returncode, 0)
        for key, d in self.dirs.items():
            self.assertEqual(list(d.iterdir()), [], f"{key}: a file was left behind")
        self.assertNotIn(PASSWORD, r.stdout + r.stderr)

    def test_a_bad_job_is_refused_before_anything_is_written(self) -> None:
        env = self.jobs_env()
        good = env["TRUST_DSN_JOBS"]
        for bad in ("", "trust_rw", "root:/x", f"trust_rw:{self.dirs['trust']} trust_rw:{self.dirs['orch']}", f"trust_rw:{self.dirs['trust']} orchestrator_rw:{self.dirs['trust']}", "trust_rw:relative/dir", good + " extra"):
            with self.subTest(bad):
                r = run(self.src, self.out, **dict(env, TRUST_DSN_JOBS=bad))
                self.assertNotEqual(r.returncode, 0, bad)
                for d in self.dirs.values():
                    self.assertEqual(list(d.iterdir()), [])

    def test_a_per_login_secret_that_could_change_the_dsn_is_refused(self) -> None:
        env = self.jobs_env()
        for bad in ("short", "has space0000000000", "a/b" + "0" * 20, "p@ss" + "0" * 20):
            with self.subTest(bad):
                (Path(env["TRUST_DSN_EMITTERS"]) / "ten_aa__core-edge").write_text(bad + "\n", encoding="utf-8")
                r = run(self.src, self.out, **env)
                self.assertNotEqual(r.returncode, 0)
                for d in self.dirs.values():
                    self.assertEqual(list(d.iterdir()), [])

    def test_a_password_that_could_change_the_dsn_is_refused(self) -> None:
        # A DSN is a URL: a password with a separator would change what it means. The generated secrets are hexadecimal; anything outside letters, digits, dot, tilde, dash and underscore is refused.
        for bad in ("", "short", "has space0000000000", "a/b" + "0" * 20, "p@ss" + "0" * 20, "a:b" + "0" * 20, "a?b" + "0" * 20, "a%b" + "0" * 20, "a#b" + "0" * 20):
            with self.subTest(bad):
                self.clear()
                self.put(bad)
                r = run(self.src, self.out)
                self.assertNotEqual(r.returncode, 0)
                self.assertEqual(list(self.out.iterdir()), [])
                self.assertNotIn(bad.strip() or "\0", (r.stdout + r.stderr).replace("\n", ""))

    def test_a_missing_password_file_is_refused(self) -> None:
        (self.src / "noema_tenant_login_password").unlink()
        self.assertNotEqual(run(self.src, self.out).returncode, 0)


if __name__ == "__main__":
    unittest.main()
