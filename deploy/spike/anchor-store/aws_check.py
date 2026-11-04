#!/usr/bin/env python3
"""SEC-14 anchoring requirement against AWS S3 Object Lock (standard library only; SigV4 from s3.py).

Proves, with two identities, that an anchored object cannot be removed or replaced by the writer and stays listed:
  writer   (put-only):  can PUT a new key; cannot overwrite it, delete it, delete a version, or shorten its retention; cannot list.
  verifier (read-only): sees the object in the version listing as the latest version, not hidden by a delete marker; reads the same
                        bytes; sees COMPLIANCE retention in the future; cannot write or delete.
Anything other than the expected refusal is a FAIL, and so is a dropped connection or a 5xx ("unknown" never passes).
Facts about AWS's behaviour are UNVERIFIED until this has run against the real account; the run is the verification.

Environment: ANCHOR_BUCKET, ANCHOR_REGION, ANCHOR_WRITER_KEY_ID, ANCHOR_WRITER_SECRET, ANCHOR_VERIFIER_KEY_ID, ANCHOR_VERIFIER_SECRET;
optional ANCHOR_ENDPOINT (default https://s3.<region>.amazonaws.com; http:// allowed only for the local self-test).
Secrets are read from the environment and never printed. Test objects live under ci-probe/ and expire by the bucket's lifecycle rule.
"""
import base64
import datetime
import hashlib
import http.client
import importlib.util
import os
import re
import sys
import urllib.parse
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("s3", os.path.join(HERE, "s3.py"))
s3 = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(s3)


class Store:
    def __init__(self, endpoint, region, bucket, key_id, secret):
        u = urllib.parse.urlparse(endpoint)
        self.scheme, self.host, self.port = u.scheme, u.hostname, u.port or (443 if u.scheme == "https" else 80)
        self.region, self.bucket, self.ak, self.sk = region, bucket, key_id, secret

    def call(self, method, key="", query=None, body=b"", headers=None):
        query = query or {}
        now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        payload = hashlib.sha256(body).hexdigest()
        host_hdr = self.host if self.port in (80, 443) else "%s:%d" % (self.host, self.port)
        hdrs = {"host": host_hdr, "x-amz-content-sha256": payload, "x-amz-date": now}
        for k, v in (headers or {}).items():
            hdrs[k.lower()] = v
        path = "/" + self.bucket + ("/" + key if key else "")
        auth = s3.sign(method, host_hdr, path, query, hdrs, payload, self.ak, self.sk, now, self.region)
        send = {k: v for k, v in hdrs.items() if k != "host"}
        send["Authorization"] = auth
        target = urllib.parse.quote(path, safe="/-_.~") + ("?" + urllib.parse.urlencode(query, quote_via=urllib.parse.quote) if query else "")
        cls = http.client.HTTPSConnection if self.scheme == "https" else http.client.HTTPConnection
        conn = cls(self.host, self.port, timeout=20)
        try:
            conn.request(method, target, body=body or None, headers=send)
            r = conn.getresponse()
            return r.status, {k.lower(): v for k, v in r.getheaders()}, r.read()
        finally:
            conn.close()


def code_of(body):
    m = re.search(rb"<Code>([^<]+)</Code>", body or b"")
    return m.group(1).decode() if m else ""


class Report:
    def __init__(self):
        self.fails = 0

    def row(self, cid, label, ok, detail=""):
        print("%s %s %s%s" % ("PASS" if ok else "FAIL", cid, label, (" (" + detail + ")") if detail else ""))
        if not ok:
            self.fails += 1


def safe(fn):
    """A dropped connection is a FAIL row, never a pass and never a crash that hides the rest."""
    try:
        return fn()
    except OSError as e:
        return (0, {}, ("connection error: %s" % type(e).__name__).encode())


def refused(res):
    status, _, body = res
    return (status in (403, 412, 409) or status == 405), "status %s %s" % (status, code_of(body))


