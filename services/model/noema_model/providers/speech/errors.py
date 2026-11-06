"""The provider error types, re-exported for the voice pipeline. The lint lets voice/ import providers.speech and nothing
else in providers (A9), and a speech stream fails with the same ProviderError the text providers raise, so failover treats
both alike."""

from noema_model.providers.llm.base import ProviderError as ProviderError
from noema_model.providers.llm.base import ProviderFailure as ProviderFailure
