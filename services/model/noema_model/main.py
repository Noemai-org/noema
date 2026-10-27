"""The model service entry point: Understanding, Explanation and Voice (SoT §35.6).

This serves only the two health endpoints and holds no product logic (PLT-01). It builds the telemetry and the harness from the environment: BENCH_OUT turns on bench cost files, NOEMA_MODEL_HARNESS_CONFIG names the harness configuration, and /readyz is ready only once the harness is built. No request endpoint exists yet.
"""

import os
from collections.abc import Callable, Mapping

from fastapi import FastAPI, Response, status
from fastapi.responses import JSONResponse

from noema_model.harness import assemble_harness
from noema_model.platform.clock import SystemClock
from noema_model.platform.telemetry import NullTelemetry, OtelTelemetry, Telemetry, cost_files_from_environment

Readiness = Callable[[], tuple[bool, str]]


def build_telemetry(environ: Mapping[str, str]) -> Telemetry:
    """The service's telemetry. When BENCH_OUT is set (the bench override of the compose stack sets it, nothing else does),
    each model call's record is also written to $BENCH_OUT/cost/ (REQ-NFR-007); otherwise no file is written."""
    return cost_files_from_environment(OtelTelemetry(NullTelemetry()), environ)


def create_app(
    check: Readiness | None = None, telemetry: Telemetry | None = None, environ: Mapping[str, str] | None = None
) -> FastAPI:
    """Build the app. /healthz says the process is alive; /readyz says it can serve: by default, that the harness was built
    from its mounted configuration (NOEMA_MODEL_HARNESS_CONFIG), and if not, why. The telemetry and the harness are kept on
    app.state for the request path, which does not exist yet (no endpoint is defined here)."""
    env = os.environ if environ is None else environ
    app = FastAPI(title="noema-model", docs_url=None, redoc_url=None, openapi_url=None)
    app.state.telemetry = telemetry if telemetry is not None else build_telemetry(env)
    assembly = assemble_harness(env, SystemClock(), app.state.telemetry)
    app.state.harness = assembly.harness
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
