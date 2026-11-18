// Package publicstage maps the investigation's internal state (PERD 28.2, 21 states) to the public stage the client may be told (investigation-response.v1; rulings A187, A207,
// A211, A243). The core owns this map; the frontend owns the plain-language string of each stage.
//
// May import: the standard library, internal/contracts, orchestrator (the State type only).
//
// Must never: guess (a state outside the table is an error), return an internal state name, or read a store, a clock or the Record.
package publicstage

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// ErrUnknownState is returned for a state the map does not name. The caller answers the internal error envelope and never a stage.
var ErrUnknownState = errors.New("publicstage: the state has no public stage")

var stages = map[orchestrator.State]contracts.InvestigationResponseV1PublicStage{
	"RECEIVED":                  contracts.InvestigationResponseV1PublicStageUnderstanding,
	"INTERPRETING":              contracts.InvestigationResponseV1PublicStageUnderstanding,
	"REJECTED_AT_GATE":          contracts.InvestigationResponseV1PublicStageNeedsInput,
	"AWAITING_CLARIFICATION":    contracts.InvestigationResponseV1PublicStageNeedsInput,
	"DORMANT":                   contracts.InvestigationResponseV1PublicStageNeedsInput,
	"AWAITING_CONFIRMATION":     contracts.InvestigationResponseV1PublicStageNeedsConfirmation,
	"EVALUATING":                contracts.InvestigationResponseV1PublicStageWorking,
	"WAITING":                   contracts.InvestigationResponseV1PublicStageWorking,
	"PLANNED":                   contracts.InvestigationResponseV1PublicStageWorking,
	"EXECUTING":                 contracts.InvestigationResponseV1PublicStageWorking,
	"AWAITING_VERIFICATION":     contracts.InvestigationResponseV1PublicStageWorking,
	"AWAITING_EXTERNAL_OUTCOME": contracts.InvestigationResponseV1PublicStageWorking,
	"RECOVERING":                contracts.InvestigationResponseV1PublicStageWorking,
	"EXPLAINING":                contracts.InvestigationResponseV1PublicStageWorking,
	"EXPLAINING_BLOCKED":        contracts.InvestigationResponseV1PublicStageWorking,
	"BLOCKED_UNKNOWN":           contracts.InvestigationResponseV1PublicStageUnavailable,
	"ESCALATING":                contracts.InvestigationResponseV1PublicStageUnavailable,
	"ESCALATED":                 contracts.InvestigationResponseV1PublicStageUnavailable,
	"COMPLETED":                 contracts.InvestigationResponseV1PublicStageAnswered,
	"REJECTED":                  contracts.InvestigationResponseV1PublicStageAnswered,
	"EXPIRED":                   contracts.InvestigationResponseV1PublicStageAnswered,
}

// Of returns the public stage of an internal state, or ErrUnknownState.
func Of(s orchestrator.State) (contracts.InvestigationResponseV1PublicStage, error) {
	st, ok := stages[s]
	if !ok {
		return "", ErrUnknownState
	}
	return st, nil
}
