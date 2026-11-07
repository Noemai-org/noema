"""The fixture loader for the Noema-side stand-in (DEC-2026-10-26 D2: the fixture-loader client lives in `benchmark/fixtures/`; benchmark-only, never reachable from the core).

It derives the stand-in's payloads from the fixture (`standin.derive`) and PUTs them to the stand-in's control route, `PUT /control/v1/standin`, with the bench credential (read from a
file, never an argument or an environment value). The route is proposed here and built by the Integration Lead; this client is the benchmark's half. A refusal or no answer is an
unavailable loader, never a silent pass. A fixture that already holds cases is refused until mock-case has a seed route. Generated layer only (INV-22)."""
from pathlib import Path
from typing import Any

from benchmark.fixtures.standin import derive
from benchmark.oracle_client.transport import HttpError, JsonClient, TransportError, read_secret
from benchmark.runner.env import EnvironmentUnavailable

Json = dict[str, Any]
ROUTE = "/control/v1/standin"


class StandInFixtureLoader:
    component = "fixture loader"
    unavailable_reason = None

    def __init__(self, base_url: str, credential_file: Path, *, timeout: float = 10.0) -> None:
        self._client = JsonClient(base_url, token=read_secret(credential_file), timeout=timeout)

    def load(self, fixture: Json) -> None:
        payload = derive(fixture)
        if payload["existing_cases"]:
            raise EnvironmentUnavailable(self.component, "the fixture holds existing cases and mock-case has no seed route yet")
        try:
            self._client.request("PUT", ROUTE, payload)
        except HttpError as error:
            raise EnvironmentUnavailable(self.component, f"the stand-in refused the fixture: {error}") from None
        except TransportError as error:
            raise EnvironmentUnavailable(self.component, f"the stand-in did not answer: {error}") from None
