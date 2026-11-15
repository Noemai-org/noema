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
        w = ac.Store(ep, "eu-west-1", BUCKET, "WK", "synthetic-secret-w", local_selftest=True)
        v = ac.Store(ep, "eu-west-1", BUCKET, "VK", "synthetic-secret-v", local_selftest=True)
        return ac.run(w, v, "ci-probe/")
    finally:
        srv.shutdown()
        srv.server_close()


class EndpointValidation(unittest.TestCase):
    """Codex finding on noema#164: credentials must never reach a non-HTTPS or arbitrary host. Every rejected form is checked twice:
    the Store refuses to be built, and no socket is opened (a listener on 127.0.0.1 sees no connection)."""

    BAD = {
        "plain http": "http://s3.eu-west-1.amazonaws.com",
        "arbitrary host": "https://evil.example.com",
        "aws name as a prefix of another host": "https://s3.eu-west-1.amazonaws.com.evil.example.com",
        "aws name as userinfo": "https://s3.eu-west-1.amazonaws.com@evil.example.com",
        "userinfo": "https://user:pw@s3.eu-west-1.amazonaws.com",
        "other port": "https://s3.eu-west-1.amazonaws.com:8443",
        "explicit 443": "https://s3.eu-west-1.amazonaws.com:443",
        "path": "https://s3.eu-west-1.amazonaws.com/bucket",
        "query": "https://s3.eu-west-1.amazonaws.com?x=1",
        "fragment": "https://s3.eu-west-1.amazonaws.com#x",
        "upper case": "https://S3.EU-WEST-1.AMAZONAWS.COM",
        "scheme in capitals": "HTTPS://s3.eu-west-1.amazonaws.com",
        "trailing dot": "https://s3.eu-west-1.amazonaws.com.",
        "ip address": "https://52.95.0.1",
        "loopback": "https://127.0.0.1",
        "region differs from the endpoint": "https://s3.us-east-1.amazonaws.com",
        "virtual-hosted bucket host": "https://bucket.s3.eu-west-1.amazonaws.com",
        "dualstack host": "https://s3.dualstack.eu-west-1.amazonaws.com",
        "china domain": "https://s3.eu-west-1.amazonaws.com.cn",
        "backslash trick": "https://evil.example.com\\@s3.eu-west-1.amazonaws.com",
        "unicode look-alike": "https://s3.eu-west-1.amazonaws.com".replace("a", "\u0430", 1),
        "whitespace": " https://s3.eu-west-1.amazonaws.com",
        "newline": "https://s3.eu-west-1.amazonaws.com\n",
        "empty": "",
        "no scheme": "s3.eu-west-1.amazonaws.com",
    }

    def listener(self):
        import socket
        s = socket.socket()
        s.bind(("127.0.0.1", 0))
        s.listen(5)
        s.settimeout(0.3)
        return s

    def test_every_rejected_form_is_refused_before_any_credential_is_used(self):
        for name, ep in self.BAD.items():
            with self.subTest(form=name):
                with self.assertRaises(ac.EndpointRefused):
                    ac.Store(ep, "eu-west-1", "bucket-ok", "AKIDEXAMPLE", "synthetic-secret")

    def test_the_only_accepted_form_is_the_exact_regional_endpoint(self):
        st = ac.Store("https://s3.eu-west-1.amazonaws.com", "eu-west-1", "bucket-ok", "AK", "SK")
        self.assertEqual((st.scheme, st.host, st.port), ("https", "s3.eu-west-1.amazonaws.com", 443))

    def test_a_bad_region_or_bucket_is_refused(self):
        for region in ("", "eu-west-1.evil.com", "EU-WEST-1", "eu west 1", "../x", "eu-west-1/", "a"):
            with self.subTest(region=region), self.assertRaises(ac.EndpointRefused):
                ac.Store("https://s3.eu-west-1.amazonaws.com", region, "bucket-ok", "AK", "SK")
        for bucket in ("", "UPPER", "a/b", "bucket?x", "-lead", "trail-", "a" * 64, "b ucket", "bucket@host", "a..b"):
            with self.subTest(bucket=bucket), self.assertRaises(ac.EndpointRefused):
                ac.Store("https://s3.eu-west-1.amazonaws.com", "eu-west-1", bucket, "AK", "SK")

    def test_main_ignores_an_endpoint_override_and_never_uses_plain_http(self):
        # ANCHOR_ENDPOINT and ANCHOR_ALLOW_PLAINTEXT no longer exist: main derives the endpoint from the region.
        old = os.environ.copy()
        lst = self.listener()
        try:
            os.environ.update(ANCHOR_BUCKET="bucket-ok", ANCHOR_REGION="eu-west-1", ANCHOR_WRITER_KEY_ID="a", ANCHOR_WRITER_SECRET="b",
                              ANCHOR_VERIFIER_KEY_ID="c", ANCHOR_VERIFIER_SECRET="d",
                              ANCHOR_ENDPOINT="http://127.0.0.1:%d" % lst.getsockname()[1], ANCHOR_ALLOW_PLAINTEXT="selftest")
            self.assertEqual(ac.main(["--validate-only"]), 0)
            with self.assertRaises(OSError):
                lst.accept()  # nothing connected to the listener
        finally:
            lst.close()
            os.environ.clear()
            os.environ.update(old)

    def test_main_refuses_a_bad_region_without_a_request(self):
        old = os.environ.copy()
        try:
            os.environ.update(ANCHOR_BUCKET="bucket-ok", ANCHOR_REGION="eu-west-1.evil.example.com", ANCHOR_WRITER_KEY_ID="a",
                              ANCHOR_WRITER_SECRET="b", ANCHOR_VERIFIER_KEY_ID="c", ANCHOR_VERIFIER_SECRET="d")
            self.assertEqual(ac.main(["--validate-only"]), 2)
        finally:
            os.environ.clear()
            os.environ.update(old)

    def test_a_credential_is_not_signed_for_a_refused_endpoint(self):
        # call() re-validates: a Store whose fields were changed after construction cannot send.
        st = ac.Store("https://s3.eu-west-1.amazonaws.com", "eu-west-1", "bucket-ok", "AK", "SK")
        st.host = "evil.example.com"
        with self.assertRaises(ac.EndpointRefused):
            st.call("GET", "x")


class AwsCheck(unittest.TestCase):
    def test_a_correct_store_passes(self):
        self.assertEqual(check(), 0)

    def test_every_flaw_is_caught(self):
        for flaw in ("writer_deletes", "overwrite", "shorten", "writer_lists", "marker", "nolock", "governance", "noretention"):
            with self.subTest(flaw=flaw):
                self.assertGreater(check(**{flaw: True}), 0, flaw + " was not caught")

    def test_unreachable_store_fails(self):
        w = ac.Store("http://127.0.0.1:1", "eu-west-1", BUCKET, "WK", "x", local_selftest=True)
        self.assertGreater(ac.run(w, w, "ci-probe/"), 0)


if __name__ == "__main__":
    unittest.main()
