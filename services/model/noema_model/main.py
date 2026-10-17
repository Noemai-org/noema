"""The model service entry point: Understanding, Explanation and Voice (SoT §35.6).

This skeleton serves only the two health endpoints and holds no product logic (PLT-01).
"""

from collections.abc import Callable

from fastapi import FastAPI, Response, status
from fastapi.responses import JSONResponse

Readiness = Callable[[], tuple[bool, str]]


def not_ready() -> tuple[bool, str]:
    """The skeleton has no dependencies wired yet, so it is never ready."""
    return False, "dependencies are not configured"


def create_app(check: Readiness = not_ready) -> FastAPI:
    """Build the app. /healthz says the process is alive; /readyz says it can serve."""
    app = FastAPI(title="noema-model", docs_url=None, redoc_url=None, openapi_url=None)

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
