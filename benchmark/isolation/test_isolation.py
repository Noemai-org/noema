"""Tests for the isolation check against a stdlib test double (AT-02, INV-22). The double is SYNTHETIC. Run: python3 -I -m unittest."""
import http.server
import json
import os
import threading
import unittest
from typing import Any, Callable, Dict, List, Tuple

from benchmark.isolation import check_isolation as ci

with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "probes.json"), encoding="utf-8") as _f:
    PROBES: List[Dict[str, Any]] = json.load(_f)["probes"]

HandlerFn = Callable[[str, str], Tuple[int, str]]


def serve(handler_fn: HandlerFn) -> http.server.ThreadingHTTPServer:
    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a: Any) -> None:
            pass

        def _do(self) -> None:
            code, body = handler_fn(self.command, self.path)
            raw = body.encode()
            self.send_response(code)
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
        do_GET = do_POST = do_DELETE = _do

    s = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=s.serve_forever, daemon=True).start()
    return s


class Isolation(unittest.TestCase):
    def run_against(self, fn: HandlerFn) -> int:
        s = serve(fn)
        try:
            return ci.run("http://127.0.0.1:%d" % s.server_address[1], "synthetic-key", PROBES)
        finally:
            s.shutdown()

    def test_refusing_double_passes(self) -> None:
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": "forbidden", "code": "scope"}))), 0)
        self.assertEqual(self.run_against(lambda m, p: (404, "")), 0)

    # mutations: each leak must be caught
    def test_leaks_fail(self) -> None:
        n = len(PROBES)
        self.assertEqual(self.run_against(lambda m, p: (200, "{}")), n)                                   # served
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"events": [{"id": 1}]}))), n)    # 4xx with data
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": {"events": [1]}}))), n) # nested data
        self.assertEqual(self.run_against(lambda m, p: (500, "")), n)                                      # crash is not a refusal
        self.assertEqual(self.run_against(lambda m, p: (302, "")), n)                                      # redirect
        self.assertEqual(self.run_against(lambda m, p: (403, "x" * 2000)), n)                              # large body
        self.assertEqual(self.run_against(lambda m, p: (403, "<html>events</html>")), n)                  # non-JSON
        # Codex: scalar fields that pass a shape check but carry sensitive data
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"message": "account 0123456789 belongs to A. Name"}))), n)
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": "forbidden", "detail": "balance 5000"}))), n)
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": "acct_0123456789"}))), n)       # digit run
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": "a b c d"}))), n)               # free text
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": "x" * 80}))), n)               # long value
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": "forbidden", "status": 200}))), n)
        # a partial leak: only /partner/events served
        self.assertEqual(self.run_against(lambda m, p: (200, "[]") if p == "/partner/events" else (403, "")), 1)

    def test_the_check_never_prints_the_remote_body(self) -> None:
        import contextlib
        import io
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            self.run_against(lambda m, p: (403, json.dumps({"message": "SECRET-ACCOUNT-0123456789", "SECRET-KEY-NAME": "x"})))
        self.assertNotIn("SECRET", buf.getvalue())

    def test_unreachable_is_fail_not_pass(self) -> None:
        self.assertEqual(ci.run("http://127.0.0.1:1", "k", PROBES[:1]), 1)

    def test_tightened_status(self) -> None:
        ci.EXPECTED_STATUS = 403
        try:
            self.assertEqual(self.run_against(lambda m, p: (404, "")), len(PROBES))
            self.assertEqual(self.run_against(lambda m, p: (403, "")), 0)
        finally:
            ci.EXPECTED_STATUS = None


if __name__ == "__main__":
    unittest.main()
