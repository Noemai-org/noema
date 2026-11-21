"""BEN-05 / A476 (rulings of 20 Nov): the `after_recovery` and `after_replay` phases of `reconstructions.json`. Written before `reread.py` was changed and seen failing. SYNTHETIC.

A476: `after_recovery` is read after a core container restart and a wait for /readyz 200, through the audit-only route (noema#783) as the AUDIT principal the stack's tenant init seeds, with its credential
file. `after_replay` is a SECOND READ of the same route and its bytes must equal the first: it is a re-read, not a replay, and nothing here claims deterministic replay (INV-21). The restart is an
infrastructure step of the workflow (IL's). Until the seeded principal and its credential file exist the phase is NOT RUN (exit 3), never faked with a generated principal."""
import json
import shutil
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.oracle_client.transport import HttpError
from benchmark.runner.api import PublicApi
from benchmark.runner.reread import NOT_RUN, ReadyTimeout, main, reread, wait_ready
from benchmark.runner.testdata import ADMIN_TOKEN, CORRELATION, FakeNoema, Script

Json = dict[str, Any]
BODY: Json = {"tenant_id": "ten_alpha", "correlation_id": CORRELATION, "events": [{"seq": 1}], "sections": []}
TOKEN = ADMIN_TOKEN
FIRST_LINE = '{"case_id":"BEN-A-0001","correlation_id":"%s","kind":"case","method":"POST","path":"/v1/conversations","status":200}\n' % CORRELATION


class Tmp:
    def __init__(self, case: unittest.TestCase) -> None:
        self.path = tempfile.mkdtemp()
        case.addCleanup(shutil.rmtree, self.path, True)

    def __enter__(self) -> str:
        return self.path

    def __exit__(self, *a: object) -> None:
        return None


def run_dir(tmp: str, phases: dict[str, dict[str, Json]] | None = None, correlations: dict[str, str] | None = None) -> Path:
    run = Path(tmp)
    cases = {"BEN-A-0001": {"initial": BODY}, "BEN-B-0001": {"initial": BODY}} if phases is None else phases
    (run / "reconstructions.json").write_text(json.dumps(cases), encoding="utf-8")
    (run / "correlations.json").write_text(json.dumps({"BEN-A-0001": CORRELATION, "BEN-B-0001": CORRELATION} if correlations is None else correlations), encoding="utf-8")
    (run / "requests.jsonl").write_text(FIRST_LINE, encoding="utf-8")
    return run


def doc(run: Path) -> Json:
    out: Json = json.loads((run / "reconstructions.json").read_text(encoding="utf-8"))
    return out


def requests_of(run: Path) -> list[Json]:
    return [json.loads(line) for line in (run / "requests.jsonl").read_text(encoding="utf-8").splitlines()]


def snapshot(run: Path) -> list[bytes]:
    return [(run / n).read_bytes() for n in ("reconstructions.json", "requests.jsonl")]


