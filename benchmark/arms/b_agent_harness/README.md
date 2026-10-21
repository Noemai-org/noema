# benchmark/arms/b_agent_harness

**Implements:** REQ-BEN-007.
**Owning track:** T4 (README only). **Not built, and not to be built by an agent session.**

Arm B. An agent plus harness plus the best equivalents of Noema's primitives. It must include a decision model as its gate: a competent internal build in 2026 will, and a comparison against an arm without one is a comparison against yesterday's alternative.

It is built by the arm builder (ARM), outside the build, only after Noema passes its own gates (plan BEN-20 to ARM-05), with the same fixtures, oracle isolation, instructions, adapters, auth context, capabilities, model access and time budget as Noema, and signed off as competent by the independent reviewer before any run. Record it with `contracts/schemas/arm-manifest.v1.json`. See [the arms README](../README.md).

Generated layer only. It never mounts, reads or infers anything from `benchmark-heldout` (INV-22, REQ-AGT-004).
