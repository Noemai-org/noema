"""Isolation refusal check (SEC-02, REQ-ENV-001 / AT-02 / INV-22; standard library only).

With the runtime key, every probe must be refused: status 400-499 and no data body. "No data body" means the body is empty,
or a small JSON object whose keys are all in ERROR_KEYS and whose values are scalars. Anything else (a list, nested data,
a 2xx/3xx/5xx, a connection that answers with data) is a FAIL. A 5xx is a FAIL: a crash is not a refusal.

Until Tushe documents the exact status and body, any 4xx without data passes; tighten EXPECTED_STATUS when it arrives.
The Tushe admin credential is never used here; the key comes from a file named on the command line.

    python3 -I check_isolation.py --base-url URL --key-file FILE [--probes probes.json]
"""
import argparse
import json
import sys
import urllib.error
import urllib.request

ERROR_KEYS = {"error", "code", "message", "detail", "status", "request_id", "type", "title"}
EXPECTED_STATUS = None  # set to an int when Tushe documents the refusal status
MAX_BODY = 1024


def judge(status, body):
    """Returns (ok, reason). Pure function, so the mutation self-test can drive it."""
    if EXPECTED_STATUS is not None and status != EXPECTED_STATUS:
        return False, "status %s, expected %s" % (status, EXPECTED_STATUS)
    if not 400 <= status <= 499:
        return False, "status %s is not a 4xx refusal" % status
    if len(body) > MAX_BODY:
        return False, "body of %d bytes is more than an error envelope" % len(body)
    if not body.strip():
        return True, "refused with an empty body"
    try:
        doc = json.loads(body)
    except ValueError:
        return False, "non-JSON body on a refusal"
    if not isinstance(doc, dict):
        return False, "body is not an object"
    extra = sorted(set(doc) - ERROR_KEYS)
    if extra:
        return False, "body carries keys outside the error envelope: %s" % ",".join(extra)
    if any(isinstance(v, (dict, list)) for v in doc.values()):
        return False, "body nests data"
    return True, "refused with an error envelope"


def call(base, key, method, path, timeout=10):
    req = urllib.request.Request(base.rstrip("/") + path, method=method, data=(b"{}" if method in ("POST", "PUT") else None))
    req.add_header("Authorization", "Bearer " + key)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, r.read(MAX_BODY + 1).decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read(MAX_BODY + 1).decode("utf-8", "replace")


def run(base, key, probes):
    failed = 0
    for p in probes:
        try:
            status, body = call(base, key, p["method"], p["path"])
            ok, why = judge(status, body)
        except OSError as e:
            ok, why, status = False, "no answer (%s): unknown, counted as FAIL" % e, 0
        print("%s %s %s %s -> %s: %s" % ("PASS" if ok else "FAIL", p["id"], p["method"], p["path"], status, why))
        failed += 0 if ok else 1
    return failed


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", required=True)
    ap.add_argument("--key-file", required=True)
    ap.add_argument("--probes", default=__file__.rsplit("/", 1)[0] + "/probes.json" if "/" in __file__ else "probes.json")
    a = ap.parse_args()
    with open(a.key_file, encoding="utf-8") as f:
        key = f.read().strip()
    with open(a.probes, encoding="utf-8") as f:
        probes = json.load(f)["probes"]
    return 1 if run(a.base_url, key, probes) else 0


if __name__ == "__main__":
    sys.exit(main())
