"""The model service's side of the schema gate (UND-04; REQ-INT-001, REQ-INT-002, REQ-INP-002; INV-02, INV-12).

Implements: the gate between the Intent Compiler's proposal and everything after it. The proposal (status EXTRACTED, no `sig`) is decoded through the generated strict
contract model, then held to the rules a closed shape cannot state: the identity it must equal, the pack it names, the node and slots the pack defines, the supersedes
chain. It is returned as VALIDATED, or refused with a sorted, unique list of reasons.

May import: noema_model.contracts, the standard library, pydantic. It reads no pack file (the caller passes a PackView), holds no key and no credential, calls no
model and no network, and has no clock.

Must never: repair, complete, re-type or infer a field (REQ-INT-002); sign (the model service signs after the gate, so a `sig` in the proposal is refused); admit an
identity other than the Context's (INV-12); quote the object in a reason (AGENT-13); raise on any input.
"""

from noema_model.understanding.gate.gate import UNSIGNED, Context, GateResult, Node, PackView, Reason, SchemaGate

__all__ = ["UNSIGNED", "Context", "GateResult", "Node", "PackView", "Reason", "SchemaGate"]
