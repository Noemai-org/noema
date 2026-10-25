"""What a run needs from outside, as small interfaces, so a missing piece is a named gap and not a silent skip."""
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Protocol

from benchmark.oracle_client.client import OracleClient
from benchmark.oracle_client.transport import read_secret

from .api import PublicApi
from .control import MockCaseControl
from .cost import CostFiles

Json = dict[str, Any]


class EnvironmentUnavailable(Exception):
    """A part of the environment this run needs is not there. `component` says which."""

    def __init__(self, component: str, reason: str) -> None:
        super().__init__(f"{component}: {reason}")
        self.component, self.reason = component, reason


class FixtureLoader(Protocol):
    def load(self, fixture: Json) -> None: ...


class SessionProvider(Protocol):
    def token_for(self, principal_id: str, roles: list[str]) -> str: ...


class UnavailableFixtureLoader:
    """The fixture loader puts a case's known facts into the synthetic enterprise (benchmark-fixture.v1.json). How a field maps to a Tushe call is the
    adapter task's; Tushe's sandbox is not reachable from this build, so there is no loader yet."""

    component = "fixture loader"

    def __init__(self, reason: str) -> None:
        self.unavailable_reason = reason

    def load(self, fixture: Json) -> None:
        raise EnvironmentUnavailable(self.component, self.unavailable_reason)


class DirSessionProvider:
    """Session tokens issued by the enterprise IdP, one file per principal in a directory (a file named for the principal id holds its token).
    The synthetic IdP is not built; until it is, tokens come from here. Never an argument or an environment value."""

    def __init__(self, directory: Path) -> None:
        self.directory = Path(directory)

    @property
    def unavailable_reason(self) -> str | None:
        return None if self.directory.is_dir() else f"the session token directory {self.directory} does not exist"

    def token_for(self, principal_id: str, roles: list[str]) -> str:
        path = self.directory / principal_id
        if not path.is_file():
            raise EnvironmentUnavailable("session provider", f"no session token for {principal_id}")
        return read_secret(path)


@dataclass
class RunEnv:
    api: PublicApi
    control: MockCaseControl
    oracle: OracleClient
    fixtures: FixtureLoader
    sessions: SessionProvider
    run_id: str
    admin_token: str | None = None
    tenant_id: str | None = None  # the tenant the admin API addresses (kill switch); no default is assumed
    settle_timeout_s: float = 30.0
    poll_s: float = 0.2
    sleep: Callable[[float], None] = time.sleep
    now: Callable[[], float] = time.monotonic
    costs: CostFiles = field(default_factory=lambda: CostFiles(None))  # the model service's bench cost files; none configured means cost is not available
