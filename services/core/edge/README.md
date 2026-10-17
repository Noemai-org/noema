# edge

**What it does.** Session authentication, rate limits, request and correlation IDs, server-sent events.

**REQs.** REQ-API-004, REQ-INP-002, REQ-SEC-008.

**Status.** Empty boundary package (PLT-01). PLT-01 creates the boundary; the edge is built by EXP and API tasks (plan §8 onward). The import rules and prohibitions are in `doc.go`.

**Tests.** `go test ./edge/...` from `services/core/` (none yet).
