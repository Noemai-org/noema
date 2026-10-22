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
