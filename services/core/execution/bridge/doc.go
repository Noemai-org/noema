// Package bridge joins the controller's ports to the adapters: CaseDispatcher is the Dispatcher of E9 and
// CaseRevalidator the Revalidator of E7 for mock-case. It carries a WriteResult across as a TransportResult and
// decides nothing: Execution classifies it and Outcome verifies it (INV-04, AGENT-21).
//
// Implements: REQ-EXE-004 (conditional writes), REQ-EXE-006 (one call, no retry), REQ-EXE-007 (raw status beside
// the class), REQ-EXE-003 (revalidation reads), REQ-CAP-002 (no mapping, no call). Task ENV-03 (the Execution
// bridge named open in status/ENV-03.md).
//
// May import: adapters, execution/controller.
//
// Must never: retry; send a conditional write without an expected version; call a capability it has no mapping for;
// import trust or outcome; declare success.
package bridge
