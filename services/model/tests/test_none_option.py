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
    if model is OverlayCardV1ClarificationCard:
        with pytest.raises(ValidationError):
            model.model_validate_json(json.dumps(without))
    else:
        # A593, A600, A655: clarification.v1 requires none_option_id in every mode but SLOT, a conditional rule of the schema. The generated model carries shape, not conditional rules
        # (tools/codegen/README.md), so it reads the document; the schema refuses it (tools/contracts/test_none_option.py, test_clarification_slot_mode.py).
        model.model_validate_json(json.dumps(without))
    # A malformed value is a shape error in both models. A null is not tested for the clarification: its member is optional (not required in SLOT mode), so the model reads a null as
    # absent (the known gap in contracts/README.md), and the schema's conditional rule is what refuses a non-SLOT card without the member.
    for bad in ("None Of These", "n" * 33, "", 5) + (() if model is ClarificationV1 else (None,)):
        with pytest.raises(ValidationError):
            model.model_validate_json(json.dumps({**doc, "none_option_id": bad}))


def test_a_card_refuses_an_id_with_five_digits() -> None:
    with pytest.raises(ValidationError):
        OverlayCardV1ClarificationCard.model_validate({**CARD, "none_option_id": "none_12345"})


def test_a_slot_clarification_reads_with_no_candidate_set_and_no_none_choice() -> None:
    """A582, A593 (mode SLOT): nothing to choose from, so no candidate set, no options and no none choice."""
    doc = json.loads((FIXTURES / "clarification" / "slot_when.json").read_text())
    parsed = ClarificationV1.model_validate_json(json.dumps(doc))  # the wire path: the mode arrives as the string "SLOT" (A655)
    assert parsed.mode is not None and parsed.mode.value == "SLOT"
    assert parsed.candidate_set_id is None and parsed.candidate_set_version is None and parsed.none_option_id is None and parsed.allow_none is None and parsed.attribute is None
    assert not parsed.options


def test_an_options_clarification_reads_with_its_candidate_set_and_its_none_choice() -> None:
    """A655, the other mode: a candidate card carries the candidate set, its version, the options and the core-minted none choice, read from the wire form."""
    parsed = ClarificationV1.model_validate_json(json.dumps(CLARIFICATION))
    assert parsed.candidate_set_id == CLARIFICATION["candidate_set_id"] and parsed.candidate_set_version == CLARIFICATION["candidate_set_version"]
    assert parsed.none_option_id == CLARIFICATION["none_option_id"] and parsed.allow_none is True
    assert len(parsed.options) >= 2
