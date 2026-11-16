// Package recorder is the Record-backed Recorder of the Explainer (EXP-01 slice 5, ruling A155): it appends EXPLANATION_RENDERED or EXPLANATION_BLOCKED to the
// Record as core-edge, so the Explainer returns a text only after the event that justifies it is stored (REQ-AUD-002, EC-E16).
//
// Implements: REQ-EXP-002, REQ-AUD-001, REQ-AUD-002, REQ-AUD-003; INV-11, INV-12.
//
// May import: explanation (for Subject and Event only), record/chain. It reads nothing from the Record and holds no credential beyond the pool it is given: the
// tenant's own core-edge emitter login, which can do nothing but append and which the database binds to the tenant and the emitter (migration 0011).
//
// Must never: write an event the reconstruction would refuse to serve; put the text of a blocked explanation, or any text over the limit, in the Record; take the
// tenant from anything but the login (a subject of another tenant is refused by the database); retry an append.
package recorder
