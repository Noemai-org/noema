"""mock-case's truth endpoint (REQ-ENV-002): read-only, bench credential, bench_net only. Shapes: synthetic/mock_case/internal/api/openapi-bench.json."""
from typing import Any

from .transport import JsonClient

Json = dict[str, Any]
MAX_PAGE = 1000  # mock-case's own limit on /truth/v1/commands


class MockCaseTruth:
    def __init__(self, base_url: str, credential: str, *, timeout: float = 10.0, page_size: int = 200) -> None:
        if not 1 <= page_size <= MAX_PAGE:
            raise ValueError(f"page_size must be 1 to {MAX_PAGE}")
        self._http = JsonClient(base_url, token=credential, timeout=timeout)
        self._page = page_size

    def state(self) -> Json:
        return self._http.request("GET", "/truth/v1/state")[0]

    def digest(self) -> Json:
        return self._http.request("GET", "/truth/v1/digest")[0]

    def commands(self, after_seq: int = 0, faults_only: bool = False) -> list[Json]:
        """Every command after `after_seq`, in order, across pages."""
        rows: list[Json] = []
        while True:
            body = self._http.request("GET", f"/truth/v1/commands?after_seq={after_seq}&limit={self._page}&faults_only={str(faults_only).lower()}")[0]
            page = body["commands"]
            rows += page
            if body.get("complete", True) or not page:
                return rows
            after_seq = page[-1]["seq"]
