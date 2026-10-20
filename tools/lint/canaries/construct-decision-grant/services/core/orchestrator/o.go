// Package orchestrator is a canary: it builds a grant itself.
package orchestrator

import "github.com/Noemai-org/noema/services/core/trust/grants"

// Forge builds a grant outside trust/grants.
func Forge() grants.DecisionGrant { return grants.DecisionGrant{ID: "forged"} }
