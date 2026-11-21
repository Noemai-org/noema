"""A275 (REQ-AMB-004, PERD §39.2; AGENT-15): the generated models of the clarification and of the overlay card carry the core-minted `none_option_id`, and refuse a clarification
without it or with an ID that is not of the form of an option ID. That it differs from every option ID of the same clarification is not shape: tools/contracts/validate.py checks it.
All data is SYNTHETIC."""
import json
from pathlib import Path

import pytest
from noema_model.contracts._base import ContractModel
from noema_model.contracts.clarification_v1 import ClarificationV1
from noema_model.contracts.overlay_card_v1 import OverlayCardV1ClarificationCard
from pydantic import ValidationError

FIXTURES = Path(__file__).resolve().parents[3] / "contracts" / "fixtures" / "valid"
CLARIFICATION = json.loads((FIXTURES / "clarification" / "two_options.json").read_text())
CARD = json.loads((FIXTURES / "overlay-card" / "clarification_four_options.json").read_text())


@pytest.mark.parametrize(("model", "doc"), [(ClarificationV1, CLARIFICATION), (OverlayCardV1ClarificationCard, CARD)])
def test_the_model_reads_and_writes_the_id_of_none_of_these(model: type[ContractModel], doc: dict[str, object]) -> None:
    parsed = model.model_validate(doc)
    assert getattr(parsed, "none_option_id") == doc["none_option_id"]
    assert json.loads(parsed.model_dump_json(exclude_none=True))["none_option_id"] == doc["none_option_id"]


@pytest.mark.parametrize(("model", "doc"), [(ClarificationV1, CLARIFICATION), (OverlayCardV1ClarificationCard, CARD)])
def test_the_model_refuses_a_clarification_without_the_id_and_one_with_a_malformed_id(model: type[ContractModel], doc: dict[str, object]) -> None:
    without = {k: v for k, v in doc.items() if k != "none_option_id"}
    with pytest.raises(ValidationError):
        model.model_validate(without)
    for bad in ("None Of These", "n" * 33, "", 5, None):
        with pytest.raises(ValidationError):
            model.model_validate({**doc, "none_option_id": bad})


def test_a_card_refuses_an_id_with_five_digits() -> None:
    with pytest.raises(ValidationError):
        OverlayCardV1ClarificationCard.model_validate({**CARD, "none_option_id": "none_12345"})
