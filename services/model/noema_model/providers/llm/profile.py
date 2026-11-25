"""Which transport the provider adapters stand on (UND-15; A635, A619, A679; REQ-INT-002, REQ-TST-004; INV-29, AGENT-12, AGENT-16).

Two settings, one meaning each. `NOEMA_MODEL_PROVIDER_PROFILE` (A679) names the provider profile: exactly `live` or `replay`. Absent refuses
the start, and so does any other value (empty, padded, upper-case, unlisted); nothing defaults to either. `NOEMA_PROFILE` stays the
environment profile (the closed set `dev`, `ci`, `bench`, `demo`, `test`, the name the evil provider already requires); if it is set it
must be one of those, and it is what lets the demo profile be identified.

`live` builds the live transport (it refuses a fixtures directory that is also set: a contradictory configuration). `replay` serves a
recorded, SYNTHETIC set from `NOEMA_MODEL_REPLAY_FIXTURES_DIR` and only in the `bench` and `test` environment profiles: the demo profile
refuses it (A679), so do `dev` and `ci` (the earlier rule, which A679 does not change), and a replay with no environment profile is refused
(nothing infers one). Fixtures that cannot be read refuse the start and never fall back to a live transport.

A refusal is a `ProfileRefused` (a `ConfigError`): its text is a closed code and never a value from the environment, a path or a file.
The module reads only the mapping it is handed (a test reads its imports), imports no network, process, clock or random module, and takes
the live transport as a factory it calls only on the live path, so a replay never constructs one.
"""

from collections.abc import Callable, Mapping
from dataclasses import dataclass
from pathlib import Path

from noema_model.platform.config import ConfigError
from noema_model.providers.llm.base import Transport
from noema_model.providers.llm.replay import ReplayFixturesRefused, ReplayTransport

ENV_PROFILE = "NOEMA_PROFILE"  # the name the evil provider already requires (benchmark/evil_provider); it must be `bench` there
ENV_PROVIDER_PROFILE = "NOEMA_MODEL_PROVIDER_PROFILE"  # A679: exactly `live` or `replay`; absent refuses the start
ENV_REPLAY_FIXTURES = "NOEMA_MODEL_REPLAY_FIXTURES_DIR"  # the recorder's output directory (`fixtures reduce`); A679 does not rename it

PROFILES = frozenset({"dev", "ci", "bench", "demo", "test"})
PROVIDER_PROFILES = frozenset({"live", "replay"})
REPLAY_PROFILES = frozenset({"bench", "test"})
REPLAY_EVIDENCE_LABEL = "Understanding: recorded replay of live output"  # the row's Done-when: the evidence says it is a replay


class ProfileRefused(ConfigError):
    """The profile or the replay setting is refused. `code` is a closed word; the text carries it and nothing else."""

    def __init__(self, code: str) -> None:
        super().__init__(f"profile refused: {code}")
        self.code = code


@dataclass(frozen=True, slots=True)
class Selection:
    transport: Transport
    replay: bool
    profile: str | None
    label: str | None  # REPLAY_EVIDENCE_LABEL for a replay, None for the live transport


def select_transport(environ: Mapping[str, str], live: Callable[[], Transport]) -> Selection:
    """The transport for this configuration. `live` builds the live transport and is called only on the `live` path."""
    profile: str | None = None
    if ENV_PROFILE in environ:
        profile = environ[ENV_PROFILE]
        if profile not in PROFILES:  # an empty, padded, upper-case or unlisted value alike
            raise ProfileRefused("UNKNOWN_PROFILE")
    if ENV_PROVIDER_PROFILE not in environ:
        raise ProfileRefused("PROVIDER_PROFILE_REQUIRED")
    provider = environ[ENV_PROVIDER_PROFILE]
    if provider not in PROVIDER_PROFILES:
        raise ProfileRefused("UNKNOWN_PROVIDER_PROFILE")
    if provider == "live":
        if ENV_REPLAY_FIXTURES in environ:
            raise ProfileRefused("LIVE_REFUSES_REPLAY_FIXTURES")
        return Selection(live(), False, profile, None)
    if profile is None:
        raise ProfileRefused("PROFILE_REQUIRED_FOR_REPLAY")
    if profile == "demo":
        raise ProfileRefused("DEMO_REFUSES_REPLAY")
    if profile not in REPLAY_PROFILES:
        raise ProfileRefused("REPLAY_ONLY_IN_BENCH_AND_TEST")
    if ENV_REPLAY_FIXTURES not in environ:
        raise ProfileRefused("REPLAY_FIXTURES_REQUIRED")
    try:
        transport = ReplayTransport(_directory(environ[ENV_REPLAY_FIXTURES]))
    except ReplayFixturesRefused as exc:  # never a live transport instead
        raise ProfileRefused("REPLAY_FIXTURES_REFUSED") from exc
    return Selection(transport, True, profile, REPLAY_EVIDENCE_LABEL)


def _directory(value: str) -> Path:
    if not value:  # a set but empty setting is a refusal, not a request for the live transport
        raise ReplayFixturesRefused("NOT_A_DIRECTORY", "directory")
    return Path(value)
