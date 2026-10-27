# execution/bridge

**What it does.** Joins the controller's ports to `adapters.Case` for mock-case: `CaseDispatcher` (E9) and `CaseRevalidator` (E7). It carries an adapter `WriteResult` across as the controller's `TransportResult` (class, acknowledgement, raw code beside the class, key-collision flag) and decides nothing.

**REQs.** REQ-EXE-004 (a route is a conditional write: no expected version, no call), REQ-EXE-006 (one call, never a retry), REQ-EXE-007 (raw status next to the class), REQ-EXE-003 (revalidation reads), REQ-CAP-002 (a capability with no mapping, or a parameter the descriptor does not name, makes no call).

**Mapped capabilities.** `fin.cap.create_escalation_case` to `CreateCase` (the controller's idempotency key, the grant's principal, the plan's parameters) and `fin.cap.route_to_internal_owner` to `RouteCase` (the grant's target version as the expected token). Nothing else is callable.

**Revalidation.** For a route: the case is read fresh and must be open or routed and hold the grant's version token. For a create: no open or routed case for the principal and transaction in this epoch (an incomplete page is an error, never a pass). `subject_resource.status` of the create is a ledger read: Tushe-dependent, paused (25 Oct 2026), not built.

**Tests.** `go test ./execution/bridge/...` from `services/core/`. They use a recording stand-in for `adapters.Case` to check what is sent and how the answer is reported; the adapter's behaviour under faults is covered by its own contract tests and by the end-to-end run against the real mock-case.
