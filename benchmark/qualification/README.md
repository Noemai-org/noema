# benchmark/qualification

**Implements:** REQ-INT-006 (UND-13); ruling A17 of 6 Nov 2026. Updated 2026-11-06.

Qualification records (`contracts/schemas/qualification-record.v1.json`) live in `records/`, one JSON file per record, named `<record_id>.json`. A record says that a pinned model (and speech model) was qualified, not qualified, or is waiting for a Founder-set pass^k floor, for the Understanding path on one provider. The model service's harness refuses a model ID that has no QUALIFIED record.

- **Who builds what:** the Benchmark Owner builds the runner and writes the records; Core Engineer 2 builds the model-service side (the drift check in `services/model`). The qualification subset is the Benchmark Owner's; held-out and oracle material never appear in a record or in this folder (INV-22).
- **Interim rule (A17):** QUALIFIED means Tier 0 failures are zero, the run is complete, the drift check passed and pass^k is not below the baseline. With no baseline the verdict is PENDING_FLOOR and the harness still refuses the model. A first qualification waits for a Founder-set floor.
- **The pass^k bar** is pack data set by a decision record. It is not a value in the schema or in code.
- A change to a record is a change to a protected path: both Founders approve (CODEOWNERS).
