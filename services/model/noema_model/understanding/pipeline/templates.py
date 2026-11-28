"""The pipeline's own prompt template: slot values as the customer said them. Versioned like every harness template."""

from noema_model.harness import PromptTemplate
from noema_model.harness.templates import DATA_RULES

SLOT_MENTIONS_V1 = PromptTemplate(
    template_id="pipeline.slot_mentions",
    version=1,
    system=(
        "You read one customer message and report what the customer said for each field in the required shape. "
        "You report words and readings only. You never decide, authorise or act, and you never invent a value the "
        "customer did not say. " + DATA_RULES
    ),
    instruction=(
        "For each field, set present to true only if the customer mentioned it. Copy their exact words into "
        "surface_form. Put your plain-text reading in proposed_value. Give your confidence between 0 and 1. "
        "If a field is not mentioned, set present to false and leave the text fields empty."
    ),
)


# A772 (interim; compile run 8): the gate's sentence is an explicit ask for an ACTION; a status question and a request to check, show, look up or explain are not one. The actions are described, never named
# (a name of one is a pack's word and a test forbids it here); no phrasing of a labelled set is copied into it (REQ-TST-006).
# UND-18 (ruling A603, interim). The two fixed sentences of a gated optional slot. They name no slot, no value and no example (INV-14, A533): which slot is gated, and at what threshold,
# is pack data (`understanding.slot_gates`). One wording for every gate; nothing here is tuned against a golden.
SLOT_GATE_V1 = (
    "Does the customer explicitly ask for an action to be taken for them (to redo, undo, return, pass on or stop something)? "
    "A question about status, or a request to check, show, look up or explain, is not an explicit ask."
)
SLOT_PICK_V1 = "Which one of these does the customer ask to be done? Choose exactly one of the listed options."
