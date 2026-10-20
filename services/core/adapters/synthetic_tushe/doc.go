// Package synthetic_tushe: Clients for the synthetic enterprise (ledger, switch, destination, customer,
// policy, case, clock): normalise responses and errors behind interfaces.
//
// Implements: REQ-ENV-001, REQ-NFR-006, REQ-EXE-007, REQ-CTX-008.
//
// May import: platform/{clock,config,telemetry,redaction}, internal/contracts.
//
// Must never: expose a monetary call (BD-08); be imported by anything but execution and evidence (import-
// adapters); hold an oracle credential (INV-22).
//
// Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with
// the engine tracks, and nothing is built here yet.
package synthetic_tushe
