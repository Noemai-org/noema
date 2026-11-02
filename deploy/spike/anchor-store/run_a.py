#!/usr/bin/env python3
"""SEC-14 spike driver: checks A1 to A7 of docs/security/sec-14-anchor-store-evaluation.md against an S3 object-lock store.
Test infrastructure only. SYNTHETIC credentials and data. Usage: run_a.py <store-name> <container> <port> <image-ref>
Refusal means an HTTP 4xx answer; a 5xx or a dropped connection is reported, not counted as a refusal.
"""
import datetime
import importlib.util
import os
import re
import socket
import subprocess
import sys
import time
import xml.sax.saxutils as xs

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("s3", os.path.join(HERE, "s3.py"))
s3 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(s3)

STORE, CONTAINER, PORT, IMAGE = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
KEYS = {  # SYNTHETIC
    "admin": ("SYNTHETICADMINKEY0001", "synthetic-admin-secret-0001-not-a-real-secret"),
    "core": ("SYNTHETICCOREKEY00001", "synthetic-core-secret-0001-not-a-real-secret"),
    "verifier": ("SYNTHETICVERIFYKEY001", "synthetic-verify-secret-001-not-a-real-secret"),
}
NS = 'xmlns="http://s3.amazonaws.com/doc/2006-03-01/"'
BUCKET = "anchors"
ROWS, FAILS = [], 0


def now():
    return datetime.datetime.now(datetime.timezone.utc)


def amz_date():
    return now().strftime("%Y%m%dT%H%M%SZ")


