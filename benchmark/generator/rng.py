"""Deterministic choices from a seed. SHA-256 of "seed:label", not `random`, so no Python release can change a suite."""
import hashlib
from typing import Sequence, TypeVar

T = TypeVar("T")


def digest(seed: int, label: str) -> int:
    return int.from_bytes(hashlib.sha256(f"{seed}:{label}".encode()).digest()[:8], "big")


def below(seed: int, label: str, n: int) -> int:
    if n < 1:
        raise ValueError("below(n) needs n >= 1")
    return digest(seed, label) % n


def pick(seed: int, label: str, items: Sequence[T]) -> T:
    return items[below(seed, label, len(items))]


def pick_distinct(seed: int, label: str, items: Sequence[T], count: int) -> list[T]:
    """`count` distinct items, in a seed-dependent order. Raises if there are fewer than `count`."""
    if count > len(items):
        raise ValueError(f"cannot pick {count} distinct from {len(items)}")
    ordered = sorted(range(len(items)), key=lambda i: digest(seed, f"{label}:{i}"))
    return [items[i] for i in ordered[:count]]
