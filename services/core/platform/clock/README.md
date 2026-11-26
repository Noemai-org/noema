# platform/clock

**What it does.** The clock interface, real and simulated.

**REQs.** REQ-ENV-003.

**Status.** Empty boundary package (PLT-01). PLT-07. The import rules and prohibitions are in `doc.go`.

**Tests.** `go test ./platform/clock/...` from `services/core/`. `clock_property_test.go` (DATA-02, REQ-DATA-002): every instant the clock gives is UTC, the simulated clock adds exactly and never runs backwards.
