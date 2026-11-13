"""What a run needs from outside, as small interfaces, so a missing piece is a named gap and not a silent skip."""
import os
import re
import subprocess
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


_PRINCIPAL = re.compile(r"^[A-Za-z0-9_][A-Za-z0-9_.:@-]*$")
_ROLE = re.compile(r"^[a-z_]+$")
# The roles the issuer accepts, exactly one per token (ruling A1; ruling A152). Mirrors the `Role` constants of `services/core/edge/session/session.go`; a test reads that file and fails if the two differ.
ISSUER_ROLES = frozenset({"customer", "ops", "approver", "audit"})


class IdpSessionProvider:
    """Session tokens from the synthetic IdP (`services/core/cmd/synthetic-idp`, REQ-SEC-001). A token lives 15 minutes, so it is issued on demand
    and reused until `refresh_margin_s` before it expires; one token per principal and role set. The issuer runs with an empty environment and
    its output is never put in an error (a token is a secret). A principal or role that could be read as a flag is refused, not passed on."""

    component = "session provider"

    def __init__(self, binary: Path, key_file: Path, *, kid: str, audience: str, tenant_id: str, ttl_s: int = 600, refresh_margin_s: int = 60,
                 timeout_s: float = 10.0, now: Callable[[], float] = time.monotonic) -> None:
        self.binary, self.key_file = Path(binary), Path(key_file)
        self.kid, self.audience, self.tenant_id = kid, audience, tenant_id
        self.ttl_s, self.refresh_margin_s, self.timeout_s, self._now = ttl_s, refresh_margin_s, timeout_s, now
        self._cache: dict[tuple[str, tuple[str, ...]], tuple[str, float]] = {}

    @property
    def unavailable_reason(self) -> str | None:
        if not self.binary.is_file():
            return f"the synthetic IdP issuer {self.binary} does not exist"
        if not self.key_file.is_file():
            return f"the IdP key file {self.key_file} does not exist"
        return None

    def token_for(self, principal_id: str, roles: list[str]) -> str:
        if not _PRINCIPAL.match(principal_id) or not roles or not all(_ROLE.match(r) for r in roles):
            raise EnvironmentUnavailable(self.component, "a principal or role is not in the form the issuer accepts")
        unknown = sorted(set(roles) - ISSUER_ROLES)
        if unknown:
            raise EnvironmentUnavailable(self.component, f"role {', '.join(unknown)} is not one the issuer accepts ({', '.join(sorted(ISSUER_ROLES))}; ruling A152): refused before the issuer is called")
        if len(set(roles)) != 1:
            raise EnvironmentUnavailable(self.component, "a session carries exactly one role (ruling A1): refused before the issuer is called")
        key = (principal_id, tuple(sorted(set(roles))))
        cached = self._cache.get(key)
        if cached and self._now() < cached[1] - self.refresh_margin_s:
            return cached[0]
        if (reason := self.unavailable_reason) is not None:
            raise EnvironmentUnavailable(self.component, reason)
        argv = [str(self.binary), "issue", f"-key={self.key_file}", f"-kid={self.kid}", f"-aud={self.audience}", f"-tenant={self.tenant_id}",
                f"-principal={principal_id}", *[f"-role={r}" for r in key[1]], f"-ttl={self.ttl_s}s"]
        try:
            done = subprocess.run(argv, capture_output=True, text=True, timeout=self.timeout_s, env={"PATH": os.defpath}, check=False)
        except subprocess.TimeoutExpired:
            raise EnvironmentUnavailable(self.component, f"the issuer timed out after {self.timeout_s} s") from None
        except OSError as error:
            raise EnvironmentUnavailable(self.component, f"the issuer could not run: {error.__class__.__name__}") from None
        token = done.stdout.strip()
        if done.returncode != 0 or not token:
            raise EnvironmentUnavailable(self.component, f"the issuer failed (exit {done.returncode}) for {principal_id}")
        self._cache[key] = (token, self._now() + self.ttl_s)
        return token


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
