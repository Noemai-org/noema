// Package interpreter is the core's step between the model service's proposal and the admitted intent (UND-04, core half; rulings A166, A168 and A177; REQ-INT-001,
// REQ-INT-002, REQ-INP-002, REQ-AUD-001; INV-02, INV-12, INV-17, EC-E16). It asks the model service to compile, runs the core's own gate over the answer, records what the
// Record must hold first (a refusal at the gate, a real identity override) and saves the admitted intent through the store, which appends the signed INTENT_ADMITTED
// event in the same transaction (A177: that event is the signature). It owns no wiring: the composition root builds the ports (cmd/core is EL's).
package interpreter
