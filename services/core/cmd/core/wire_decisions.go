package main

import (
	"io"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// wire_decisions.go is the decisions lane of the G1 final composition (STM-01; A537, interim, relayed, recorded in docs#229; REQ-STM-001, REQ-TRU-002, REQ-TRU-008, REQ-TRU-009;
// INV-03, INV-16, AGENT-16): Intents, the Recorder, the Confirmations path and the Planner. It builds what can be built from what is on main, and names the rest as gaps. RED STUB.

// The fixed texts of a gap: what an adapter lacks, never a value (A469, A486). They are the only words a gap uses.
const (
	needTrustLogins    = "the Trust logins (NOEMA_TRUST_DSN_DIR)"
	needPackBytes      = "the verified pack's bytes"
	needPack           = "the verified pack"
	needGrantKeys      = "the grant keys"
	needRecordLogin    = "the Record write login of the core-trust identity"
	needSealedFacts    = "the sealed-facts source (snapshot reader, step facts, policy id)"
	needRecordSigners  = "the core-trust Record signing keys (no setting names them)"
	needRequestRead    = "a read of a confirmation request by its id (the store has Latest only)"
	needRequestEvents  = "the Record events of the confirmation store (no confirm-request or confirm-event contract)"
	needConfirmPolicy  = "the pack's confirmation terms (no accessor)"
	needGraphReader    = "the pack's action-graph reader (plangraphs.Pack)"
	needIntentSlots    = "the stored intent's slot reader (plangraphs.Intents)"
	needPlanContexts   = "the plan contexts (plangraphs.PlanContexts)"
	needRegistry       = "the pack's capability registry (no accessor)"
	needRunnableAdapt  = "an intake.Runnable adapter over the pack's graph conditions"
	needHandoffAdapter = "an intake.Handoffs adapter over the pack's handoff routes"
)

// Adapter names of the gaps that are not a TrustAdapters field: fields of intake.Service.
const (
	gapServiceRunnable = "Service.Runnable"
	gapServiceHandoffs = "Service.Handoffs"
)

// decisionGap names one thing of the decisions lane that is not built and what it lacks. Adapter is a TrustAdapters field name (or a Service field); Missing is fixed text.
type decisionGap struct {
	Adapter string
	Missing []string
}

// decisionInputs are what the lane consumes from the process and from the other lanes. Each one that is absent is named in a gap; none has a default.
type decisionInputs struct {
	Env      Env
	Clock    clock.Clock
	Pools    trustport.TenantPools
	PackJSON []byte
	Pack     *trustpack.Pack
	Keys     *grants.Keys
	Begin    trustport.TxBeginner
	Facts    trustport.RecordFactSource
	Appender *chain.Appender
	Entropy  io.Reader
}

// decisionParts is what the lane built. A nil adapter is in Gaps.
type decisionParts struct {
	Intents  intake.Intents
	Recorder intake.Recorder
	Gaps     []decisionGap
}

// buildDecisions builds the adapters of the lane.
func buildDecisions(in decisionInputs) (decisionParts, error) { return decisionParts{}, nil }

// fill sets the adapters this lane built on a TrustAdapters and leaves every other field as it is.
func (p decisionParts) fill(a TrustAdapters) TrustAdapters { return a }
