"""Provider-neutral streaming speech adapters (Aethex AI, Indigenius, N-ATLAS).

Implements: REQ-VOI-002, REQ-VOI-003.

May import: noema_model.platform and noema_model.providers.llm.base (the shared ProviderError and AdapterSettings).

Must never: be imported by anything but the harness and the voice pipeline's injected wiring; depend on a single vendor (BD-16); pass a partial transcript as final.

VOI-01: `base.py` (types, stream-order contract), `matrix.py` with `capability_matrix.json`, `scaffolds.py` and `registry.py`. The three adapters are scaffolds that refuse until a vendor document and a recorded sample exist.
"""
