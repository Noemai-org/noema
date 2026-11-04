"""Self-test of aws_check.py against an in-process SYNTHETIC S3 model (versioned, locked). No network beyond 127.0.0.1.
A correct model must pass; each mutation (a store that lets the writer delete, overwrite, shorten retention, list, hide the object
behind a delete marker, or that has no lock) must fail. Run: python3 -I -m unittest test_aws_check -v   (from this directory)
"""
import hashlib
import http.server
import importlib.util
import os
import re
import threading
import unittest
import urllib.parse

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("aws_check", os.path.join(HERE, "aws_check.py"))
ac = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ac)

BUCKET = "synthetic-anchor-test"


class Model:
    def __init__(self, **flaws):
        self.flaws = flaws
        self.versions = []  # (key, vid, bytes, marker)
        self.n = 0


def make_server(model):
    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def who(self):
            m = re.search(r"Credential=([^/]+)/", self.headers.get("Authorization", ""))
            return {"WK": "writer", "VK": "verifier"}.get(m.group(1) if m else "", "nobody")

        def send(self, code, body=b"", headers=None):
            self.send_response(code)
            for k, v in (headers or {}).items():
                self.send_header(k, v)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def deny(self, code=403, name="AccessDenied"):
            self.send(code, ("<Error><Code>%s</Code></Error>" % name).encode())

        def handle_any(self):
            u = urllib.parse.urlparse(self.path)
            q = dict(urllib.parse.parse_qsl(u.query, keep_blank_values=True))
            key = urllib.parse.unquote(u.path)[len("/" + BUCKET):].lstrip("/")
            body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
            who, f, m = self.who(), model.flaws, self.command
            if not key:  # bucket level
                if m == "GET" and "object-lock" in q:
                    mode = "GOVERNANCE" if f.get("governance") else "COMPLIANCE"
                    on = "Disabled" if f.get("nolock") else "Enabled"
                    return self.send(200, ("<ObjectLockConfiguration><ObjectLockEnabled>%s</ObjectLockEnabled><Rule><DefaultRetention><Mode>%s</Mode></DefaultRetention></Rule></ObjectLockConfiguration>" % (on, mode)).encode())
                if m == "GET" and "versioning" in q:
                    return self.send(200, b"<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>")
                if m == "GET" and who == "writer" and not f.get("writer_lists"):
                    return self.deny()
                if m == "GET":
                    out = ""
                    for k, vid, _, marker in model.versions:
                        if k.startswith(q.get("prefix", "")):
                            last = [x for x in model.versions if x[0] == k][-1] is (k, vid, _, marker) or [x for x in model.versions if x[0] == k][-1][1] == vid
                            if marker:
                                out += "<DeleteMarker><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%s</IsLatest></DeleteMarker>" % (k, vid, str(last).lower())
                            else:
                                out += "<Version><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%s</IsLatest></Version>" % (k, vid, str(last).lower())
                    return self.send(200, ("<ListVersionsResult>%s</ListVersionsResult>" % out).encode())
            if m == "PUT" and "retention" in q:
                return self.send(200) if f.get("shorten") and who == "writer" else self.deny()
            if m == "PUT":
                exists = any(k == key and not mk for k, _, _, mk in model.versions)
                if who == "verifier" or who == "nobody":
                    return self.deny()
                if exists and not f.get("overwrite"):
                    return self.deny(412, "PreconditionFailed") if self.headers.get("If-None-Match") else self.deny()
                model.n += 1
                vid = "v%d" % model.n
                model.versions.append((key, vid, body, False))
                return self.send(200, headers={"x-amz-version-id": vid})
            if m == "DELETE":
                if who != "writer" or not (f.get("writer_deletes") or f.get("marker")):
                    return self.deny()
                if f.get("marker") and "versionId" not in q:
                    model.n += 1
                    model.versions.append((key, "m%d" % model.n, b"", True))
                    return self.send(204)
                model.versions = [x for x in model.versions if x[1] != q.get("versionId")]
                return self.send(204)
            if m == "GET":
                for k, vid, data, marker in model.versions:
                    if k == key and vid == q.get("versionId") and not marker:
                        h = {"x-amz-object-lock-mode": "COMPLIANCE", "x-amz-object-lock-retain-until-date": "2099-01-01T00:00:00.000Z"}
                        if f.get("noretention"):
                            h = {}
                        return self.send(200, data, h)
                return self.deny(404, "NoSuchKey")
            self.deny()

        do_GET = do_PUT = do_DELETE = handle_any

    srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def check(**flaws):
    model = Model(**flaws)
    srv = make_server(model)
    try:
        ep = "http://127.0.0.1:%d" % srv.server_address[1]
        w = ac.Store(ep, "eu-west-1", BUCKET, "WK", "synthetic-secret-w")
        v = ac.Store(ep, "eu-west-1", BUCKET, "VK", "synthetic-secret-v")
        return ac.run(w, v, "ci-probe/")
    finally:
        srv.shutdown()
        srv.server_close()


class AwsCheck(unittest.TestCase):
    def test_a_correct_store_passes(self):
        self.assertEqual(check(), 0)

    def test_every_flaw_is_caught(self):
        for flaw in ("writer_deletes", "overwrite", "shorten", "writer_lists", "marker", "nolock", "governance", "noretention"):
            with self.subTest(flaw=flaw):
                self.assertGreater(check(**{flaw: True}), 0, flaw + " was not caught")

    def test_unreachable_store_fails(self):
        w = ac.Store("http://127.0.0.1:1", "eu-west-1", BUCKET, "WK", "x")
        self.assertGreater(ac.run(w, w, "ci-probe/"), 0)

    def test_plain_http_is_refused_by_main(self):
        env = dict(os.environ, ANCHOR_BUCKET="b", ANCHOR_REGION="r", ANCHOR_WRITER_KEY_ID="a", ANCHOR_WRITER_SECRET="b",
                   ANCHOR_VERIFIER_KEY_ID="c", ANCHOR_VERIFIER_SECRET="d", ANCHOR_ENDPOINT="http://127.0.0.1:1")
        old = os.environ.copy()
        os.environ.update(env)
        os.environ.pop("ANCHOR_ALLOW_PLAINTEXT", None)
        try:
            self.assertEqual(ac.main(), 2)
        finally:
            os.environ.clear()
            os.environ.update(old)


if __name__ == "__main__":
    unittest.main()
