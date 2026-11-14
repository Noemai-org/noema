"""The longest unmasked digit run is pack data, read from the one file the core reads (SEC-03, REQ-SEC-003, AGENT-16, AGENT-13).

masking-limits.v1: a value below the four digits a mask keeps, above the maximum, not a whole number, a missing field, an unknown field or anything after the document
is refused; there is no default. The Redactor built from it masks the first run longer than the value. Ruling A154 as corrected (relayed, not yet committed). SYNTHETIC.
"""

import json
from pathlib import Path

import pytest

from noema_model.platform.masking import MaskingLimits, MaskingLimitsError, load_limits, load_limits_file
from noema_model.platform.redaction import Redactor

PACK_FILE = Path(__file__).resolve().parents[3] / "packs" / "fintech" / "limits" / "masking_limits.json"


def doc(**over: object) -> str:
    base: dict[str, object] = {"schema_version": "1.0", "pack_id": "fin", "label": "SYNTHETIC", "max_unmasked_digit_run": 4}
    base.update(over)
    return json.dumps(base)


def test_the_packs_limit_is_four_and_a_five_digit_run_is_the_first_masked() -> None:
    limits = load_limits_file(PACK_FILE)
    assert (limits.max_unmasked_run, limits.min_masked_run, limits.kept_digits) == (4, 5, 4)
    redactor = Redactor.from_limits(limits)
    assert redactor.mask_account_numbers("ref 1234") == "ref 1234"
    assert redactor.mask_account_numbers("ref 12345") == "ref *2345"
    assert redactor.mask_account_numbers("acct 0123456789") == "acct ******6789"


@pytest.mark.parametrize(
    "text",
    [
        doc(max_unmasked_digit_run=3), doc(max_unmasked_digit_run=5), doc(max_unmasked_digit_run=8), doc(max_unmasked_digit_run=32), doc(max_unmasked_digit_run=33), doc(max_unmasked_digit_run=0), doc(max_unmasked_digit_run=-4),
        doc(max_unmasked_digit_run=4.5), doc(max_unmasked_digit_run="4"), doc(max_unmasked_digit_run=True), doc(max_unmasked_digit_run=None),
        doc(schema_version="2.0"), doc(label="REAL"), doc(pack_id="fin\n"), doc(pack_id="FIN"), doc(min_masked_run=5),
        json.dumps({"schema_version": "1.0", "pack_id": "fin", "label": "SYNTHETIC"}),
        doc() + doc(), "four", "", "[]", "null",
    ],
)
def test_any_deviation_from_the_contract_is_refused(text: str) -> None:
    with pytest.raises(MaskingLimitsError):
        load_limits(text)


def test_a_limit_object_cannot_be_made_with_a_bad_value() -> None:
    for bad in (3, 5, 8, 32, 33, 4.5, True, None, "4"):
        with pytest.raises(MaskingLimitsError):
            MaskingLimits(max_unmasked_run=bad)  # type: ignore[arg-type]
