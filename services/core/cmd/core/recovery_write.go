package main

import (
	"github.com/Noemai-org/noema/services/core/orchestrator"
	verifierport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/verifier"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// recovery_write.go is the write path of recovery (STM-05; A696, A675, A692, interim, relayed; REQ-VER-004, REQ-VER-001, REQ-SEC-004; INV-03, INV-04, INV-07, INV-12, AGENT-08). Recovery
// verifies what the Record shows as spent and never spends: its write ports are Outcome's Verifier over the request reader of LANE-5 slice 4, the Journal over the Record reader, the Trust
// adapter's Bindings and an Executor that always refuses. So it does not wait for the dispatcher of the live write path. It builds only from parts that are all present; a part that is
// missing is a named gap in fixed text and no ports are built.

// recoveryWriteSeams are the parts of the recovery write path that other lanes own. A nil seam is a named gap, never a stand-in.
type recoveryWriteSeams struct {
	Verifier verifierport.Verifier // Outcome's verifier
	Requests outcomeRequestSeams   // the signature check, the pack's outcomes and the id source of the request reader
	Bindings orchestrator.Bindings // the Trust adapter
}

// buildRecoveryWritePorts builds the ports over the Record reader (the Journal) and the outcome pools of slice 1's parts (the request reader), or returns the zero WritePorts and the gap
// lines, in a fixed order: this file's own first, then the request reader's. A part-built set is never returned (NewWriteRunner would refuse it anyway).
func buildRecoveryWritePorts(records reconstruct.TenantDBs, parts *executionParts, s recoveryWriteSeams) (orchestrator.WritePorts, []string) {
	var gaps []string
	for _, c := range []struct {
		missing bool
		gap     string
	}{
		{records == nil, "recovery write: record reader missing"},
		{s.Verifier == nil, "recovery write: outcome verifier missing"},
		{s.Bindings == nil, "recovery write: bindings missing"},
	} {
		if c.missing {
			gaps = append(gaps, c.gap)
		}
	}
	requests, requestGaps := buildOutcomeRequests(parts, s.Requests)
	gaps = append(gaps, requestGaps...)
	if len(gaps) > 0 {
		return orchestrator.WritePorts{}, gaps
	}
	return orchestrator.WritePorts{
		Executor: recovery.NoDispatch{},
		Verifier: verifierport.Port{Verifier: s.Verifier, Requests: requests},
		Journal:  tenantJournal{dbs: records},
		Bindings: s.Bindings,
	}, nil
}
