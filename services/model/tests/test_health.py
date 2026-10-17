"""Health endpoints of the model service (PLT-01; REQ-OPS-001)."""

from fastapi.testclient import TestClient

from noema_model.main import create_app


def test_healthz_is_200() -> None:
    client = TestClient(create_app())
    response = client.get("/healthz")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_readyz_is_503_until_dependencies_exist() -> None:
    client = TestClient(create_app())
    response = client.get("/readyz")
    assert response.status_code == 503
    assert response.json()["status"] == "not_ready"


def test_readyz_is_200_when_ready() -> None:
    client = TestClient(create_app(lambda: (True, "")))
    response = client.get("/readyz")
    assert response.status_code == 200
    assert response.json() == {"status": "ready"}


def test_no_docs_or_openapi_endpoint_is_exposed() -> None:
    client = TestClient(create_app())
    assert client.get("/docs").status_code == 404
    assert client.get("/openapi.json").status_code == 404
