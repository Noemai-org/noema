"""The /partner refusal probe (benchmark/runner/tushe_partner_probe.py; ruling A263): a refusal counts only after a control read was served; a served administration route fails it; any other
answer is NOT RUN; it makes GET requests only and never prints the credential. A local server stands for Tushe; nothing here says anything about Tushe's behaviour."""
import io
import tempfile
import threading
import unittest
from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

from benchmark.runner import tushe_partner_probe as probe

SECRET = "synthetic-runtime-credential-value"
CONTROL = "/v1/events"
ADMIN = ["/partner/events", "/partners/api-keys/scopes"]


class FakeTushe(BaseHTTPRequestHandler):
    expects = "Authorization"  # the header the fake wants the credential in
    methods: list[str] = []

    def log_message(self, *args: Any) -> None:
        pass

    def _answer(self, status: int, extra: dict[str, str] | None = None) -> None:
        self.send_response(status)
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.send_header("Content-Length", "2")
        self.end_headers()
        self.wfile.write(b"{}")

    def _handle(self) -> None:
        type(self).methods.append(self.command)
        presented = self.headers.get(self.expects, "")
        ok = presented.endswith(SECRET) if self.expects == "Authorization" else presented == SECRET
        # The fake only serves the ordinary edge: the control read is served to the credential in the header it wants, and an administration route is refused. It never fakes a
        # failure (a served route, a refused control, a scope refusal, a stall): those answers come from the hostility proxy's FaultPlans (partner_probe_test.go; AGENT-12, REQ-TST-004).
        if self.path.startswith(CONTROL) or self.path == "/v1/audit-events":
            return self._answer(200 if ok else 401)
        self._answer(403 if ok else 401)

    do_GET = _handle
    do_POST = _handle
    do_PUT = _handle
    do_DELETE = _handle


