"""UND-13, REQ-INT-006, AGENT-28 (EL's reviewer-role statement on noema#215 at ddb9be1, finding 1; Engineering Lead statement of 7 Nov): a malformed record must never allow.

A comparison with NaN is false, so `rate < bar` would not add a reason for a NaN rate; a bool is an int; a string is not a number; `$` in a regular expression accepts a trailing
newline; an unhashable reason raised TypeError instead of refusing. Each of these must refuse the identity (fail closed), never allow, never raise. SYNTHETIC records.
"""

import copy
import json
import math
from typing import Any

import pytest

from noema_model.qualification.gate import check_model
from tests.support import FIXTURES

GOLDEN: dict[str, Any] = json.loads((FIXTURES / "qualification" / "gate_golden.json").read_text())
QUALIFIED = next(c for c in GOLDEN["lookups"] if c["name"] == "qualified")
BASE: dict[str, Any] = copy.deepcopy(GOLDEN["records"][QUALIFIED["records"][0]])
WHO = (QUALIFIED["provider"], QUALIFIED["model_id"], QUALIFIED["version"], QUALIFIED["kind"])
BAD_RATIOS: list[Any] = [math.nan, math.inf, -math.inf, True, False, "0.99", None, [], {}, 1.01, -0.01]


def lookup(record: dict[str, Any]) -> list[str]:
    return check_model([record], *WHO)


def test_the_base_record_is_allowed() -> None:
    assert BASE["verdict"] == "QUALIFIED" and lookup(copy.deepcopy(BASE)) == []


@pytest.mark.parametrize("bad", BAD_RATIOS, ids=[repr(b) for b in BAD_RATIOS])
def test_a_pass_k_rate_that_is_not_a_ratio_refuses_the_model(bad: Any) -> None:
    record = copy.deepcopy(BASE)
    record["pass_k"]["rate"] = {**(record["pass_k"].get("rate") or {}), "rate": bad}
    assert lookup(record) != [], f"rate {bad!r} allowed the model"


@pytest.mark.parametrize("field", ["baseline_pass_k", "floor"])
@pytest.mark.parametrize("bad", [b for b in BAD_RATIOS if b is not None], ids=[repr(b) for b in BAD_RATIOS if b is not None])
def test_a_bar_that_is_not_a_ratio_refuses_the_model(field: str, bad: Any) -> None:
    record = copy.deepcopy(BASE)
    record["bar"][field] = bad
    assert lookup(record) != [], f"bar {field}={bad!r} allowed the model"


@pytest.mark.parametrize("field", ["scorecard_sha256", "qualified_on"])
def test_a_trailing_newline_does_not_pass_a_digest_or_a_date(field: str) -> None:
    record = copy.deepcopy(BASE)
    record[field] = record[field] + "\n"
    assert lookup(record) != []


@pytest.mark.parametrize("reasons", [[["a"]], [{"a": 1}], [1], [None]], ids=["list", "dict", "int", "none"])
def test_a_reason_that_is_not_a_string_refuses_instead_of_raising(reasons: list[Any]) -> None:
    record = copy.deepcopy(BASE)
    record["verdict"] = "NOT_QUALIFIED"
    record["reasons"] = reasons
    assert lookup(record) != []  # a TypeError here would be an error, not a refusal


def test_a_record_must_not_be_changed_by_the_lookup() -> None:
    record = copy.deepcopy(BASE)
    before = json.dumps(record, sort_keys=True)
    lookup(record)
    assert json.dumps(record, sort_keys=True) == before
