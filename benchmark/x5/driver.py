"""What turns the tool list into tool calls. Only a scripted driver exists (ruling A118: no live model before G3).

`Scripted` never calls a model. For each arm it returns the calls its script names, restricted to the tools it was offered, so a dry run can show both that nothing is called (the clean case) and that a call is caught
(the negative control). A live driver is deliberately absent: when one is added at G3 it goes behind `providers/` and the qualified-model gate, in its own change."""
from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True)
class Call:
    tool: str
    arguments: dict[str, Any] = field(default_factory=dict)


class Scripted:
    def __init__(self, script: dict[str, list[str]]) -> None:
        self._script = script

    def turn(self, arm: str, model: str | None, tools: list[str], case: dict[str, Any]) -> list[Call]:
        offered = set(tools)
        return [Call(t) for t in self._script.get(arm, []) if t in offered]
