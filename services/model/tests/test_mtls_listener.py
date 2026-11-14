"""mTLS on the model service's listener (noema#555; PERD 36.1 and AGENTS section 6: mTLS between the two services, never a shared bearer; Security Reviewer's blocking 1 on noema#544).

Network isolation is not authentication. The listener must refuse a client that presents no certificate, or one signed by another CA, at the TLS handshake, and there must be no plain
listener. The test starts the real service under uvicorn with exactly the TLS settings the Dockerfile sets (it reads them from the Dockerfile, so removing them from the image makes the test
fail), with certificates made by deploy/compose/tls_local.sh, and talks to it over real sockets. Needs openssl and sh; it skips, loudly, where they are missing. SYNTHETIC material (AGENT-25).
"""

from __future__ import annotations

import http.client
import os
import re
import shutil
import socket
import ssl
import subprocess
import sys
import time
from collections.abc import Iterator
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[3]
SERVICE = REPO / "services" / "model"
SCRIPT = REPO / "deploy" / "compose" / "tls_local.sh"
DOCKERFILE = SERVICE / "Dockerfile"

pytestmark = pytest.mark.skipif(shutil.which("openssl") is None or shutil.which("sh") is None, reason="openssl and sh are needed to make the test certificates")


def dockerfile_env() -> dict[str, str]:
    """The `ENV KEY=value` pairs of the Dockerfile that start with UVICORN_ (one ENV instruction may carry several pairs)."""
    out: dict[str, str] = {}
    for line in DOCKERFILE.read_text().splitlines():
        if line.startswith("ENV "):
            for key, value in re.findall(r"([A-Z][A-Z0-9_]*)=(\S+)", line):
                if key.startswith("UVICORN_"):
                    out[key] = value
    return out


def make_certs(directory: Path) -> Path:
    subprocess.run(["sh", str(SCRIPT), str(directory)], check=True, capture_output=True)
    return directory


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


@pytest.fixture(scope="module")
def certs(tmp_path_factory: pytest.TempPathFactory) -> Path:
    return make_certs(tmp_path_factory.mktemp("tls"))


@pytest.fixture(scope="module")
def listener(certs: Path, tmp_path_factory: pytest.TempPathFactory) -> Iterator[int]:
    """The model service as the Dockerfile runs it: the Dockerfile's UVICORN_* settings, its certificate paths pointed at the test certificates."""
    env_settings = dockerfile_env()
    mapped = {
        "UVICORN_SSL_CERTFILE": certs / "model.crt",
        "UVICORN_SSL_KEYFILE": certs / "model.key",
        "UVICORN_SSL_CA_CERTS": certs / "ca.crt",
    }
    env = {**os.environ, **env_settings}
    for key, path in mapped.items():
        if key in env_settings:
            env[key] = str(path)  # the image's value is a path inside the container; here it is the same file on this machine
    port = free_port()
    proc = subprocess.Popen(
        [sys.executable, "-m", "uvicorn", "noema_model.main:app", "--host", "127.0.0.1", "--port", str(port)],
        cwd=SERVICE, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
    )
    deadline = time.time() + 30
    while time.time() < deadline:
        if proc.poll() is not None:
            raise RuntimeError("the model service did not start:\n" + (proc.stdout.read().decode() if proc.stdout else ""))
        with socket.socket() as s:
            s.settimeout(0.3)
            if s.connect_ex(("127.0.0.1", port)) == 0:
                break
        time.sleep(0.2)
    else:
        proc.kill()
        raise RuntimeError("the model service did not listen in 30 seconds")
    try:
        yield port
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()


def tls_context(ca: Path, client: tuple[Path, Path] | None) -> ssl.SSLContext:
    ctx = ssl.create_default_context(cafile=str(ca))
    if client is not None:
        ctx.load_cert_chain(str(client[0]), str(client[1]))
    return ctx


def get_healthz(port: int, ctx: ssl.SSLContext) -> int:
    conn = http.client.HTTPSConnection("127.0.0.1", port, context=ctx, timeout=5)
    try:
        conn.request("GET", "/healthz")
        return conn.getresponse().status
    finally:
        conn.close()


def test_the_image_sets_the_tls_options_that_require_a_client_certificate() -> None:
    env = dockerfile_env()
    assert env.get("UVICORN_SSL_CERT_REQS") == "2", "CERT_REQUIRED (2) must be set: a client without a certificate must be refused"
    for key in ("UVICORN_SSL_CERTFILE", "UVICORN_SSL_KEYFILE", "UVICORN_SSL_CA_CERTS"):
        assert env.get(key, "").startswith("/run/tls/"), f"{key} must name a file under /run/tls (file-mounted, never in the image)"
    text = DOCKERFILE.read_text()
    assert "--ssl-cert-reqs" not in text or "UVICORN_SSL_CERT_REQS" in text
    assert re.search(r"(?m)^COPY .*\.(crt|key|pem)", text) is None, "no certificate or key may be copied into the image"


def test_a_client_with_the_cores_certificate_reaches_healthz(listener: int, certs: Path) -> None:
    assert get_healthz(listener, tls_context(certs / "ca.crt", (certs / "core-client.crt", certs / "core-client.key"))) == 200


def test_the_health_checks_own_certificate_also_works(listener: int, certs: Path) -> None:
    assert get_healthz(listener, tls_context(certs / "ca.crt", (certs / "model-health.crt", certs / "model-health.key"))) == 200


def test_a_client_without_a_certificate_is_refused_at_the_handshake(listener: int, certs: Path) -> None:
    ctx = tls_context(certs / "ca.crt", None)
    with pytest.raises((ssl.SSLError, ConnectionError, OSError)):
        # TLS 1.3 reports a refused client certificate on the first read, so the request is made, not just the connect.
        get_healthz(listener, ctx)


def test_a_client_certificate_from_another_ca_is_refused(listener: int, certs: Path, tmp_path: Path) -> None:
    other = make_certs(tmp_path / "other")
    ctx = tls_context(certs / "ca.crt", (other / "core-client.crt", other / "core-client.key"))
    with pytest.raises((ssl.SSLError, ConnectionError, OSError)):
        get_healthz(listener, ctx)


def test_there_is_no_plain_http_listener(listener: int) -> None:
    conn = http.client.HTTPConnection("127.0.0.1", listener, timeout=5)
    try:
        with pytest.raises((http.client.HTTPException, ConnectionError, OSError)):
            conn.request("GET", "/healthz")
            conn.getresponse().read()
    finally:
        conn.close()
