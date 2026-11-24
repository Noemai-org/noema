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

    def refused(self, env_text: str | None, shell: str | None, fallback: str = "ten_alpha") -> subprocess.CompletedProcess:
        with tempfile.TemporaryDirectory() as tmp:
            envfile = Path(tmp) / ".env"
            if env_text is not None:
                envfile.write_text(env_text, encoding="utf-8")
            env = {k: v for k, v in os.environ.items() if k != "NOEMA_TENANTS"}
            env["NOEMA_ENV_FILE"] = str(envfile)
            if shell is not None:
                env["NOEMA_TENANTS"] = shell
            sh = shutil.which("sh")
            assert sh
            return subprocess.run([sh, str(self.SCRIPT), fallback], env=env, capture_output=True, text=True, check=False)

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
        self.assertEqual(self.effective("NOEMA_TENANTS=ten_aa ten_bb\n", None, "ten_aa"), "ten_aa ten_bb", "only .env: .env wins over the fallback (Codex P2)")
        self.assertEqual(self.effective('NOEMA_TENANTS="ten_aa"\n', None, "ten_aa"), "ten_aa", "a quoted .env value")
        self.assertEqual(self.effective("# comment\nNOEMA_TENANTS=ten_aa # trailing\n", None, "ten_aa"), "ten_aa", "a trailing comment is not part of the value")
        self.assertEqual(self.effective("XNOEMA_TENANTS=ten_zz\n#NOEMA_TENANTS=ten_yy\n", None), "ten_alpha", "a look-alike line is not read")
        self.assertEqual(self.effective("NOEMA_TENANTS=ten_aa\n", "ten_cc", "ten_cc"), "ten_cc", "the shell wins over .env")
        self.assertEqual(self.effective(None, "ten_cc ten_dd", "ten_dd"), "ten_cc ten_dd", "only the shell")
        self.assertEqual(self.effective("NOEMA_TENANTS=ten_aa\n", "", "ten_bench"), "ten_bench", "an empty shell value is no list: the fallback, never a list nobody asked for")
        self.assertEqual(self.effective(None, None, "ten_bench"), "ten_bench", "the fallback is the argument, not a constant")

    # SR and C1 on noema#790 (P2): Make pastes the list into the recipe's command line as text, so a value with a quote or a `$(...)` would run as shell. Every word is checked against the tenant
    # shape here, before anything is printed, and the stand-in tenant must be one of them (C1's P3).
    def test_a_word_that_is_not_a_tenant_id_is_refused_and_nothing_is_printed(self) -> None:
        # A469 to A485, item 6 (C1's P2): the list is checked against ^[a-z0-9_]+$ before it reaches any recipe text, so an upper-case word is refused too.
        hostile = ["ten_AB9", "ten_aB", "ten_AA ten_bb", 'ten_aa"; echo INJECTED >&2; "', "ten_aa $(id)", "ten_aa`id`", "ten_aa;ls", "ten_aa&&ls", "ten_aa|ls", "ten_aa'x", "ten_a", "ten_" + "a" * 33, "Ten_aa", "ten_aa ten_b", "x", "ten_a$HOME", "ten_aa\\n", "../ten_aa"]
        for value in hostile:
            # each hostile value is tried alone and BESIDE the stand-in tenant, so the refusal is the word's own and not the missing stand-in tenant's
            for listed in (value, "ten_alpha " + value):
                for where in ("shell", "dotenv"):
                    done = self.refused(None, listed) if where == "shell" else self.refused("NOEMA_TENANTS=" + listed + "\n", None)
                    self.assertNotEqual(done.returncode, 0, (where, listed))
                    self.assertEqual(done.stdout, "", (where, listed, "something was printed for the shell to take"))
                    self.assertNotIn("INJECTED", done.stdout)

    def test_the_fallback_argument_is_checked_too_and_a_good_list_passes(self) -> None:
        self.assertNotEqual(self.refused(None, None, 'ten_aa"; id; "').returncode, 0)
        self.assertNotEqual(self.refused(None, None, "").returncode, 0, "no list and no stand-in tenant is an error, not an empty list")
        self.assertEqual(self.effective(None, "ten_alpha ten_ab9"), "ten_alpha ten_ab9")
        self.assertEqual(self.effective(None, "ten_" + "a" * 32 + " ten_aa", "ten_aa"), "ten_" + "a" * 32 + " ten_aa", "the longest tenant id is accepted")

    def test_the_stand_in_tenant_must_be_one_of_the_tenants(self) -> None:
        done = self.refused(None, "ten_bb ten_cc", "ten_alpha")
        self.assertNotEqual(done.returncode, 0, "the stand-in tenant is not in the list: sessions would name a tenant with no login")
        self.assertEqual(done.stdout, "")
        self.assertEqual(self.effective(None, "ten_bb ten_alpha", "ten_alpha"), "ten_bb ten_alpha")

    def test_bench_up_stops_before_anything_runs_when_the_list_is_empty(self) -> None:
        # `$(shell ...)` drops a failing script's exit code, so an empty result must stop the recipe on its own line.
        up = recipe("bench-up")
        guard = re.search(r'(?m)^\t@test -n "\$\(BENCH_TENANTS\)" \|\| \{ echo "bench-up: [^"]*"[^}]*exit 2; \}$', up)
        assert guard, "bench-up has no guard on an empty tenant list"
        self.assertLess(up.index(guard.group(0)), up.index("secrets_local.sh"), "the guard comes before the secret step")

    def test_postgres_the_dsn_step_and_the_tenant_steps_take_the_same_expression_in_the_overlay(self) -> None:
        # One tenant list for everything that acts per tenant: postgres (logins), the DSN step (files) and the two tenant one-shots (rows and login mappings; A353, DEBT-04). Each takes the same expression,
        # so the four cannot name different tenants.
        expr = '"${NOEMA_TENANTS:-${STANDIN_TENANT_ID:-ten_alpha}}"'
        self.assertEqual(OVERLAY.count("NOEMA_TENANTS: " + expr), 3, "postgres, tenants-create and tenants-provision")
        self.assertEqual(OVERLAY.count("TRUST_DSN_TENANTS: " + expr), 1)
        for service in ("tenants-create", "tenants-provision"):
            block = re.search(r"(?ms)^  " + service + r":\n(.*?)(?=^  [a-z][a-z-]*:\n)", OVERLAY)
            assert block, service
            self.assertIn("NOEMA_TENANTS: " + expr, block.group(1), service)


if __name__ == "__main__":
    unittest.main()
