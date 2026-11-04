"""Tests for the isolation check against a stdlib test double (AT-02, INV-22). The double is SYNTHETIC. Run: python3 -I -m unittest."""
import http.server
import json
import threading
import unittest

import check_isolation as ci

PROBES = json.load(open(__file__.rsplit("/", 1)[0] + "/probes.json" if "/" in __file__ else "probes.json"))["probes"]


def serve(handler_fn):
    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def _do(self):
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
    def run_against(self, fn):
        s = serve(fn)
        try:
            return ci.run("http://127.0.0.1:%d" % s.server_address[1], "synthetic-key", PROBES)
        finally:
            s.shutdown()

    def test_refusing_double_passes(self):
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": "forbidden", "code": "scope"}))), 0)
        self.assertEqual(self.run_against(lambda m, p: (404, "")), 0)

    # mutations: each leak must be caught
    def test_leaks_fail(self):
        n = len(PROBES)
        self.assertEqual(self.run_against(lambda m, p: (200, "{}")), n)                                   # served
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"events": [{"id": 1}]}))), n)    # 4xx with data
        self.assertEqual(self.run_against(lambda m, p: (403, json.dumps({"error": {"events": [1]}}))), n) # nested data
        self.assertEqual(self.run_against(lambda m, p: (500, "")), n)                                      # crash is not a refusal
        self.assertEqual(self.run_against(lambda m, p: (302, "")), n)                                      # redirect
        self.assertEqual(self.run_against(lambda m, p: (403, "x" * 2000)), n)                              # large body
        self.assertEqual(self.run_against(lambda m, p: (403, "<html>events</html>")), n)                  # non-JSON
        # a partial leak: only /partner/events served
        self.assertEqual(self.run_against(lambda m, p: (200, "[]") if p == "/partner/events" else (403, "")), 1)

    def test_unreachable_is_fail_not_pass(self):
        self.assertEqual(ci.run("http://127.0.0.1:1", "k", PROBES[:1]), 1)

    def test_tightened_status(self):
        ci.EXPECTED_STATUS = 403
        try:
            self.assertEqual(self.run_against(lambda m, p: (404, "")), len(PROBES))
            self.assertEqual(self.run_against(lambda m, p: (403, "")), 0)
        finally:
            ci.EXPECTED_STATUS = None


if __name__ == "__main__":
    unittest.main()
