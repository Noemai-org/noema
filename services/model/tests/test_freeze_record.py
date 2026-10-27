"""The prompt template named by the model-selection freeze is the one the harness holds (benchmark/decisions/2026-10-27-model-selection.md)."""

import json
import re
from pathlib import Path

from noema_model.harness.templates import DECIDE_V2

RECORD = Path(__file__).resolve().parents[3] / "benchmark" / "decisions" / "2026-10-27-model-selection.md"


def test_the_recorded_template_is_decide_v2_with_its_current_digest() -> None:
    block = re.search(r"```json frozen\n(.*?)\n```", RECORD.read_text(encoding="utf-8"), re.S)
    assert block is not None
    template = json.loads(block.group(1))["prompt_template"]
    assert (template["version"], template["digest"]) == (DECIDE_V2.version, DECIDE_V2.digest)