def iso(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%SZ")


CL = {name: s3.Client("127.0.0.1", PORT, ak, sk, amz_date) for name, (ak, sk) in KEYS.items()}


def code_of(body):
    m = re.search(rb"<Code>([^<]+)</Code>", body or b"")
    return m.group(1).decode() if m else ""


def record(cid, label, ok, detail=""):
    global FAILS
    if not ok:
        FAILS += 1
    ROWS.append((cid, label, "PASS" if ok else "**FAIL**", detail))
    print(("PASS " if ok else "FAIL ") + cid + " " + label + (": " + detail if detail else ""), flush=True)


def info(cid, label, detail):
    ROWS.append((cid, label, "INFO", detail))
    print("INFO " + cid + " " + label + ": " + detail, flush=True)


def call(who, method, path, **kw):
    try:
        return CL[who].request(method, path, **kw)
    except OSError as e:  # connection refused, reset, timeout
        return None, {}, repr(e).encode()


def describe(r):
    st, _, body = r
    return "no answer: " + body.decode()[:80] if st is None else f"{st} {code_of(body)}".strip()


MALFORMED = {"MissingContentMD5", "MalformedXML", "InvalidArgument", "MissingContentLength", "SignatureDoesNotMatch", "InvalidDigest", "BadDigest",
             "InvalidRequestBody", "XAmzContentSHA256Mismatch"}
LAYER = {"AccessDenied": "authorization", "InvalidBucketState": "bucket state, NOT the credential", "BucketNotEmpty": "bucket state, NOT the credential",
         "InvalidRequest": "object-lock rule", "NoSuchVersion": "none"}


def refused(cid, label, who, method, path, **kw):
    """A refusal counts only as a 4xx that is a decision of the store. A request the store could not parse is inconclusive and FAILS."""
    r = call(who, method, path, **kw)
    code = code_of(r[2]) if r[0] is not None else ""
    if code in MALFORMED:
        record(cid, label, False, f"INCONCLUSIVE, the store rejected the request as malformed: {describe(r)}")
    else:
        record(cid, label, r[0] is not None and 400 <= r[0] < 500, describe(r) + (f" [{LAYER[code]}]" if code in LAYER else ""))
    return r


def ok_call(cid, label, who, method, path, want=200, **kw):
    r = call(who, method, path, **kw)
    record(cid, label, r[0] == want, describe(r))
    return r


def wait_ready(seconds=90):
    end = time.time() + seconds
    while time.time() < end:
        r = call("admin", "GET", "/")
        if r[0] == 200:
            return True
        time.sleep(2)
    return False


def lock_cfg(mode, days):
    return (f'<ObjectLockConfiguration {NS}><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention>'
            f'<Mode>{mode}</Mode><Days>{days}</Days></DefaultRetention></Rule></ObjectLockConfiguration>').encode()


def retention(mode, until):
    return f'<Retention {NS}><Mode>{mode}</Mode><RetainUntilDate>{iso(until)}</RetainUntilDate></Retention>'.encode()


def put_anchor(who, key, text, extra=None):
    return call(who, "PUT", f"/{BUCKET}/{key}", body=text.encode(), headers=extra or {})


s3.selfcheck()

# A1: the store runs from the pinned image and answers S3.
digest = subprocess.run(["docker", "inspect", "--format", "{{index .Image}}", CONTAINER], capture_output=True, text=True).stdout.strip()
record("A1", "store started from the pinned image and answers S3 (ListBuckets as the admin identity)", wait_ready(), IMAGE)
info("A1", "image id of the running container", digest)

# A2: Object Lock bucket, default COMPLIANCE retention.
ok_call("A2", "create the bucket with Object Lock enabled", "admin", "PUT", f"/{BUCKET}", headers={"x-amz-bucket-object-lock-enabled": "true"})
ok_call("A2", "set default retention COMPLIANCE, 1 day", "admin", "PUT", f"/{BUCKET}", query={"object-lock": ""}, body=lock_cfg("COMPLIANCE", 1))
r = call("admin", "GET", f"/{BUCKET}", query={"object-lock": ""})
record("A2", "the bucket reports COMPLIANCE default retention", r[0] == 200 and b"COMPLIANCE" in r[2], describe(r))
w1 = put_anchor("core", "head-0001", "SYNTHETIC anchor 1")
V1 = w1[1].get("x-amz-version-id", "")
record("A2", "the core identity writes an anchor and gets a version id", w1[0] == 200 and V1 != "", describe(w1) + f" version={V1[:12]}")
h = call("admin", "HEAD", f"/{BUCKET}/head-0001", query={"versionId": V1})
until = h[1].get("x-amz-object-lock-retain-until-date", "")
mode = h[1].get("x-amz-object-lock-mode", "")
try:
    held = (datetime.datetime.strptime(until[:19], "%Y-%m-%dT%H:%M:%S").replace(tzinfo=datetime.timezone.utc) - now()).total_seconds()
except ValueError:
    held = -1
record("A2", "the written object is COMPLIANCE and retained for about the default period (more than 23 hours)", mode == "COMPLIANCE" and held > 23 * 3600,
       f"mode={mode} retain-until={until} ({int(held)} s ahead)")
refused("A2", "the admin identity cannot delete the locked version", "admin", "DELETE", f"/{BUCKET}/head-0001", query={"versionId": V1})
refused("A2", "the core identity cannot delete the locked version", "core", "DELETE", f"/{BUCKET}/head-0001", query={"versionId": V1})

# A3: the core credential is write-only.
refused("A3", "core: GetObject", "core", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})
refused("A3", "core: ListObjectsV2", "core", "GET", f"/{BUCKET}", query={"list-type": "2"})
refused("A3", "core: shorten retention (PutObjectRetention, earlier date)", "core", "PUT", f"/{BUCKET}/head-0001", query={"retention": "", "versionId": V1},
        body=retention("COMPLIANCE", now() + datetime.timedelta(hours=1)))
refused("A3", "core: PutObjectLegalHold", "core", "PUT", f"/{BUCKET}/head-0001", query={"legal-hold": "", "versionId": V1},
        body=f'<LegalHold {NS}><Status>ON</Status></LegalHold>'.encode())
refused("A3", "core: change the bucket's Object Lock configuration", "core", "PUT", f"/{BUCKET}", query={"object-lock": ""}, body=lock_cfg("COMPLIANCE", 0 + 1))
refused("A3", "core: suspend bucket versioning", "core", "PUT", f"/{BUCKET}", query={"versioning": ""},
        body=f'<VersioningConfiguration {NS}><Status>Suspended</Status></VersioningConfiguration>'.encode())
