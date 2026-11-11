# platform/redaction

**What it does.** The mandatory redactor of the core: masks an account-number candidate to its last four digits and removes the value under a secret-looking key (`token`, `secret`, `password`, `authorization`, `api_key`, `credential`, `bearer` and the like), in a log message, a log attribute (groups, errors, stringers and numbers by their text) and a trace attribute. `New(minDigits)` has no default; `Handler(inner)` wraps an `slog.Handler`; `FilterAttributes` is the trace attribute filter; `MaskAccountNumbers` masks one string.

**REQs.** REQ-SEC-003, REQ-OBS-002 (AGENT-13, POC-NFR-06).

**Status.** Built by SEC-03 (2026-11-11). **Not yet installed in `cmd/core`**: the threshold (`minDigits`) is a configuration value (AGENT-16) and the core has no configuration source for it yet; the model service reads `masking.account_run_min_digits` from its decision config. Until a source is decided the handler is a tested library, and the core's own log lines carry only IDs and fixed text. See the SEC-03 status file for the open question.

**Differences from the model service's redactor, on purpose.** A digit is any Unicode decimal digit (full-width, Arabic-Indic and Devanagari numbers are masked like ASCII ones). A run is digits with a single space or hyphen between two digits; two or more separators end a run (the same limit as the model service); the leak scanner (`tools/ci/leakscan.py`) is the second wall.

**Tests.** `go test ./platform/redaction/...` from `services/core/`. Each case plants a synthetic account number and checks it is masked; nine mutations of the redactor each turn the suite red (message, ASCII-only digits, secret keys, numbers, groups, `WithAttrs`, errors, threshold, trace filter). `edge/session/no_echo_test.go` plants a number in a refused request (path, query, bearer token, header, body) and checks nothing comes back in the response body or headers.
