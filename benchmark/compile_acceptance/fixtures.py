"""UND-15 (REQ-INT-002; REQ-TST-004, AGENT-12; A261; SR's conditions on noema#778): the reduction of a live run's recordings to committed replay fixtures. RED STUB: it answers nothing yet."""
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from benchmark.compile_acceptance.replay import RecordingSet


def reduce_reply(reply: Mapping[str, Any]) -> dict[str, Any]:
    raise NotImplementedError("UND-15: red stub")


def reduce_run(src: Path, dst: Path, *, head_sha: str) -> None:
    raise NotImplementedError("UND-15: red stub")


def load_fixtures(directory: Path) -> RecordingSet:
    raise NotImplementedError("UND-15: red stub")
