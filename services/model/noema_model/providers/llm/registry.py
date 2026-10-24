"""The adapter kinds, by name. A provider is chosen by configuration: a kind, an endpoint and a pinned model version.

Nothing here knows of a benchmark or of any particular endpoint, so a hostile test provider is only another
endpoint behind one of these kinds (PERD §19.1, the harness row).
"""

from collections.abc import Callable, Mapping
from types import MappingProxyType

from noema_model.providers.llm.anthropic_messages import AnthropicMessages
from noema_model.providers.llm.base import AdapterSettings, DecisionProvider, LLMProvider, Transport
from noema_model.providers.llm.gemini_generate import GeminiGenerate
from noema_model.providers.llm.openai_chat import OpenAIChat
from noema_model.providers.llm.openai_decisions import OpenAIDecisions

Factory = Callable[[str, AdapterSettings, Transport], LLMProvider]

KINDS: Mapping[str, Factory] = MappingProxyType(
    {
        "anthropic_messages": AnthropicMessages,
        "openai_chat": OpenAIChat,
        "gemini_generate": GeminiGenerate,
    }
)

NativeFactory = Callable[[str, AdapterSettings, Transport], DecisionProvider]

# Decision models (D9): a probability per question, no generated text. Beside the LLM kinds, never instead of them.
NATIVE_KINDS: Mapping[str, NativeFactory] = MappingProxyType({"openai_decisions": OpenAIDecisions})
