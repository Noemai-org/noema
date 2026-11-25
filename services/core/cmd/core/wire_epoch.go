package main

import (
	"github.com/Noemai-org/noema/services/core/evidence/epochport"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

// wire_epoch.go sets the Interpreter's two optional ports together (UND-04; rulings A520, A513, A482, interim, relayed, not yet committed; REQ-INT-001, REQ-SEC-004; INV-12, INV-14).
// With no setup both stay unset: epoch 1 and no subject, the G1 behaviour. With one, both are set, the subject from the pack view the gate uses and the epoch from the case list; a
// setup with a part missing is an error with fixed text, never half. In production no case-list reader is built in Compose yet, so the setup is absent and the epoch stays a named gap
// (status/UND-04-epoch-wiring.md).
func epochParts(view understanding.PackView, s *epochport.Setup) (func(understanding.AdmittedIntent) (string, bool), interpreter.Epochs, error) {
	if s == nil {
		return nil, nil, nil
	}
	if view == nil {
		return nil, nil, epochport.ErrRefused
	}
	epochs, err := epochport.New(*s)
	if err != nil {
		return nil, nil, err
	}
	return understanding.SubjectOf(view), epochs, nil
}
