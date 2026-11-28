package main

import (
	"github.com/Noemai-org/noema/services/core/orchestrator"
	verifierport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/verifier"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// recoveryWriteSeams are the parts of the recovery write path that other lanes own. A nil seam is a named gap, never a stand-in.
type recoveryWriteSeams struct {
	Verifier verifierport.Verifier // Outcome's verifier
	Requests outcomeRequestSeams   // the signature check, the pack's outcomes and the id source of the request reader
	Bindings orchestrator.Bindings // the Trust adapter
}

// buildRecoveryWritePorts is the red stub: it builds nothing.
func buildRecoveryWritePorts(records reconstruct.TenantDBs, parts *executionParts, s recoveryWriteSeams) (orchestrator.WritePorts, []string) {
	return orchestrator.WritePorts{}, []string{"recovery write: not built"}
}
