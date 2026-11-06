"""The drift canary's way of asking: the service's own provider adapter, one request per qualification case (REQ-INT-006, D25).

Lives in the harness because only the harness imports providers. `build` turns a case into the request the service would send (the prompt and the schema belong to the pipeline,
not to the canary). A failed call is AskerUnavailable. A reply with no usable structured answer (refused, truncated, not JSON, not an object) is None, i.e. unavailable. A reply that does not
report its version is attributed to no version, so it can never match the pin.
"""

import json
from collections.abc import Callable, Mapping
from typing import Any

from noema_model.providers.llm.base import FinishReason, LLMProvider, ProviderError, ProviderRequest
from noema_model.qualification.canary import AskerUnavailable, CaseAnswer

UNREPORTED = "(version not reported)"


class ProviderAsker:
    def __init__(self, provider: LLMProvider, build: Callable[[Mapping[str, Any]], ProviderRequest]) -> None:
        self._provider = provider
        self._build = build

    async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
        try:
            response = await self._provider.complete(self._build(case))
        except ProviderError as err:
            raise AskerUnavailable(str(err)) from err
        if response.finish is not FinishReason.COMPLETE:
            return None
        try:
            slots = json.loads(response.output_json)
        except ValueError:
            return None
        if not isinstance(slots, dict):
            return None
        return CaseAnswer(slots=slots, answered_by=response.model_version_reported or UNREPORTED)
