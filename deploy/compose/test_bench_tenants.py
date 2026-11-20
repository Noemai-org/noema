"""One tenant list for the bench stack (Codex P1 on noema#790; A356).

`make bench-up` runs secrets_local.sh BEFORE Compose starts. secrets_local.sh makes one emitter-login secret per tenant of NOEMA_TENANTS, and postgres's init.sh refuses to start when a tenant it is
given has none. In the bench overlay postgres and trust-dsn-init take NOEMA_TENANTS, falling back to the stand-in tenant (BENCH_TENANT, default ten_alpha). If the secret step does not see the same
fallback, a fresh checkout with NOEMA_TENANTS unset gets a tenant in postgres and no secret for it, and the stack does not come up. These tests read the real Makefile and the real overlay."""
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MAKEFILE = (ROOT / "Makefile").read_text(encoding="utf-8")
OVERLAY = (ROOT / "deploy" / "compose" / "bench-stack.yml").read_text(encoding="utf-8")


def recipe(name: str) -> str:
    m = re.search(r"(?ms)^" + re.escape(name) + r":.*?(?=^\S)", MAKEFILE)
    assert m, name
    return m.group(0)


class BenchTenants(unittest.TestCase):
    def test_there_is_one_effective_tenant_list_and_it_falls_back_to_the_bench_tenant(self) -> None:
        self.assertRegex(MAKEFILE, r'(?m)^BENCH_TENANTS = \$\(shell sh deploy/compose/bench_tenants\.sh "\$\(BENCH_TENANT\)"\)$')

    def test_the_secret_step_of_bench_up_sees_the_same_list_postgres_gets(self) -> None:
        up = recipe("bench-up")
        self.assertRegex(up, r'NOEMA_TENANTS="\$\(BENCH_TENANTS\)" deploy/compose/secrets_local\.sh')
        self.assertNotRegex(up, r"(?m)^\tdeploy/compose/secrets_local\.sh")  # no call without the list

    def test_compose_gets_the_same_list(self) -> None:
        env = re.search(r"(?m)^BENCH_ENV = (.*)$", MAKEFILE)
        assert env
        self.assertIn('NOEMA_TENANTS="$(BENCH_TENANTS)"', env.group(1))

    # Codex P2 on noema#790 (twice): a tenant list set only in Compose's .env must not be overridden by the stand-in fallback. The script resolves the shell, then .env (the rules of
    # secrets_local.sh and provision-tenants.sh: the shell wins, even over .env), and only then falls back, so one list reaches the secret step and Compose.
    SCRIPT = ROOT / "deploy" / "compose" / "bench_tenants.sh"

    def effective(self, env_text: str | None, shell: str | None, fallback: str = "ten_alpha") -> str:
        with tempfile.TemporaryDirectory() as tmp:
            envfile = Path(tmp) / ".env"
            if env_text is not None:
                envfile.write_text(env_text, encoding="utf-8")
            env = {k: v for k, v in os.environ.items() if k != "NOEMA_TENANTS"}
            env["NOEMA_ENV_FILE"] = str(envfile)
            if shell is not None:
                env["NOEMA_TENANTS"] = shell
            sh = shutil.which("sh")
            assert sh, "sh is needed"
            done = subprocess.run([sh, str(self.SCRIPT), fallback], env=env, capture_output=True, text=True, check=False)
            self.assertEqual(done.returncode, 0, done.stderr)
            return done.stdout.strip()

    def test_the_effective_list_is_the_shell_then_dotenv_then_the_stand_in_tenant(self) -> None:
        self.assertTrue(self.SCRIPT.is_file(), "deploy/compose/bench_tenants.sh does not exist")
        self.assertEqual(self.effective(None, None), "ten_alpha", "nothing set: the stand-in tenant")
        self.assertEqual(self.effective("", None), "ten_alpha", "an empty .env: the stand-in tenant")
        self.assertEqual(self.effective("NOEMA_TENANTS=ten_aa ten_bb\n", None), "ten_aa ten_bb", "only .env: .env wins over the fallback (Codex P2)")
        self.assertEqual(self.effective('NOEMA_TENANTS="ten_aa"\n', None), "ten_aa", "a quoted .env value")
        self.assertEqual(self.effective("# comment\nNOEMA_TENANTS=ten_aa # trailing\n", None), "ten_aa", "a trailing comment is not part of the value")
        self.assertEqual(self.effective("XNOEMA_TENANTS=ten_zz\n#NOEMA_TENANTS=ten_yy\n", None), "ten_alpha", "a look-alike line is not read")
        self.assertEqual(self.effective("NOEMA_TENANTS=ten_aa\n", "ten_cc"), "ten_cc", "the shell wins over .env")
        self.assertEqual(self.effective(None, "ten_cc ten_dd"), "ten_cc ten_dd", "only the shell")
        self.assertEqual(self.effective("NOEMA_TENANTS=ten_aa\n", "", "ten_bench"), "ten_bench", "an empty shell value is no list: the fallback, never a list nobody asked for")
        self.assertEqual(self.effective(None, None, "ten_bench"), "ten_bench", "the fallback is the argument, not a constant")

    def test_postgres_and_the_dsn_step_take_the_same_expression_in_the_overlay(self) -> None:
        expr = '"${NOEMA_TENANTS:-${STANDIN_TENANT_ID:-ten_alpha}}"'
        self.assertEqual(OVERLAY.count("NOEMA_TENANTS: " + expr), 1)
        self.assertEqual(OVERLAY.count("TRUST_DSN_TENANTS: " + expr), 1)


if __name__ == "__main__":
    unittest.main()
