# Qualification records

One JSON file per qualification run, written by `python -m benchmark.qualification record` and read by `check` and by the model service's configuration check (UND-13, REQ-INT-006, ruling A17).
The format is `contracts/schemas/qualification-record.v1.json`. A file is named `<provider>__<model_id>__<version>__<qualified_on>.json`.

No record is committed yet. Interim QUALIFIED needs a complete run, Tier 0 at zero, a drift PASS and pass^k not below the bar; with no baseline record and no Founder-set floor, a first qualification
is NOT_QUALIFIED and waits for the floor (A17). A NOT_QUALIFIED record is still written and committed: a failed qualification is on file, and a later one revokes an earlier qualification.

Changes here need both founders (CODEOWNERS). A record is never hand-edited: it is recomputable from the scorecard and the run record, and `record.validate_record` refuses a QUALIFIED verdict its own fields do not support.
