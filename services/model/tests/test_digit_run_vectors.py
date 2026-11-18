"""A259, A279/A270 (REQ-SEC-003, AGENT-13): the redactor gives the verdict of the one shared vector file, `contracts/fixtures/digit_run_vectors.json`, which the Record guard (the source of
truth for the digit run) and the UI scan read as well. A string is refused when the redactor changes it (a run of more than four digits is masked). All data is SYNTHETIC."""

import json
from pathlib import Path
from typing import Any

import pytest

from noema_model.platform.masking import MaskingLimits
from noema_model.platform.redaction import Redactor

FILE = Path(__file__).resolve().parents[3] / "contracts" / "fixtures" / "digit_run_vectors.json"
R = Redactor.from_limits(MaskingLimits(4))


def _vectors() -> list[dict[str, Any]]:
    document = json.loads(FILE.read_text(encoding="utf-8"))
    vectors: list[dict[str, Any]] = document["vectors"]
    return vectors


VECTORS = _vectors()


def test_the_shared_vectors_are_named_once_and_say_something() -> None:
    assert len(VECTORS) >= 100
    names = [v["name"] for v in VECTORS]
    assert len(names) == len(set(names))
    assert all(v["name"] and v["proves"] and v["group"] for v in VECTORS)
    groups = {v["group"] for v in VECTORS}
    assert {"length", "separator_joins", "ends_a_run", "separators_in_a_row", "scripts", "comma", "money_accepted", "money_plain", "money_refused"} <= groups


@pytest.mark.parametrize("vector", VECTORS, ids=[v["name"] for v in VECTORS])
def test_the_redactor_gives_the_verdict_of_every_shared_vector(vector: dict[str, Any]) -> None:
    text: str = vector["input"]
    assert (R.mask_account_numbers(text) != text) is vector["refused"], vector["proves"]


@pytest.mark.parametrize("vector", VECTORS, ids=[v["name"] for v in VECTORS])
def test_redact_of_a_string_value_gives_the_same_verdict(vector: dict[str, Any]) -> None:
    text: str = vector["input"]
    assert (R.redact(text) != text) is vector["refused"], vector["proves"]
