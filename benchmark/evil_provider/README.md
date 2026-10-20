# benchmark/evil_provider

**Implements:** REQ-TST-004, REQ-BEN-005 (adversarial case ADV-08).
**Owning track:** T4.

An OpenAI-compatible HTTP server for the `bench` profile only, on `provider_egress`, returning adversarial outputs. No evil-provider code exists in any product image; the model service reaches it through ordinary harness configuration.

Generated layer only. It never mounts, reads or infers anything from `benchmark-heldout` (INV-22, REQ-AGT-004).

Created by T1 of DEC-2026-10-20. Empty: nothing is built here yet.
