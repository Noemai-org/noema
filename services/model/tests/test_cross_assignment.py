"""CON-06 (OD-P-10, DEC-2026-10-17 section 3; REQ-API-001): the generated Pydantic types of enums.v1.json are distinct, and a value of one is refused by every other that does
not hold the same word. The names stay as the SoT writes them: OUTCOME_UNKNOWN is an Execution Receipt state and EXECUTION_OUTCOME_UNKNOWN a transaction classification; S0 to S3
are finding severities and S3_financial_pii a data sensitivity. DataSensitivity is a pattern, not a list, so it is checked here (the generated Go types do not check patterns;
internal/contracts/cross_assignment_test.go covers the closed lists there). A closed list added to the contract and not to this module fails the coverage test."""
import json
from enum import Enum
from pathlib import Path

import pytest
from noema_model.contracts import enums_v1
from noema_model.contracts._base import validator_for
from pydantic import ValidationError

ROOT = Path(__file__).resolve().parents[3]
DEFS = json.loads((ROOT / "contracts" / "schemas" / "enums.v1.json").read_text())["$defs"]
LISTS = {name: d["enum"] for name, d in DEFS.items() if "enum" in d}


def accepts(type_: object, value: str) -> bool:
    try:
        validator_for(type_).validate_json(json.dumps(value))
    except ValidationError:
        return False
    return True


def generated(name: str) -> type[Enum]:
    return getattr(enums_v1, "EnumsV1" + name)  # type: ignore[no-any-return]


def test_every_closed_list_of_the_contract_has_a_generated_type_with_exactly_its_values() -> None:
    for name, values in LISTS.items():
        assert [m.value for m in generated(name)] == values, name


def test_a_value_of_one_closed_list_is_refused_by_every_other_list_that_does_not_hold_it() -> None:
    tried = 0
    for a, values in LISTS.items():
        for v in values:
            assert accepts(generated(a), v), f"{a} refuses its own value {v}"
            for b, other in LISTS.items():
                if a == b or v in other:
                    continue  # the same word in two lists is the same word
                tried += 1
                assert not accepts(generated(b), v), f"{a} value {v!r} was accepted as a {b}"
    assert tried >= 500, f"only {tried} cross-assignments were tried: the matrix is not reading the contract"


@pytest.mark.parametrize("value", ["S0", "S1", "S2", "S3", "S3_financial_pii", "S4_special", "S3 ", "s3_financial_pii", "S3_", "S33_x", "S3_financial_pii\n"])
def test_a_finding_severity_and_a_data_sensitivity_are_not_interchangeable(value: str) -> None:
    severity = value in LISTS["FindingSeverity"]
    sensitivity = accepts(enums_v1.EnumsV1DataSensitivity, value)
    assert accepts(generated("FindingSeverity"), value) is severity
    assert not (severity and sensitivity), f"{value!r} is both a severity and a sensitivity"
    assert sensitivity is (value in {"S3_financial_pii", "S4_special"})


def test_the_receipt_state_and_the_transaction_classification_are_not_interchangeable() -> None:
    state = generated("ExecutionState")
    assert accepts(state, "OUTCOME_UNKNOWN")
    assert not accepts(state, "EXECUTION_OUTCOME_UNKNOWN")