refused("A3", "core: DeleteBucket", "core", "DELETE", f"/{BUCKET}")
w2 = put_anchor("core", "head-0001", "SYNTHETIC anchor 1 OVERWRITE ATTEMPT")
V2 = w2[1].get("x-amz-version-id", "")
record("A3", "core: writing the same key again creates a new version and never replaces the first", w2[0] == 200 and V2 != "" and V2 != V1, describe(w2))
r1 = call("verifier", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})
record("A3", "the first version is intact after the overwrite attempt", r1[0] == 200 and r1[2] == b"SYNTHETIC anchor 1", describe(r1))
dm = call("core", "DELETE", f"/{BUCKET}/head-0001")
r1 = call("verifier", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})
info("A3", "core: DeleteObject without a version id", describe(dm))
record("A3", "whatever that answered, version 1 is still readable (a delete marker, if any, does not remove data)", r1[0] == 200 and r1[2] == b"SYNTHETIC anchor 1", describe(r1))

# A4: the administrator identity cannot shorten or remove COMPLIANCE retention through the API.
refused("A4", "admin: shorten retention (PutObjectRetention, earlier date)", "admin", "PUT", f"/{BUCKET}/head-0001", query={"retention": "", "versionId": V1},
        body=retention("COMPLIANCE", now() + datetime.timedelta(hours=1)))
refused("A4", "admin: change the mode to GOVERNANCE", "admin", "PUT", f"/{BUCKET}/head-0001", query={"retention": "", "versionId": V1},
        body=retention("GOVERNANCE", now() + datetime.timedelta(days=2)))
refused("A4", "admin: delete the version with x-amz-bypass-governance-retention", "admin", "DELETE", f"/{BUCKET}/head-0001", query={"versionId": V1},
        headers={"x-amz-bypass-governance-retention": "true"})
refused("A4", "admin: suspend bucket versioning", "admin", "PUT", f"/{BUCKET}", query={"versioning": ""},
        body=f'<VersioningConfiguration {NS}><Status>Suspended</Status></VersioningConfiguration>'.encode())
refused("A4", "admin: DeleteBucket", "admin", "DELETE", f"/{BUCKET}")
h = call("verifier", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})
record("A4", "after every admin attempt the first version is intact", h[0] == 200 and h[2] == b"SYNTHETIC anchor 1", describe(h))
exposed = []
for p in (8888, 9333, 8080, 18888, 19333, 9001):
    s = socket.socket()
    s.settimeout(2)
    if s.connect_ex(("127.0.0.1", p)) == 0:
        exposed.append(p)
    s.close()
record("A4", "no filer, master or volume port is published to the host (only the S3 port)", not exposed, f"reachable: {exposed}")

# A5: the verifier's credential reads and cannot write.
record("A5", "verifier: GetObject returns the anchor", call("verifier", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})[2] == b"SYNTHETIC anchor 1")
vers = call("verifier", "GET", f"/{BUCKET}", query={"versions": ""})
record("A5", "verifier: ListObjectVersions shows the key and version 1", vers[0] == 200 and b"head-0001" in vers[2] and V1.encode() in vers[2], describe(vers))
lst = call("verifier", "GET", f"/{BUCKET}", query={"list-type": "2"})
info("A5", "verifier: the default listing (ListObjectsV2) after the core created a delete marker in A3", f"{describe(lst)}; key shown: {b'head-0001' in lst[2]}. "
     "A write-only writer can hide a key from the default listing with a delete marker (data intact); the verifier must list versions and address them by version id")
refused("A5", "verifier: PutObject", "verifier", "PUT", f"/{BUCKET}/head-9999", body=b"x")
refused("A5", "verifier: delete the version", "verifier", "DELETE", f"/{BUCKET}/head-0001", query={"versionId": V1})

# A7: the retention period comes from the store, not from the writer.
past = put_anchor("core", "head-past", "SYNTHETIC", {"x-amz-object-lock-mode": "COMPLIANCE", "x-amz-object-lock-retain-until-date": iso(now() - datetime.timedelta(days=1))})
record("A7", "core: an explicit retain-until date in the past is refused", past[0] is not None and 400 <= past[0] < 500, describe(past))
short = put_anchor("core", "head-short", "SYNTHETIC", {"x-amz-object-lock-mode": "COMPLIANCE", "x-amz-object-lock-retain-until-date": iso(now() + datetime.timedelta(hours=1))})
if short[0] == 200:
    sh = call("admin", "HEAD", f"/{BUCKET}/head-short", query={"versionId": short[1].get("x-amz-version-id", "")})
    u = sh[1].get("x-amz-object-lock-retain-until-date", "")
    try:
        ahead = (datetime.datetime.strptime(u[:19], "%Y-%m-%dT%H:%M:%S").replace(tzinfo=datetime.timezone.utc) - now()).total_seconds()
    except ValueError:
        ahead = -1
    record("A7", "core: a SHORTER explicit retention (1 hour) is refused or ignored; the default (1 day) holds", ahead > 23 * 3600, f"accepted with retain-until={u} ({int(ahead)} s ahead)")
