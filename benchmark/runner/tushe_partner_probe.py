"""The refusal probe of Tushe's `/partner...` routes with the RUNTIME integration credential (G0-N isolation row; TEN-08; ruling A263).

What it shows: the credential the runtime holds is refused by Tushe on the administration routes. What makes a refusal worth something: a credential presented the wrong way is refused
on every route, so a refusal alone proves nothing. So the probe first makes a CONTROL request, a read that this credential is allowed to make, and every refusal counts only if the
control was served (2xx). Then each probe path must answer 401 or 403; a 2xx is a failure (the credential reached an administration route); any other answer (404, 5xx, a timeout) is
inconclusive and is NOT RUN, never a pass.

It makes GET requests only: a POST that Tushe wrongly accepted would create something. HTTPS only, no redirects, one request per path, the body of every answer discarded unread beyond the
status line, and the credential is read from a file (never an argument, an environment value or a log line) and never printed. Only the base URL and the credential file are required. The way the credential is presented is
found, not assumed (`--scheme auto` tries each against the control read and reports which was served); the control reads and the routes to refuse are the documented ones (the constants
below, with their sources; ruling A276). SYNTHETIC test environment only (AGENT-25).

Exit: 0 met, 1 not met (a probe was served, or the control was refused), 2 bad input, 3 NOT RUN (no credential, the control or a probe was inconclusive, or a transport failure).
"""
import argparse
import http.client
import json
import ssl
import sys
from pathlib import Path
from urllib.parse import urlsplit

Json = dict[str, object]
SCHEMES = ("bearer", "x-api-key")
# Where these defaults come from (ruling A276): no governing document says how a Tushe API key is presented, so the tool does not assume it: with `--scheme auto` (the default) it tries each way
# against the control read, and the way that is served is the way used and is written into the report. The control reads are Tushe's documented GETs of the event feed and the audit trail
# (delta v1.3 section 3, from the addendum of 15 Nov 2026, section 7); whether the RUNTIME credential may make either is not stated, so each is tried and none served is NOT RUN. The routes to
# refuse are the administration routes: `GET /partners/api-keys/scopes` (delta v1.3 section 2) and `GET /partner/events` (delta v1.2, AT-02 and AT-03: "an ordinary API key calling any
# /partner/... route is refused"). All are GETs that change nothing.
CONTROL_PATHS = ("/v1/events?limit=50", "/v1/audit-events")
PROBE_PATHS = ("/partners/api-keys/scopes", "/partner/events")
REFUSED = (401, 403)


class NotRun(Exception):
    pass


