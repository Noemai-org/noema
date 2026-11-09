"""Canary: voice may import providers.speech (A9) but not the LLM providers."""
from noema_model.providers.llm import LLM  # noqa: F401
