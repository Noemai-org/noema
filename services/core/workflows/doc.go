// Package workflows holds the Temporal workflows and activities: watches, confirmation expiry, recovery orchestration and callback delivery (SoT §35.6; PERD §36.1). Workflow code does no I/O; Temporal is never authoritative (INV-30).
//
// May import: internal/contracts and, in activities only, the packages they call through interfaces.
//
// Must never: do I/O in workflow code, read the wall clock, hold authoritative state, or import a model or provider client.
//
// Retry policy (D8, PERD 36.1): retry.go declares, as data, which activities write and which read, and how each
// retries. Workflow definitions take their activity options from it (options.go). The Temporal SDK
// (go.temporal.io/sdk) is approved (DEC-2026-10-25, AGENT-14); the escalate case (escalate.go) is the first workflow.
package workflows