class Phases(unittest.TestCase):
    def test_after_recovery_reads_every_case_with_the_seeded_audit_credential_and_adds_the_phase(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            mismatched = reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            reads = [x for x in fake.requests if x[1].startswith("/v1/reconstructions/")]
        self.assertEqual([], mismatched)
        self.assertEqual(["initial", "after_recovery"], list(doc(run)["BEN-A-0001"]))
        self.assertEqual(BODY, doc(run)["BEN-A-0001"]["after_recovery"])
        self.assertEqual(2, len(reads))
        self.assertTrue(all(x[2] == f"Bearer {ADMIN_TOKEN}" for x in reads))

    def test_the_reads_are_added_to_the_request_record_and_the_earlier_lines_are_kept(self) -> None:
        """Codex P2: each re-read writes a RECORD_ACCESSED line in the core, so requests.jsonl must carry it: case, correlation, route template, status, kind reconstruction."""
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            reread(run, "after_replay", PublicApi(fake.url), TOKEN)
        got = requests_of(run)
        self.assertEqual(json.loads(FIRST_LINE), got[0])
        added = got[1:]
        self.assertEqual(4, len(added))
        self.assertEqual({"BEN-A-0001", "BEN-B-0001"}, {x["case_id"] for x in added})
        for x in added:
            self.assertEqual(("GET", "/v1/reconstructions/{correlation_id}", 200, "reconstruction", CORRELATION), (x["method"], x["path"], x["status"], x["kind"], x["correlation_id"]))

    def test_after_replay_is_a_second_read_and_the_bytes_must_equal_the_first(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            mismatched = reread(run, "after_replay", PublicApi(fake.url), TOKEN)
        self.assertEqual([], mismatched)
        self.assertEqual(["initial", "after_recovery", "after_replay"], list(doc(run)["BEN-A-0001"]))
        self.assertEqual(doc(run)["BEN-A-0001"]["after_recovery"], doc(run)["BEN-A-0001"]["after_replay"])

    def test_a_second_read_that_differs_is_returned_and_the_phase_is_not_written(self) -> None:
        """Codex: a mismatching phase is not written before the mismatch is returned. The reads did happen, so they stay in the request record."""
        other = {**BODY, "events": [{"seq": 1}, {"seq": 2}]}
        with FakeNoema(Script(reconstruction=other)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp, {"BEN-A-0001": {"initial": BODY, "after_recovery": BODY}})
            before = (run / "reconstructions.json").read_bytes()
            mismatched = reread(run, "after_replay", PublicApi(fake.url), TOKEN)
        self.assertEqual(["BEN-A-0001"], mismatched)
        self.assertEqual(before, (run / "reconstructions.json").read_bytes())
        self.assertEqual(2, len(requests_of(run)))

    def test_the_comparison_is_of_the_canonical_json_so_key_order_is_not_a_difference(self) -> None:
        reordered = {k: BODY[k] for k in reversed(list(BODY))}
        with FakeNoema(Script(reconstruction=reordered)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp, {"BEN-A-0001": {"initial": BODY, "after_recovery": BODY}})
            self.assertEqual([], reread(run, "after_replay", PublicApi(fake.url), TOKEN))

    def test_a_phase_needs_the_phase_before_it_and_is_read_once(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            with self.assertRaises(ValueError):
                reread(run, "after_replay", PublicApi(fake.url), TOKEN)
            self.assertEqual(["initial"], list(doc(run)["BEN-A-0001"]))
            reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            with self.assertRaises(ValueError):
                reread(run, "after_recovery", PublicApi(fake.url), TOKEN)

    def test_an_unknown_phase_a_stamped_or_unreadable_file_or_a_missing_correlation_is_an_error_and_nothing_is_written(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            before = snapshot(run)
            with self.assertRaises(ValueError):
                reread(run, "after_nothing", PublicApi(fake.url), TOKEN)
            (run / "reconstructions.json").write_text(json.dumps({"stamp": {"sha": "a" * 40, "run_id": "r"}, "cases": {}}), encoding="utf-8")
            with self.assertRaises(ValueError):
                reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            for text in ("[]", "null", "not json"):
                (run / "reconstructions.json").write_text(text, encoding="utf-8")
                with self.assertRaises(ValueError, msg=text):
                    reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            (run / "reconstructions.json").write_bytes(before[0])
            (run / "correlations.json").write_text(json.dumps({"BEN-A-0001": CORRELATION}), encoding="utf-8")
            with self.assertRaises(ValueError):
                reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            self.assertEqual(before, snapshot(run))

    def test_one_refused_read_fails_the_whole_phase_and_writes_nothing(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            before = snapshot(run)
            with self.assertRaises(HttpError):
                reread(run, "after_recovery", PublicApi(fake.url), "not-a-session")
            self.assertEqual(before, snapshot(run))

    def test_a_missing_request_record_is_an_error_before_any_read(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            (run / "requests.jsonl").unlink()
            with self.assertRaises(ValueError):
                reread(run, "after_recovery", PublicApi(fake.url), TOKEN)
            self.assertEqual([], [x for x in fake.requests if x[1].startswith("/v1/reconstructions/")])


class Ready(unittest.TestCase):
    def test_it_waits_until_readyz_answers_200_and_never_longer_than_the_limit(self) -> None:
        answers = iter([503, 503, 200])
        slept: list[float] = []
        wait_ready(lambda: next(answers), timeout_s=10, poll_s=1, sleep=slept.append, now=iter([0.0, 1.0, 2.0, 3.0]).__next__)
        self.assertEqual([1, 1], slept)

    def test_a_service_that_never_gets_ready_is_a_timeout_never_a_pass(self) -> None:
        ticks = iter([float(n) for n in range(50)])
        with self.assertRaises(ReadyTimeout):
            wait_ready(lambda: 503, timeout_s=5, poll_s=1, sleep=lambda s: None, now=lambda: next(ticks))

    def test_only_a_200_is_ready_not_any_2xx_and_a_failure_to_answer_is_not_ready(self) -> None:
        for status in (201, 204, 301, 404, 500, None):
            ticks = iter([float(n) for n in range(50)])
            with self.assertRaises(ReadyTimeout, msg=str(status)):
                wait_ready(lambda s=status: s, timeout_s=3, poll_s=1, sleep=lambda x: None, now=lambda: next(ticks))  # type: ignore[misc]


class Cli(unittest.TestCase):
    def test_without_the_seeded_audit_credential_the_phase_is_not_run_and_nothing_is_written_or_generated(self) -> None:
        """Codex P1 (A476): the re-read uses the audit principal the stack's tenant init seeds and its credential file. Until that exists the phase is NOT RUN, never faked with a generated principal."""
        with Tmp(self) as tmp:
            run = run_dir(tmp)
            before = snapshot(run)
            base = ["--run", str(run), "--phase", "after_recovery", "--api-url", "http://127.0.0.1:9"]
            self.assertEqual(NOT_RUN, main(base))
            self.assertEqual(NOT_RUN, main([*base, "--audit-credential-file", str(Path(tmp) / "absent")]))
            (Path(tmp) / "empty").write_text("\n", encoding="utf-8")
            self.assertEqual(NOT_RUN, main([*base, "--audit-credential-file", str(Path(tmp) / "empty")]))
            self.assertEqual(before, snapshot(run))

    def test_with_the_credential_file_the_phase_runs_as_that_session(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            cred = Path(tmp) / "audit.token"
            cred.write_text(ADMIN_TOKEN + "\n", encoding="utf-8")
            code = main(["--run", str(run), "--phase", "after_recovery", "--api-url", fake.url, "--audit-credential-file", str(cred)])
            reads = [x for x in fake.requests if x[1].startswith("/v1/reconstructions/")]
        self.assertEqual(0, code)
        self.assertEqual(2, len(reads))
        self.assertTrue(all(x[2] == f"Bearer {ADMIN_TOKEN}" for x in reads))

    def test_a_mismatch_exits_one_and_writes_no_phase(self) -> None:
        other = {**BODY, "events": []}
        with FakeNoema(Script(reconstruction=other)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp, {"BEN-A-0001": {"initial": BODY, "after_recovery": BODY}})
            cred = Path(tmp) / "audit.token"
            cred.write_text(ADMIN_TOKEN, encoding="utf-8")
            before = (run / "reconstructions.json").read_bytes()
            code = main(["--run", str(run), "--phase", "after_replay", "--api-url", fake.url, "--audit-credential-file", str(cred)])
            self.assertEqual(1, code)
            self.assertEqual(before, (run / "reconstructions.json").read_bytes())

    def test_bad_arguments_or_an_unreadable_run_exit_two_and_write_nothing(self) -> None:
        with Tmp(self) as tmp:
            run = run_dir(tmp)
            cred = Path(tmp) / "audit.token"
            cred.write_text("t", encoding="utf-8")
            before = snapshot(run)
            url = "http://127.0.0.1:9"
            self.assertEqual(2, main(["--run", str(run), "--phase", "after_nothing", "--api-url", url, "--audit-credential-file", str(cred)]))
            self.assertEqual(2, main(["--run", str(Path(tmp) / "absent"), "--phase", "after_recovery", "--api-url", url, "--audit-credential-file", str(cred)]))
            self.assertEqual(2, main(["--run", str(run), "--phase", "after_recovery", "--api-url", url, "--audit-credential-file", str(cred), "--readyz-url", url + "/readyz"]))
            self.assertEqual(before, snapshot(run))


class Abbreviations(unittest.TestCase):
    """IL's rule (noema#791): argparse accepts any unique prefix, so an abbreviated flag could select a setting without the full flag appearing in a workflow."""

    def test_an_abbreviated_flag_is_refused(self) -> None:
        with Tmp(self) as tmp:
            run = run_dir(tmp)
            with self.assertRaises(SystemExit) as raised:
                main(["--run", str(run), "--phase", "after_recovery", "--api-url", "http://127.0.0.1:9", "--audit-credential-f", tmp])
            self.assertEqual(2, raised.exception.code)


if __name__ == "__main__":
    unittest.main()
