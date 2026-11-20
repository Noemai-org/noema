"""The record-only pass of the compile acceptance run (UND-16; A361, A372, the Architect's answer 5, relayed): the five G1 inputs of BO's `benchmark/replay/g1_inputs.json`, taken through the same compile
pipeline after the golden examples and recorded by the same recorder, so UND-15 can later make replay fixtures of them. Inputs only: no expectation is compared or invented (INV-22).

STUB (red commit): nothing is implemented yet."""
from dataclasses import dataclass
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_PATH = ROOT / "benchmark" / "replay" / "g1_inputs.json"


class RecordOnlyRefused(Exception):
    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


@dataclass(frozen=True, slots=True)
class RecordOnlyInput:
    case_id: str
    channel: str
    locale_mix: tuple[str, ...]
    text: str


def load_inputs(path: Path) -> tuple[RecordOnlyInput, ...]:
    raise NotImplementedError


def canonical_input(inp: RecordOnlyInput) -> dict[str, Any]:
    raise NotImplementedError
