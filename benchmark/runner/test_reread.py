"""BEN-05 / A476 (rulings of 20 Nov): the `after_recovery` and `after_replay` phases of `reconstructions.json`. Written before `reread.py` and seen failing. SYNTHETIC.

A476: `after_recovery` is read after a core container restart and a wait for /readyz 200, through the audit-only route (noema#783) as an audit principal. `after_replay` is a SECOND READ of the same
route and its bytes must equal the first: it is a re-read, not a replay, and nothing here claims deterministic replay (INV-21). The restart itself is an infrastructure step of the workflow (IL's);
this module waits for readiness and reads."""
import json
import shutil
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.oracle_client.transport import HttpError
from benchmark.runner.api import PublicApi
from benchmark.runner.reread import ReadyTimeout, main, reread, wait_ready
from benchmark.runner.testdata import ADMIN_TOKEN, CORRELATION, USER_TOKEN, FakeNoema, Script, reconstruction

Json = dict[str, Any]
BODY: Json = {"tenant_id": "ten_alpha", "correlation_id": CORRELATION, "events": [{"seq": 1}], "sections": []}


class Sessions:
    def __init__(self) -> None:
        self.asked: list[tuple[str, tuple[str, ...]]] = []

    def token_for(self, principal_id: str, roles: list[str]) -> str:
        self.asked.append((principal_id, tuple(roles)))
        return ADMIN_TOKEN if roles == ["audit"] else USER_TOKEN


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
    return run


def doc(run: Path) -> Json:
    out: Json = json.loads((run / "reconstructions.json").read_text(encoding="utf-8"))
    return out