def _present(scheme: str, secret: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {secret}"} if scheme == "bearer" else {"X-API-Key": secret}


def get_status(base_url: str, path: str, headers: dict[str, str], *, timeout: float, allow_loopback_http: bool = False) -> int:
    """One GET; returns the status line's code and nothing else. No redirects are followed (a 3xx is returned as it is)."""
    parts = urlsplit(base_url)
    if parts.scheme != "https" and not (allow_loopback_http and parts.scheme == "http" and parts.hostname in ("127.0.0.1", "localhost")):
        raise NotRun("the base URL must be https")
    if not path.startswith("/") or "\n" in path or "\r" in path or " " in path:
        raise NotRun(f"not a path: {path!r}")
    host, port = parts.hostname or "", parts.port
    conn: http.client.HTTPConnection = (
        http.client.HTTPSConnection(host, port, timeout=timeout, context=ssl.create_default_context())
        if parts.scheme == "https" else http.client.HTTPConnection(host, port, timeout=timeout)
    )
    try:
        conn.request("GET", (parts.path.rstrip("/") + path), headers={**headers, "Accept": "application/json"})
        return conn.getresponse().status
    except (OSError, http.client.HTTPException) as error:
        raise NotRun(f"transport failure on {path}: {type(error).__name__}") from error
    finally:
        conn.close()


def probe(base_url: str, secret: str, scheme: str, control_paths: list[str], probe_paths: list[str], *, timeout: float = 10.0, allow_loopback_http: bool = False) -> Json:
    if scheme != "auto" and scheme not in SCHEMES:
        raise ValueError(f"scheme must be auto or one of {SCHEMES}")
    if not probe_paths or not control_paths:
        raise ValueError("at least one control path and one probe path are required")
    attempts: list[Json] = []
    served: tuple[str, str] | None = None
    for candidate in (SCHEMES if scheme == "auto" else (scheme,)):
        for control_path in control_paths:
            status = get_status(base_url, control_path, _present(candidate, secret), timeout=timeout, allow_loopback_http=allow_loopback_http)
            attempts.append({"scheme": candidate, "path": control_path, "status": status})
            if 200 <= status < 300:
                served = (candidate, control_path)
                break
        if served:
            break
    result: Json = {"synthetic": True, "control": {"attempts": attempts, "served": served is not None, "scheme": served[0] if served else None, "path": served[1] if served else None},
                    "probes": [], "verdict": ""}
    if served is None:
        # No control read was served: the credential is not being presented the way Tushe wants, or it may make none of these reads. No refusal below would mean anything.
        result["verdict"] = "NOT RUN: no control read was served, so a refusal on an administration route proves nothing"
        return result
    headers = _present(served[0], secret)
    rows: list[Json] = []
    for path in probe_paths:
        status = get_status(base_url, path, headers, timeout=timeout, allow_loopback_http=allow_loopback_http)
        rows.append({"path": path, "status": status, "outcome": "refused" if status in REFUSED else "SERVED" if 200 <= status < 300 else "inconclusive"})
    result["probes"] = rows
    if any(r["outcome"] == "SERVED" for r in rows):
        result["verdict"] = "NOT MET: the runtime credential was served on an administration route"
    elif any(r["outcome"] == "inconclusive" for r in rows):
        result["verdict"] = "NOT RUN: a probe answered neither 2xx nor 401/403, so it is not read as a refusal"
    else:
        result["verdict"] = "MET: the control read was served and every administration route refused the runtime credential"
    return result


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--base-url", required=True, help="https URL of the Tushe API (never kept in the repository)")
    p.add_argument("--credential-file", type=Path, required=True)
    p.add_argument("--scheme", default="auto", choices=("auto", *SCHEMES), help="how the credential is presented; auto tries each against the control read")
    p.add_argument("--control-path", action="append", help="a GET this credential may make; repeat; default: the documented event feed and audit trail")
    p.add_argument("--probe-path", action="append", help="an administration route (GET) that must refuse it; repeat; default: the documented ones")
    p.add_argument("--out", type=Path)
    p.add_argument("--timeout", type=float, default=10.0)
    p.add_argument("--allow-loopback-http", action="store_true", help=argparse.SUPPRESS)  # for the tests only
    a = p.parse_args(argv)
    try:
        secret = a.credential_file.read_text(encoding="utf-8").strip()
        if not secret:
            raise NotRun("the credential file is empty")
        doc = probe(a.base_url, secret, a.scheme, a.control_path or list(CONTROL_PATHS), a.probe_path or list(PROBE_PATHS), timeout=a.timeout, allow_loopback_http=a.allow_loopback_http)
    except OSError:
        print("NOT RUN: the credential file could not be read")
        return 3
    except NotRun as error:
        print(f"NOT RUN: {error}")
        return 3
    except ValueError as error:
        print(f"refused: {error}")
        return 2
    text = json.dumps(doc, indent=2, sort_keys=True)
    if a.out:
        a.out.parent.mkdir(parents=True, exist_ok=True)
        a.out.write_text(text + "\n", encoding="utf-8")
    print(text)
    verdict = str(doc["verdict"])
    return 0 if verdict.startswith("MET") else 3 if verdict.startswith("NOT RUN") else 1


if __name__ == "__main__":
    sys.exit(main())
