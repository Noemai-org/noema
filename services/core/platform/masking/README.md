# platform/masking

**What it does.** Reads the longest digit run that may appear unmasked from the pack's `masking-limits.v1` document (`packs/fintech/limits/masking_limits.json`, value 4). It is the one source for the core's redactor, the explanation validator (PERD 31.2 check 4, `explanation.Templates.WithMaskingLimits`) and the error-scanner test, and the model service reads the same file (`noema_model.platform.masking`, `Redactor.from_limits`). `benchmark/isolation/check_isolation.py` reads it too. There is no default: `Load` returns a `*masking.Error` and the zero `Limits` for anything that is not exactly the contract, and a `Limits` that was never loaded reports `Loaded() == false` so a caller blocks instead of using zero.

**REQs.** REQ-SEC-003, REQ-EXP-002, AGENT-13, AGENT-16. Ruling A154 as corrected (relayed, not yet committed).

**Not here.** The exemption by type (typed IDs, RFC 3339 timestamps, number-typed structured fields) is the Security Reviewer's list (noema#457) and is applied by the caller, not by this value. The core's redactor (noema#457) takes `Limits.MinMaskedRun()` as its threshold once that PR merges. The model service's harness still reads its own `masking.account_run_min_digits` from the frozen run configs (`benchmark/decisions/harness-*.json`, hashed in a decision record); moving it to this file needs those files and the record changed, which is the Founders' (see `status/SEC-03.md`).

**Tests.** `go test ./platform/masking/` from `services/core/`.