class Phases(unittest.TestCase):
    def test_after_recovery_reads_every_case_as_an_audit_principal_and_adds_the_phase(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            sessions = Sessions()
            run = run_dir(tmp)
            mismatched = reread(run, "after_recovery", PublicApi(fake.url), sessions)
            reads = [x for x in fake.requests if x[1].startswith("/v1/reconstructions/")]
        self.assertEqual([], mismatched)
        self.assertEqual(["initial", "after_recovery"], list(doc(run)["BEN-A-0001"]))
        self.assertEqual(BODY, doc(run)["BEN-A-0001"]["after_recovery"])
        self.assertEqual(2, len(reads))
        self.assertTrue(all(x[2] == f"Bearer {ADMIN_TOKEN}" for x in reads))
        self.assertEqual(2, len(sessions.asked))
        for principal, roles in sessions.asked:
            self.assertEqual(("audit",), roles)
            self.assertRegex(principal, r"^usr_[a-z]{20}$")

    def test_after_replay_is_a_second_read_and_the_bytes_must_equal_the_first(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            reread(run, "after_recovery", PublicApi(fake.url), Sessions())
            mismatched = reread(run, "after_replay", PublicApi(fake.url), Sessions())
        self.assertEqual([], mismatched)
        self.assertEqual(["initial", "after_recovery", "after_replay"], list(doc(run)["BEN-A-0001"]))
        self.assertEqual(doc(run)["BEN-A-0001"]["after_recovery"], doc(run)["BEN-A-0001"]["after_replay"])

    def test_a_second_read_that_differs_is_reported_and_still_written(self) -> None:
        other = {**BODY, "events": [{"seq": 1}, {"seq": 2}]}
        with FakeNoema(Script(reconstruction=other)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp, {"BEN-A-0001": {"initial": BODY, "after_recovery": BODY}})
            mismatched = reread(run, "after_replay", PublicApi(fake.url), Sessions())
        self.assertEqual(["BEN-A-0001"], mismatched)
        self.assertEqual(other, doc(run)["BEN-A-0001"]["after_replay"])

    def test_the_comparison_is_of_the_bytes_so_key_order_does_not_hide_a_difference_but_does_not_make_one(self) -> None:
        reordered = {k: BODY[k] for k in reversed(list(BODY))}
        with FakeNoema(Script(reconstruction=reordered)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp, {"BEN-A-0001": {"initial": BODY, "after_recovery": BODY}})
            self.assertEqual([], reread(run, "after_replay", PublicApi(fake.url), Sessions()))  # the same canonical JSON

    def test_a_phase_needs_the_phase_before_it(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            with self.assertRaises(ValueError):
                reread(run, "after_replay", PublicApi(fake.url), Sessions())  # no after_recovery yet
            self.assertEqual(["initial"], list(doc(run)["BEN-A-0001"]))
            reread(run, "after_recovery", PublicApi(fake.url), Sessions())
            with self.assertRaises(ValueError):
                reread(run, "after_recovery", PublicApi(fake.url), Sessions())  # a phase is read once

    def test_an_unknown_phase_or_a_stamped_file_or_an_unreadable_one_is_an_error_and_nothing_is_written(self) -> None:
        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            before = (run / "reconstructions.json").read_bytes()
            with self.assertRaises(ValueError):
                reread(run, "after_nothing", PublicApi(fake.url), Sessions())
            (run / "reconstructions.json").write_text(json.dumps({"stamp": {"sha": "a" * 40, "run_id": "r"}, "cases": {}}), encoding="utf-8")
            with self.assertRaises(ValueError):  # already stamped: the phases are read before the stamp
                reread(run, "after_recovery", PublicApi(fake.url), Sessions())
            for text in ("[]", "null", "not json"):
                (run / "reconstructions.json").write_text(text, encoding="utf-8")
                with self.assertRaises(ValueError, msg=text):
                    reread(run, "after_recovery", PublicApi(fake.url), Sessions())
            (run / "reconstructions.json").write_bytes(before)
            (run / "correlations.json").write_text(json.dumps({"BEN-A-0001": CORRELATION}), encoding="utf-8")  # a case with no correlation
            with self.assertRaises(ValueError):
                reread(run, "after_recovery", PublicApi(fake.url), Sessions())
            self.assertEqual(before, (run / "reconstructions.json").read_bytes())

    def test_one_refused_read_fails_the_whole_phase_and_writes_nothing(self) -> None:
        class Bad(Sessions):
            def token_for(self, principal_id: str, roles: list[str]) -> str:
                return "not-a-session"

        with FakeNoema(Script(reconstruction=BODY)) as fake, Tmp(self) as tmp:
            run = run_dir(tmp)
            before = (run / "reconstructions.json").read_bytes()
            with self.assertRaises(HttpError):
                reread(run, "after_recovery", PublicApi(fake.url), Bad())
            self.assertEqual(before, (run / "reconstructions.json").read_bytes())


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
    def test_bad_arguments_or_an_unreadable_run_exit_two_and_write_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = run_dir(tmp)
            before = (run / "reconstructions.json").read_bytes()
            self.assertEqual(2, main(["--run", str(run), "--phase", "after_nothing", "--api-url", "http://127.0.0.1:9", "--tokens-dir", tmp]))
            self.assertEqual(2, main(["--run", str(Path(tmp) / "absent"), "--phase", "after_recovery", "--api-url", "http://127.0.0.1:9", "--tokens-dir", tmp]))
            self.assertEqual(2, main(["--run", str(run), "--phase", "after_recovery", "--api-url", "http://127.0.0.1:9"]))  # no session source
            self.assertEqual(2, main(["--run", str(run), "--phase", "after_recovery", "--api-url", "http://127.0.0.1:9", "--tokens-dir", tmp, "--idp-key", "k"]))  # two session sources
            self.assertEqual(before, (run / "reconstructions.json").read_bytes())

    def test_a_ready_url_needs_its_timeout(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run = run_dir(tmp)
            self.assertEqual(2, main(["--run", str(run), "--phase", "after_recovery", "--api-url", "http://127.0.0.1:9", "--tokens-dir", tmp, "--readyz-url", "http://127.0.0.1:9/readyz"]))


if __name__ == "__main__":
    unittest.main()
