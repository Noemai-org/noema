"""The modular voice pipeline guard (VOI-18, REQ-VOI-006): transcribe, reason, synthesise.

A speech-to-speech model reasons and speaks in one step. The transcript, and the per-token confidences that REQ-AMB-007
needs to force a clarification on a key slot, are then not ours to see. So the `bench` and `demo` profiles refuse any
configured provider whose mode is SPEECH_TO_SPEECH, and refuse a profile they do not know, because an unknown profile
cannot be shown to be safe. `dev` and `ci` may hold one, so that it can be benchmarked before any profile accepts it.
"""

from collections.abc import Iterable

from noema_model.platform.config import ConfigError
from noema_model.providers.speech.base import PipelineMode, ProviderCapabilities

MODULAR_ONLY_PROFILES = frozenset({"bench", "demo"})
OPEN_PROFILES = frozenset({"dev", "ci"})  # PERD §65.1 lists exactly these four profiles


def check_modular_pipeline(profile: str, providers: Iterable[ProviderCapabilities]) -> None:
    """Raise ConfigError, so the service does not start, when the profile cannot hold the configured providers."""
    if profile not in MODULAR_ONLY_PROFILES | OPEN_PROFILES:
        raise ConfigError("voice profile: unknown")
    if profile in OPEN_PROFILES:
        return
    refused = sorted(caps.name for caps in providers if caps.mode is not PipelineMode.TRANSCRIBE_ONLY)
    if refused:
        raise ConfigError(f"voice profile {profile}: speech-to-speech provider not allowed: {', '.join(refused)}")
