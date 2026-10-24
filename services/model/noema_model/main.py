"""The model service entry point: Understanding, Explanation and Voice (SoT §35.6).

This skeleton serves only the two health endpoints and holds no product logic (PLT-01). It builds the telemetry from the environment (BENCH_OUT turns on bench cost files).
"""

import os
from collections.abc import Callable, Mapping

from fastapi import FastAPI, Response, status
from fastapi.responses import JSONResponse

from noema_model.platform.telemetry import NullTelemetry, OtelTelemetry, Telemetry, cost_files_from_environment

Readiness = Callable[[], tuple[bool, str]]


def not_ready() -> tuple[bool, str]:
    """The skeleton has no dependencies wired yet, so it is never ready."""
    return False, "dependencies are not configured"


def build_telemetry(environ: Mapping[str, str]) -> Telemetry:
    """The service's telemetry. When BENCH_OUT is set (the bench override of the compose stack sets it, nothing else does),
    each model call's record is also written to $BENCH_OUT/cost/ (REQ-NFR-007); otherwise no file is written."""
    return cost_files_from_environment(OtelTelemetry(NullTelemetry()), environ)


def create_app(check: Readiness = not_ready, telemetry: Telemetry | None = None) -> FastAPI:
    """Build the app. /healthz says the process is alive; /readyz says it can serve. The telemetry is kept on
    app.state for the harness, which this skeleton does not construct yet."""
    app = FastAPI(title="noema-model", docs_url=None, redoc_url=None, openapi_url=None)
    app.state.telemetry = telemetry if telemetry is not None else build_telemetry(os.environ)

    @app.get("/healthz")
    def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/readyz")
    def readyz(response: Response) -> Response:
        ready, reason = check()
        if ready:
            return JSONResponse({"status": "ready"}, status_code=status.HTTP_200_OK)
        return JSONResponse(
            {"status": "not_ready", "reason": reason}, status_code=status.HTTP_503_SERVICE_UNAVAILABLE
        )

    return app


app = create_app()
