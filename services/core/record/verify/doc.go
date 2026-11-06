// Package verify: Nightly and on-demand chain verification, and signed chain-head checkpoints exported
// outside Postgres (PERD v1.11).
//
// Implements: REQ-AUD-005, REQ-AUD-001.
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, record/chain.
//
// Must never: repair or rewrite a broken chain; keep a checkpoint only inside Postgres; ignore a break (it
// raises an alert and the kill switch).
//
// Built by REC-05 (slice 1): Verifier.Run for the nightly job, Verifier.RunTenant for the on-demand form. The signed,
// exported checkpoints (REC-07) and the HTTP endpoint (EDG-01 wires it behind the ops and audit role check) are later slices.
package verify
