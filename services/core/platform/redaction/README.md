# platform/redaction

**What it does.** The mandatory redactor of the core (ruling A154, PERD §44.2). The longest digit run that reaches an outward surface is **four**. `MaskAccountNumbers` masks every run longer than four digits to asterisks plus its last four, Unicode digits included (full-width, Arabic-Indic, Devanagari). `Handler(inner)` wraps an `slog.Handler` so that no log message or attribute (groups, errors, stringers, numbers, maps and slices of plain values) reaches it unmasked; `FilterAttributes` is the trace attribute filter. The value under a secret-looking key (`token`, `secret`, `password`, `authorization`, `api_key`, `credential`, `bearer` and the like), everything inside a secret-looking group (including one opened with `WithGroup`), and any value it cannot read safely (a struct, a pointer, a byte slice) is replaced by `[REDACTED]`.

**No threshold.** `New()` takes nothing: the limit is the PERD's, not a setting (A154). Free text and string-valued fields are strict.

**The exemptions (an AGENT-16 constant; `redaction.Exemptions()`).** By type only; a Founder approves the list. Each has its own tests, and a test fails when the list and the tests differ.

| Name | What is left alone | What stays strict |
|---|---|---|
| `typed_id` | A whole token `<prefix>_<ULID>`: 2 to 6 lowercase letters, `_`, 26 Crockford characters (first one 0 to 7, at least one a letter), not touching a letter, digit or underscore | Ten digits in a string field; an ID-shaped token of digits only; a token one character too long or short; a token glued to other characters |
| `rfc3339_timestamp` | A whole token that parses as an RFC 3339 date-time, at most nine fraction digits, not touching a letter, digit, underscore or hyphen; a `time.Time` attribute is written this way | A date alone (`2026-11-12`); `2026111210`; a date-time that is not a time |
| `minor_money_field` | An integer or float attribute whose key ends in `_minor` | A string under that key; the same number under another key |
| `count_field` | An integer or float attribute whose key ends in `_count` | as above |
| `size_field` | An integer or float attribute whose key ends in `_bytes` | as above |
| `duration_field` | A `time.Duration` attribute; an integer or float attribute whose key ends in `_ms`, `_s`, `_seconds`, `_minutes` or `_micros` | as above |

The keys of the numeric exemptions are chosen by code, never by a user, a model or an enterprise field, and every suffix but `_bytes` already names fields in `contracts/schemas`. The trace filter takes strings only, so the numeric exemptions never apply to it.

**Known limits, stated.** (1) A comma ends a run, so an amount written `20,000,000` stays readable; a card number written with commas between groups of four would also survive. (2) A typed ID or timestamp token can carry digits of its own choosing inside the shape (a timestamp has up to nine fraction digits; a typed ID up to 25 characters of which some may be digits); the shape rules above are what keep this narrow, and the leak scanner (`tools/ci/leakscan.py`) is the second wall. (3) A date alone is masked: write it as a full RFC 3339 time. (4) Not yet installed in `cmd/core`; the composition root installs `Handler` on the core's logger and the telemetry filter.

**REQs.** REQ-SEC-003, REQ-OBS-002 (AGENT-13, POC-NFR-06).

**Tests.** `go test ./platform/redaction/...` from `services/core/`. Twenty-one mutations of the redactor each turn the suite red (the limit, ASCII-only digits, each exemption removed, loosened or widened, the secret-key rules, nested containers, `WithGroup`, `WithAttrs`, the message, the trace filter, the time kind). `edge/session/no_echo_test.go` plants a number in a refused request (path, query, bearer token, header, body) and checks no run of five digits comes back in the response body or headers.
