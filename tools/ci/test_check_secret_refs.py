"""Tests for check_secret_refs.py: unlisted names, the bench-only rule for protected prefixes, and the fail-closed forms.

Run with the pinned PyYAML: .venv-ci/bin/python -I tools/ci/test_check_secret_refs.py
Every form that must fail has a test that it fails, and every form that must pass has one too.
"""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_secret_refs as c  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent
ENV = "OPENAI_API_KEY=\nTUSHE_ORACLE_*=\nTUSHE_FIXTURE_*=\nDOCS_READ_TOKEN=\n"


def job(body: str, job_id="j", environment=None, extra="") -> str:
    env = f"    environment: {environment}\n" if environment else ""
    return f"jobs:\n  {job_id}:\n    runs-on: ubuntu-24.04\n{env}{extra}    steps:\n      - run: x\n        env:\n          K: {body}\n"


def run(workflow: str, env: str = ENV, name: str = "bench.yml") -> list[str]:
    with tempfile.TemporaryDirectory() as tmp:
        d = Path(tmp)
        (d / "wf").mkdir()
        (d / "wf" / name).write_text(workflow)
        (d / "env").write_text(env)
        return c.violations(d / "wf", d / "env")


ORACLE = "${{ secrets.TUSHE_ORACLE_CLIENT_ID }}"


class Names(unittest.TestCase):
    def test_listed_secret_passes(self):
        self.assertEqual(run(job("${{ secrets.OPENAI_API_KEY }}"), name="model.yml"), [])

    def test_unlisted_secret_fails(self):
        self.assertEqual(len(run(job("${{ secrets.NOPE_KEY }}"), name="model.yml")), 1)

    def test_github_token_is_built_in(self):
        self.assertEqual(run(job("${{ secrets.GITHUB_TOKEN }}"), env=""), [])

    def test_names_are_case_insensitive(self):
        self.assertEqual(run(job("${{ secrets.openai_api_key }}"), name="model.yml"), [])
        self.assertEqual(len(run(job("${{ secrets.tushe_oracle_x }}"), name="model.yml")), 1)

    def test_a_longer_name_does_not_match_a_listed_one(self):
        self.assertEqual(len(run(job("${{ secrets.OPENAI_API_KEY_2 }}"), name="model.yml")), 1)

    def test_comments_and_plain_text_are_not_references(self):
        wf = "# ${{ secrets.NOPE }}\n" + job("plain", extra="").replace("- run: x", '- run: echo "we keep secrets.NOPE out of logs"')
        self.assertEqual(run(wf, name="model.yml"), [])

    def test_an_if_expression_is_checked(self):
        wf = job("1").replace("      - run: x", "      - if: secrets.NOPE != ''\n        run: x")
        self.assertEqual(len(run(wf, name="model.yml")), 1)

    def test_the_real_repository_passes(self):
        self.assertEqual(c.violations(ROOT / ".github/workflows", ROOT / ".env.example"), [])


class BenchOnly(unittest.TestCase):
    def test_a_bench_environment_job_may_read_the_oracle_prefix(self):
        self.assertEqual(run(job(ORACLE, "bench_run", "bench")), [])

    def test_the_mapping_form_of_the_environment_also_passes(self):
        wf = job(ORACLE, "bench_run").replace("    steps:", "    environment:\n      name: bench\n    steps:")
        self.assertEqual(run(wf), [])

    def test_a_job_with_another_id_and_environment_bench_passes_because_the_id_is_free_text(self):
        self.assertEqual(run(job(ORACLE, "deploy", "bench")), [])

    def test_a_job_named_bench_without_the_environment_fails(self):
        self.assertEqual(len(run(job(ORACLE, "bench"))), 1)

    def test_a_job_in_another_environment_fails(self):
        self.assertEqual(len(run(job(ORACLE, "j", "core-ci"))), 1)

    def test_an_expression_as_the_environment_fails(self):
        self.assertEqual(len(run(job(ORACLE, "j", "${{ inputs.env }}"))), 1)

    def test_an_environment_with_extra_keys_fails(self):
        wf = job(ORACLE).replace("    steps:", "    environment:\n      name: bench\n      url: https://x.invalid\n    steps:")
        self.assertEqual(len(run(wf)), 1)

    def test_a_non_bench_workflow_file_fails_even_with_the_environment(self):
        self.assertEqual(len(run(job(ORACLE, "j", "bench"), name="core.yml")), 1)

    def test_a_non_bench_job_reading_the_prefix_fails_although_the_example_lists_it(self):
        self.assertEqual(len(run(job(ORACLE), name="core.yml")), 1)
        self.assertEqual(len(run(job(ORACLE))), 1)

    def test_the_fixture_prefix_is_protected_too(self):
        self.assertEqual(len(run(job("${{ secrets.TUSHE_FIXTURE_ID }}"))), 1)
        self.assertEqual(run(job("${{ secrets.TUSHE_FIXTURE_ID }}", "j", "bench")), [])

    def test_bracket_form_in_a_non_bench_job_fails(self):
        self.assertEqual(len(run(job("${{ secrets['TUSHE_ORACLE_CLIENT_ID'] }}"))), 1)
        self.assertEqual(len(run(job('${{ secrets["TUSHE_ORACLE_CLIENT_ID"] }}'))), 1)

    def test_bracket_form_in_a_bench_job_passes(self):
        self.assertEqual(run(job("${{ secrets['TUSHE_ORACLE_CLIENT_ID'] }}", "j", "bench")), [])

    def test_a_workflow_level_reference_fails(self):
        wf = "env:\n  K: " + ORACLE + "\n" + job("1", "j", "bench")
        self.assertEqual(len(run(wf)), 1)

    def test_a_reusable_workflow_call_cannot_be_bench(self):
        wf = "jobs:\n  j:\n    uses: ./.github/workflows/x.yml\n    environment: bench\n    secrets:\n      K: " + ORACLE + "\n"
        self.assertGreaterEqual(len(run(wf)), 1)


class FailClosed(unittest.TestCase):
    def test_a_computed_lookup_in_a_non_bench_job_fails(self):
        self.assertEqual(len(run(job("${{ secrets[matrix.name] }}"), name="model.yml")), 1)
        self.assertEqual(len(run(job("${{ secrets['TUSHE_' + 'ORACLE_ID'] }}"), name="model.yml")), 1)

    def test_dumping_the_whole_context_fails(self):
        for body in ("${{ toJSON(secrets) }}", "${{ secrets }}", "${{ format('{0}', secrets) }}"):
            with self.subTest(body):
                self.assertEqual(len(run(job(body), name="model.yml")), 1)

    def test_a_dump_in_a_bench_job_is_allowed(self):
        self.assertEqual(run(job("${{ toJSON(secrets) }}", "j", "bench")), [])

    def test_secrets_inherit_fails(self):
        wf = "jobs:\n  j:\n    uses: ./.github/workflows/x.yml\n    secrets: inherit\n"
        self.assertEqual(len(run(wf, name="model.yml")), 1)

    def test_a_file_that_cannot_be_parsed_fails(self):
        self.assertEqual(len(run("jobs: [unclosed\n", name="model.yml")), 1)

    def test_a_file_without_jobs_fails(self):
        self.assertEqual(len(run("name: x\n", name="model.yml")), 1)


if __name__ == "__main__":
    unittest.main()
