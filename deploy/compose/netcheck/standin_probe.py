"""Stand-in identity probe (ENV-05; REQ-ENV-002, INV-12). Standard library only.

Runs holding both stand-in credentials (the bench token and the runtime token) and prints one JSON object of HTTP status codes. The matrix expects
the bench token to open the control side and nothing else, and the runtime token to read nothing without an identity assertion. Nothing is loaded:
the stand-in must answer "not loaded" (503), never an empty answer. Credentials are read from files and never printed.
"""
import json
import urllib.error
import urllib.request

CONTROL = "http://standin:8094"
DATA = "http://standin:8093"


def token(path: str) -> str:
    with open(path) as handle:
        return handle.read().strip()


def status(url: str, bearer: str | None) -> int:
    request = urllib.request.Request(url)
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
print(json.dumps({
    "control_with_bench_credential": status(CONTROL + "/control/v1/standin", bench),
    "control_with_runtime_credential": status(CONTROL + "/control/v1/standin", runtime),
    "control_without_credential": status(CONTROL + "/control/v1/standin", None),
    "control_with_guessed_credential": status(CONTROL + "/control/v1/standin", guess),
    "data_with_bench_credential": status(DATA + "/standin/v1/principal", bench),
    "data_without_credential": status(DATA + "/standin/v1/principal", None),
    "data_with_runtime_credential_nothing_loaded": status(DATA + "/standin/v1/principal", runtime),
}))
