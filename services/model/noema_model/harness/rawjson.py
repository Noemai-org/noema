"""A JSON decoder that keeps the text of every number (A649, A612; INV-21).

Python's own `json.loads` turns `9e-1` and `0.9` into the same float, and `NaN` and `Infinity` into floats too, so by the time a reader sees a number the text the provider wrote is gone. A
caller that must judge the TEXT of a number (the probability of a gate: only a plain decimal counts) decodes with `loads_lossless`: every numeric token becomes a `RawNumber` holding exactly the
characters the grammar matched, and the non-finite names Python accepts become `NonNumber`, so they can never be mistaken for numbers. No float is made here. Syntactically invalid JSON, and an object that
repeats a key (the last would silently win), raise `ValueError`, as everywhere else in the harness.
"""

import json
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True, slots=True)
class RawNumber:
    """The characters of one JSON number token, unread: "0.9", "9e-1", "1", "-0.1". Whether it is a probability is the reader's rule, not this type's."""

    text: str


@dataclass(frozen=True, slots=True)
class NonNumber:
    """One of the names Python's JSON reader accepts although JSON has no such number: NaN, Infinity, -Infinity."""

    token: str


def _no_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    keys = [k for k, _ in pairs]
    if len(set(keys)) != len(keys):
        raise ValueError("duplicate key")
    return dict(pairs)


def loads_lossless(text: str) -> Any:
    """The JSON value of `text`, with every number as a `RawNumber` and NaN, Infinity and -Infinity as a `NonNumber`."""
    return json.loads(text, parse_float=RawNumber, parse_int=RawNumber, parse_constant=NonNumber, object_pairs_hook=_no_duplicate_keys)