else:
    record("A7", "core: a SHORTER explicit retention (1 hour) is refused or ignored; the default (1 day) holds", short[0] is not None and 400 <= short[0] < 500, describe(short))

# A6: the store is down: the failure is visible and a retry works.
subprocess.run(["docker", "stop", "-t", "5", CONTAINER], check=True, capture_output=True)
down = put_anchor("core", "head-0002", "SYNTHETIC anchor 2")
record("A6", "store down: the anchor write fails visibly (no answer), never silently", down[0] is None, describe(down))
subprocess.run(["docker", "start", CONTAINER], check=True, capture_output=True)
record("A6", "store restarted and answers again", wait_ready(), "")
t0 = time.time()
up = put_anchor("core", "head-0002", "SYNTHETIC anchor 2")
while up[0] != 200 and time.time() - t0 < 90:  # REQ-AUD-007: a missed anchor is retried. S3 answers before a volume server is ready.
    time.sleep(3)
    up = put_anchor("core", "head-0002", "SYNTHETIC anchor 2")
record("A6", "the anchor write, retried with backoff after the restart, succeeds within 90 s", up[0] == 200, f"{describe(up)} after {int(time.time() - t0)} s")
refused("A6", "after the restart the first version is still locked (admin delete refused)", "admin", "DELETE", f"/{BUCKET}/head-0001", query={"versionId": V1})
record("A6", "after the restart the first version is intact", call("verifier", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})[2] == b"SYNTHETIC anchor 1")

# LIMIT: the operator. Not counted as a pass or a fail: it states what the API-level control does not cover.
if STORE == "seaweedfs":
    ip = subprocess.run(["docker", "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", CONTAINER], capture_output=True, text=True).stdout.strip()
    import http.client
    for what, path in (("the object's path", f"/buckets/{BUCKET}/head-0001"), ("the locked version's file", f"/buckets/{BUCKET}/head-0001.versions/v_{V1}")):
        try:
            c = http.client.HTTPConnection(ip, 8888, timeout=10)
            c.request("DELETE", path)
            d = c.getresponse()
            d.read()
            gone = call("verifier", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})
            info("LIMIT", f"operator with network access to the filer (not published; reachable on the container network): DELETE {what} beneath the S3 API",
                 f"filer answered {d.status}; S3 read of version 1 now answers {describe(gone)}")
        except OSError as e:
            info("LIMIT", f"operator with network access to the filer: DELETE {what}", "could not connect to the filer: " + repr(e))
else:
    rm = subprocess.run(["docker", "exec", CONTAINER, "sh", "-c", f"rm -rf /data/{BUCKET}/head-0001"], capture_output=True, text=True)
    gone = call("verifier", "GET", f"/{BUCKET}/head-0001", query={"versionId": V1})
    info("LIMIT", "operator with access to the container removes the object's files under /data, beneath the S3 API",
         f"rm exit {rm.returncode} {rm.stderr.strip()[:80]}; S3 read of version 1 now answers {describe(gone)}")

out = os.environ.get("GITHUB_STEP_SUMMARY", os.devnull)
with open(out, "a") as f:
    f.write(f"## Anchor-store spike (SEC-14): {STORE}\nTest infrastructure only; nothing is adopted. SYNTHETIC keys and data.\n\n| Check | What | Result | Detail |\n|---|---|---|---|\n")
    for cid, label, res, det in ROWS:
        f.write(f"| {cid} | {label} | {res} | {det.replace('|', '/')[:200]} |\n")
    f.write(f"\n**{len([r for r in ROWS if r[2] == "PASS"])} passed, {FAILS} failed.** Rows marked INFO and LIMIT are evidence, not pass or fail.\n")
sys.exit(1 if FAILS else 0)
