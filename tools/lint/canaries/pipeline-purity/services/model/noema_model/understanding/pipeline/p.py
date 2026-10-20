"""Canary: the pipeline keeps module-level state and talks to the network."""
import httpx

CACHE: dict[str, str] = {}


def step(key: str) -> str:
    return CACHE.get(key) or httpx.get(key).text
