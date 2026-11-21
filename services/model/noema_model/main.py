"""The model service entry point: Understanding, Explanation and Voice (SoT §35.6).

This serves only the two health endpoints and holds no product logic (PLT-01). It builds the telemetry and the harness: NOEMA_MODEL_HARNESS_CONFIG names the harness configuration, and /readyz is ready only once the harness is built. The service reads no benchmark variable: the bench stack hands in its own cost-file telemetry from outside (benchmark/model_cost, Architect's ruling A55). The compile route is mounted (UND-05); the service behind it is built only when NOEMA_MODEL_COMPILE_CONFIG names its configuration and the listener requires a client certificate (understanding/compiler/wiring.py).
"""

import os
from collections.abc import Callable, Mapping

from fastapi import FastAPI, Response, status
from fastapi.responses import JSONResponse

from noema_model.harness import assemble_harness
from noema_model.platform.clock import SystemClock
from noema_model.platform.telemetry import NullTelemetry, OtelTelemetry, Telemetry
from noema_model.evidence.transport import EvidenceTransport
from noema_model.understanding.compiler import CompileService
from noema_model.understanding.compiler.wiring import build_compile_service, compile_requested
from noema_model.understanding.compiler.route import register as register_compile_route

Readiness = Callable[[], tuple[bool, str]]


def build_telemetry() -> Telemetry:
    """The service's own telemetry. It reads no environment variable and writes no file; a bench run hands in different telemetry through
    create_app(telemetry=...) from benchmark/model_cost (REQ-NFR-007)."""
    return OtelTelemetry(NullTelemetry())


def create_app(
    check: Readiness | None = None,
    telemetry: Telemetry | None = None,
    environ: Mapping[str, str] | None = None,
    compile_service: CompileService | None = None,
    evidence_transport: EvidenceTransport | None = None,
) -> FastAPI:
    """Build the app. /healthz says the process is alive; /readyz says it can serve: by default, that the harness was built
    from its mounted configuration (NOEMA_MODEL_HARNESS_CONFIG), and if not, why. The telemetry and the harness are kept on
    app.state for the request path, which does not exist yet (no endpoint is defined here)."""
    env = os.environ if environ is None else environ
    app = FastAPI(title="noema-model", docs_url=None, redoc_url=None, openapi_url=None)
    app.state.telemetry = telemetry if telemetry is not None else build_telemetry()
    assembly = assemble_harness(env, SystemClock(), app.state.telemetry)
    app.state.harness = assembly.harness
    if compile_service is None and compile_requested(env):
        # Named by NOEMA_MODEL_COMPILE_CONFIG: built from that file, and only on a listener that requires a client certificate; any problem refuses to start (UND-05, noema#555).
        compile_service = build_compile_service(env, assembly.harness, transport=evidence_transport)
    app.state.compile_service = compile_service  # None unless named: the compile route then answers 503 (UND-05)
    register_compile_route(app)
    readiness: Readiness = check if check is not None else (lambda: (assembly.harness is not None, assembly.reason))

    @app.get("/healthz")
    def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/readyz")
    def readyz(response: Response) -> Response:
        ready, reason = readiness()
        if ready:
            return JSONResponse({"status": "ready"}, status_code=status.HTTP_200_OK)
        return JSONResponse(
            {"status": "not_ready", "reason": reason}, status_code=status.HTTP_503_SERVICE_UNAVAILABLE
        )

    return app


app = create_app()
