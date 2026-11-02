"""Minimal AWS Signature Version 4 client for S3 (standard library only). Test infrastructure for the SEC-14 spike.

It exists so the spike adds no client dependency. `selfcheck()` reproduces the signature in AWS's published example
(Amazon S3 'GET Object with a Range header': examplebucket, /test.txt, bytes=0-9, 2013-05-24T00:00:00Z) before any
request is signed; a wrong implementation stops the run.
"""
import hashlib
import hmac
import http.client
import urllib.parse

EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()


def _hmac(key, msg):
    return hmac.new(key, msg.encode(), hashlib.sha256).digest()


def sign(method, host, path, query, headers, payload_hash, access_key, secret_key, amz_date, region="us-east-1"):
    """Returns the Authorization header value. `headers` are the headers to sign (lower-case names, host included)."""
    date = amz_date[:8]
    canon_query = "&".join(
        f"{urllib.parse.quote(k, safe='-_.~')}={urllib.parse.quote(v, safe='-_.~')}" for k, v in sorted(query.items()))
    names = sorted(headers)
    canon_headers = "".join(f"{n}:{' '.join(str(headers[n]).split())}\n" for n in names)
    signed = ";".join(names)
    canonical = "\n".join([method, urllib.parse.quote(path, safe="/-_.~"), canon_query, canon_headers, signed, payload_hash])
    scope = f"{date}/{region}/s3/aws4_request"
    to_sign = "\n".join(["AWS4-HMAC-SHA256", amz_date, scope, hashlib.sha256(canonical.encode()).hexdigest()])
    k = _hmac(_hmac(_hmac(_hmac(("AWS4" + secret_key).encode(), date), region), "s3"), "aws4_request")
    signature = hmac.new(k, to_sign.encode(), hashlib.sha256).hexdigest()
    return f"AWS4-HMAC-SHA256 Credential={access_key}/{scope}, SignedHeaders={signed}, Signature={signature}"


def selfcheck():
    auth = sign("GET", "examplebucket.s3.amazonaws.com", "/test.txt", {},
                {"host": "examplebucket.s3.amazonaws.com", "range": "bytes=0-9", "x-amz-content-sha256": EMPTY_SHA256,
                 "x-amz-date": "20130524T000000Z"},
                EMPTY_SHA256, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "20130524T000000Z")
    want = "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
    if not auth.endswith("Signature=" + want):
        raise SystemExit("SigV4 self-check FAILED: " + auth)


class Client:
    """One S3 identity talking to host:port. Every call has a timeout. SYNTHETIC credentials only."""

    def __init__(self, host, port, access_key, secret_key, clock):
        self.host, self.port, self.ak, self.sk, self.clock = host, port, access_key, secret_key, clock

    def request(self, method, path, query=None, body=b"", headers=None, timeout=15):
        query = query or {}
        amz_date = self.clock()
        payload_hash = hashlib.sha256(body).hexdigest()
        hdrs = {"host": f"{self.host}:{self.port}", "x-amz-content-sha256": payload_hash, "x-amz-date": amz_date}
        for k, v in (headers or {}).items():
            hdrs[k.lower()] = v
        if body:
            hdrs["content-length"] = str(len(body))
        auth = sign(method, self.host, path, query, hdrs, payload_hash, self.ak, self.sk, amz_date)
        send = {k: v for k, v in hdrs.items() if k != "host"}
        send["Authorization"] = auth
        target = path + ("?" + urllib.parse.urlencode(query, quote_via=urllib.parse.quote) if query else "")
        conn = http.client.HTTPConnection(self.host, self.port, timeout=timeout)
        try:
            conn.request(method, target, body=body or None, headers=send)
            resp = conn.getresponse()
            data = resp.read()
            return resp.status, {k.lower(): v for k, v in resp.getheaders()}, data
        finally:
            conn.close()
