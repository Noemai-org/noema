# benchmark/arms/a0_naive_diag

**Implements:** REQ-BEN-007.
**Owning track:** T4 (README only). **Not built, and not to be built by an agent session.**

Arm A0. A naive model: a diagnostic floor labelled "not a competitor", never in a "Noema beats" claim (PERD §50.1).

It is built by the arm builder (ARM), outside the build, only after Noema passes its own gates (plan BEN-20 to ARM-05), with the same fixtures, oracle isolation, instructions, adapters, auth context, capabilities, model access and time budget as Noema, and signed off as competent by the independent reviewer before any run. Record it with `contracts/schemas/arm-manifest.v1.json`. See [the arms README](../README.md).

Generated layer only. It never mounts, reads or infers anything from `benchmark-heldout` (INV-22, REQ-AGT-004).
