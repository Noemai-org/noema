"""mock-case identity probe (ENV-08; REQ-ENV-002, REQ-ENV-008). Standard library only.

Runs on bench_net, holding both mock-case credentials, and prints one JSON object of HTTP status codes.
The matrix expects the bench credential to open the truth endpoint and every runtime identity to be refused.
Credentials are read from files and never printed.
"""
import json
import urllib.error
import urllib.request

TRUTH = "http://mock-case:8091"
CASE = "http://mock-case:8090"


def token(path: str) -> str:
    with open(path) as handle:
        return handle.read().strip()


def status(method: str, url: str, bearer: str | None) -> int:
    request = urllib.request.Request(url, method=method, data=b"{}" if method == "POST" else None)
    request.add_header("Content-Type", "application/json")
    if bearer is not None:
        request.add_header("Authorization", "Bearer " + bearer)
    try:
        return urllib.request.urlopen(request, timeout=5).status
    except urllib.error.HTTPError as err:
        return err.code
    except OSError:
        return 0


runtime = token("/creds/runtime/token")
bench = token("/creds/bench/token")
guess = "guess-" * 8
cases = CASE + "/v1/cases?tenant_id=ten_probe&principal_id=usr_probe"
print(json.dumps({
    "truth_with_bench_credential": status("GET", TRUTH + "/truth/v1/state", bench),
    "truth_with_runtime_credential": status("GET", TRUTH + "/truth/v1/state", runtime),
    "truth_without_credential": status("GET", TRUTH + "/truth/v1/state", None),
    "truth_with_guessed_credential": status("GET", TRUTH + "/truth/v1/state", guess),
    "reset_with_runtime_credential": status("POST", TRUTH + "/control/v1/reset", runtime),
    "fault_plan_with_runtime_credential": status("GET", TRUTH + "/control/v1/fault-plan", runtime),
    "case_api_with_bench_credential": status("GET", cases, bench),
    "case_api_with_runtime_credential": status("GET", cases, runtime),
    "case_api_without_credential": status("GET", cases, None),
    "case_health_is_open": status("GET", CASE + "/healthz", None),
}))
