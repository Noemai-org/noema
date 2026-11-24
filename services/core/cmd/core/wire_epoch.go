package main

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/evidence/epochport"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

// epochParts gives the Interpreter's Subject and Epochs together, or neither.
func epochParts(understanding.PackView, *epochport.Setup) (func(understanding.AdmittedIntent) (string, bool), interpreter.Epochs, error) {
	return nil, nil, epochport.ErrRefused
}

func buildUnderstandingWith(Env, clock.Clock, *orchestratorRecorder, orchestrator.Admissions, *epochport.Setup) (UnderstandingParts, error) {
	return UnderstandingParts{}, errors.New("cmd/core: not built")
}