class Probe(unittest.TestCase):
    def setUp(self) -> None:
        FakeTushe.expects, FakeTushe.methods = "Authorization", []
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), FakeTushe)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.tmp = tempfile.TemporaryDirectory()
        self.cred = Path(self.tmp.name) / "cred"
        self.cred.write_text(SECRET + "\n", encoding="utf-8")

    def tearDown(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.tmp.cleanup()

    def run_cli(self, *, scheme: str = "bearer", cred: Path | None = None, url: str | None = None, loopback: bool = True, defaults: bool = False) -> tuple[int, str]:
        argv = ["--base-url", url or self.url, "--credential-file", str(cred or self.cred)]
        if not defaults:
            argv += ["--scheme", scheme, "--control-path", CONTROL]
            for path in ADMIN:
                argv += ["--probe-path", path]
        if loopback:
            argv.append("--allow-loopback-http")
        out = io.StringIO()
        with redirect_stdout(out):
            code = probe.main(argv)
        return code, out.getvalue()

    def test_a_refusal_after_a_served_control_is_met(self) -> None:
        code, text = self.run_cli()
        self.assertEqual(code, 0, text)
        self.assertIn("MET", text)
        self.assertNotIn(SECRET, text)

    def test_a_credential_presented_the_wrong_way_is_not_run_and_a_refusal_is_not_read_as_a_pass(self) -> None:
        # The credential is presented the wrong way (the fake wants X-API-Key): every route answers 401, which looks like a refusal and means nothing.
        FakeTushe.expects = "X-API-Key"
        code, text = self.run_cli(scheme="bearer")
        self.assertEqual(code, 3, text)
        self.assertIn("no control read was served", text)

    def test_the_other_scheme_works_when_the_server_wants_it(self) -> None:
        FakeTushe.expects = "X-API-Key"
        self.assertEqual(self.run_cli(scheme="x-api-key")[0], 0)

    def test_with_only_a_base_url_and_a_credential_file_the_documented_reads_and_routes_are_used_and_the_scheme_is_found(self) -> None:
        # Ruling A276: nothing but the base URL and the credential comes from outside. The scheme is found by the control read and reported; the paths are the documented ones.
        for expects, scheme in (("Authorization", "bearer"), ("X-API-Key", "x-api-key")):
            with self.subTest(scheme):
                FakeTushe.expects = expects
                FakeTushe.methods = []
                code, text = self.run_cli(defaults=True)
                self.assertEqual(code, 0, text)
                self.assertIn(f'"scheme": "{scheme}"', text)
                self.assertIn("/partners/api-keys/scopes", text)
                self.assertIn("/partner/events", text)

    def test_every_status_is_classified_by_one_pure_function(self) -> None:
        # No server and no manufactured failure: the meaning of each status is a table. Failure ANSWERS from a real system are produced by the hostility proxy's FaultPlans, in
        # benchmark/hostility_proxy/internal/proxy/partner_probe_test.go (AGENTS 8: no mocked failure modes).
        for status in (401, 403):
            self.assertEqual(probe.classify(status), "refused", status)
        for status in (200, 201, 204, 299):
            self.assertEqual(probe.classify(status), "SERVED", status)
        for status in (100, 301, 302, 304, 400, 404, 405, 409, 429, 500, 502, 503):
            self.assertEqual(probe.classify(status), "inconclusive", status)

    def test_a_served_route_is_never_erased_by_an_inconclusive_one(self) -> None:
        # Codex P2 on noema#655: a later timeout must not turn a confirmed isolation breach into NOT RUN.
        def row(outcome: str) -> dict[str, object]:
            return {"path": "/x", "status": None, "outcome": outcome}

        self.assertTrue(probe.verdict_of([row("SERVED"), row("inconclusive")]).startswith("NOT MET"))
        self.assertTrue(probe.verdict_of([row("inconclusive"), row("SERVED")]).startswith("NOT MET"))
        self.assertTrue(probe.verdict_of([row("refused"), row("inconclusive")]).startswith("NOT RUN"))
        self.assertTrue(probe.verdict_of([row("refused"), row("refused")]).startswith("MET"))

    def test_a_credential_that_is_not_one_printable_value_is_refused_and_never_echoed(self) -> None:
        # Codex P1 on noema#655: http.client puts a header value it rejects into its error text; a two-line credential must not reach it, and must not be printed.
        self.assertFalse(probe.valid_credential("first-line\nsecond-line"))
        for bad in ("", "has space", "tab\there", "nul\x00", "caf\u00e9", "x" * 4097):
            self.assertFalse(probe.valid_credential(bad), repr(bad))
        self.assertTrue(probe.valid_credential(SECRET))
        multi = Path(self.tmp.name) / "multi"
        multi.write_text("first-secret-line\nsecond-secret-line\n", encoding="utf-8")
        code, text = self.run_cli(cred=multi)
        self.assertEqual(code, 3, text)
        self.assertNotIn("first-secret-line", text)
        self.assertNotIn("second-secret-line", text)
        self.assertEqual(FakeTushe.methods, [], "no request was made with it")

    def test_it_makes_get_requests_only(self) -> None:
        self.run_cli()
        self.assertEqual(set(FakeTushe.methods), {"GET"})

    def test_http_is_refused_unless_the_test_flag_is_set_and_a_bad_input_is_refused(self) -> None:
        self.assertEqual(self.run_cli(loopback=False)[0], 3)
        self.assertEqual(self.run_cli(url="http://example.invalid", loopback=True)[0], 3)

    def test_a_missing_or_empty_credential_file_is_not_run_and_a_failed_transport_is_not_run(self) -> None:
        self.assertEqual(self.run_cli(cred=Path(self.tmp.name) / "nope")[0], 3)
        empty = Path(self.tmp.name) / "empty"
        empty.write_text("\n", encoding="utf-8")
        self.assertEqual(self.run_cli(cred=empty)[0], 3)
        self.server.shutdown()
        self.server.server_close()
        self.assertEqual(self.run_cli()[0], 3)

    def test_the_credential_is_never_in_the_written_report(self) -> None:
        out = Path(self.tmp.name) / "report.json"
        argv = ["--base-url", self.url, "--credential-file", str(self.cred), "--scheme", "bearer", "--control-path", CONTROL, "--probe-path", ADMIN[0], "--out", str(out), "--allow-loopback-http"]
        with redirect_stdout(io.StringIO()):
            self.assertEqual(probe.main(argv), 0)
        self.assertNotIn(SECRET, out.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