def run(writer, verifier, prefix):
    rep = Report()
    key = "%s%s.json" % (prefix, uuid.uuid4().hex)
    data = ('{"synthetic":true,"anchor_test":"%s"}' % uuid.uuid4().hex).encode()
    digest = hashlib.sha256(data).hexdigest()

    st, hd, body = safe(lambda: writer.call("PUT", key, body=data, headers={"if-none-match": "*", "content-type": "application/json"}))
    rep.row("W1", "writer puts a new key", st == 200, "status %s %s" % (st, code_of(body)))
    version = hd.get("x-amz-version-id", "")
    rep.row("W1b", "the put returns a version id (versioning is on)", bool(version))
    if st != 200:
        return rep.fails + 1

    ok, d = refused(safe(lambda: writer.call("PUT", key, body=b'{"synthetic":true,"tampered":1}', headers={"if-none-match": "*"})))
    rep.row("W2", "writer overwrite with If-None-Match is refused", ok, d)
    ok, d = refused(safe(lambda: writer.call("PUT", key, body=b'{"synthetic":true,"tampered":2}')))
    rep.row("W3", "writer overwrite without If-None-Match is refused (bucket policy)", ok, d)
    ok, d = refused(safe(lambda: writer.call("DELETE", key)))
    rep.row("W4", "writer delete (would add a delete marker) is refused", ok, d)
    ok, d = refused(safe(lambda: writer.call("DELETE", key, {"versionId": version})))
    rep.row("W5", "writer delete of the version is refused", ok, d)
    retention = (b'<Retention xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Mode>COMPLIANCE</Mode>'
                 b'<RetainUntilDate>2000-01-01T00:00:00Z</RetainUntilDate></Retention>')
    md5 = base64.b64encode(hashlib.md5(retention).digest()).decode()
    ok, d = refused(safe(lambda: writer.call("PUT", key, {"retention": "", "versionId": version}, body=retention, headers={"content-md5": md5})))
    rep.row("W6", "writer cannot shorten retention", ok, d)
    ok, d = refused(safe(lambda: writer.call("GET", "", {"list-type": "2", "prefix": prefix})))
    rep.row("W7", "writer cannot list the bucket (put-only)", ok, d)

    st, hd, body = safe(lambda: verifier.call("GET", "", {"versions": "", "prefix": key}))
    text = body.decode("utf-8", "replace")
    present = st == 200 and ("<Key>%s</Key>" % key) in text and ("<VersionId>%s</VersionId>" % version) in text
    rep.row("V1", "verifier lists the original version", present, "status %s" % st)
    latest = re.search(r"<Version>(?:(?!</Version>).)*?<Key>%s</Key>(?:(?!</Version>).)*?<IsLatest>true</IsLatest>" % re.escape(key), text, re.S)
    rep.row("V2", "the original version is still the latest", bool(latest))
    rep.row("V3", "no delete marker hides the key", st == 200 and "<DeleteMarker>" not in text)
    st, hd, body = safe(lambda: verifier.call("GET", key, {"versionId": version}))
    rep.row("V4", "verifier reads the same bytes", st == 200 and hashlib.sha256(body).hexdigest() == digest, "status %s" % st)
    until = hd.get("x-amz-object-lock-retain-until-date", "")
    future = False
    try:
        future = datetime.datetime.strptime(until[:19], "%Y-%m-%dT%H:%M:%S").replace(tzinfo=datetime.timezone.utc) > datetime.datetime.now(datetime.timezone.utc)
    except ValueError:
        pass
    rep.row("V5", "retention mode is COMPLIANCE", hd.get("x-amz-object-lock-mode") == "COMPLIANCE", hd.get("x-amz-object-lock-mode", "none"))
    rep.row("V6", "retention runs until a future date", future)
    st, _, body = safe(lambda: verifier.call("GET", "", {"object-lock": ""}))
    t = body.decode("utf-8", "replace")
    rep.row("V7", "bucket Object Lock is enabled with a COMPLIANCE default", st == 200 and "<ObjectLockEnabled>Enabled</ObjectLockEnabled>" in t and "<Mode>COMPLIANCE</Mode>" in t, "status %s" % st)
    st, _, body = safe(lambda: verifier.call("GET", "", {"versioning": ""}))
    rep.row("V8", "bucket versioning is Enabled", st == 200 and "<Status>Enabled</Status>" in body.decode("utf-8", "replace"), "status %s" % st)
    ok, d = refused(safe(lambda: verifier.call("PUT", prefix + "verifier-must-not-write.json", body=b"{}")))
    rep.row("V9", "verifier cannot write", ok, d)
    ok, d = refused(safe(lambda: verifier.call("DELETE", key, {"versionId": version})))
    rep.row("V10", "verifier cannot delete", ok, d)

    # Last: the object is still there after every attempt above.
    st, hd, body = safe(lambda: verifier.call("GET", key, {"versionId": version}))
    rep.row("Z1", "after all attempts the object is still readable and unchanged", st == 200 and hashlib.sha256(body).hexdigest() == digest, "status %s" % st)
    print("anchoring: %s (%d failed rows)" % ("PASS" if rep.fails == 0 else "FAIL", rep.fails))
    return rep.fails


def main():
    s3.selfcheck()
    need = ["ANCHOR_BUCKET", "ANCHOR_REGION", "ANCHOR_WRITER_KEY_ID", "ANCHOR_WRITER_SECRET", "ANCHOR_VERIFIER_KEY_ID", "ANCHOR_VERIFIER_SECRET"]
    missing = [n for n in need if not os.environ.get(n)]
    if missing:
        print("FAIL: missing " + ", ".join(missing) + " (unknown, not pass)")
        return 2
    region, bucket = os.environ["ANCHOR_REGION"], os.environ["ANCHOR_BUCKET"]
    endpoint = os.environ.get("ANCHOR_ENDPOINT") or "https://s3.%s.amazonaws.com" % region
    if endpoint.startswith("http://") and os.environ.get("ANCHOR_ALLOW_PLAINTEXT") != "selftest":
        print("FAIL: plain http is for the local self-test only")
        return 2
    w = Store(endpoint, region, bucket, os.environ["ANCHOR_WRITER_KEY_ID"], os.environ["ANCHOR_WRITER_SECRET"])
    v = Store(endpoint, region, bucket, os.environ["ANCHOR_VERIFIER_KEY_ID"], os.environ["ANCHOR_VERIFIER_SECRET"])
    return 1 if run(w, v, "ci-probe/") else 0


if __name__ == "__main__":
    sys.exit(main())
