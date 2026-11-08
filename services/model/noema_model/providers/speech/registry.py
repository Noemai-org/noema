"""The speech adapter kinds, by name. A provider is chosen by configuration, never by an import (BD-16, REQ-VOI-002).

The capability matrix is the only source of what a provider can do: a kind with no matrix row cannot be built, and a row
with no kind is a configuration error. Both directions are checked, so a vendor cannot be half added.
"""

from collections.abc import Callable, Mapping
from types import MappingProxyType

from noema_model.platform.config import ConfigError
from noema_model.providers.llm.base import AdapterSettings
from noema_model.providers.speech.base import ProviderCapabilities, SpeechProvider
from noema_model.providers.speech.matrix import load_matrix
from noema_model.providers.speech.scaffolds import AethexAI, Indigenius, NAtlas

Factory = Callable[[str, AdapterSettings, ProviderCapabilities], SpeechProvider]

SPEECH_KINDS: Mapping[str, Factory] = MappingProxyType(
    {
        "aethex_ai": AethexAI,
        "indigenius": Indigenius,
        "n_atlas": NAtlas,
    }
)


def build_speech_provider(
    kind: str, name: str, settings: AdapterSettings, matrix: Mapping[str, ProviderCapabilities] | None = None
) -> SpeechProvider:
    rows = load_matrix() if matrix is None else matrix
    if set(rows) != set(SPEECH_KINDS):
        raise ConfigError("speech matrix and speech kinds list different providers")
    if kind not in SPEECH_KINDS:
        raise ConfigError("speech kind: unknown")
    return SPEECH_KINDS[kind](name, settings, rows[kind])
