package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// wire_decisions.go is the decisions lane of the G1 final composition (STM-01; A537, interim, relayed, recorded in docs#229; REQ-STM-001, REQ-TRU-002, REQ-TRU-008, REQ-TRU-009;
// INV-03, INV-16, AGENT-16): Intents, the Recorder, the Confirmations path and the Planner. It builds what can be built from what is on main, and names the rest as gaps.

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

// buildDecisions builds the adapters of the lane from its inputs and returns them with the gaps. A setting or an input that is absent is a gap (A486: the process starts, /readyz names
// the adapter); a present setting that is invalid, pack bytes the adapter cannot trust and an escalation graph the pack lacks are errors that name the setting and never a value (A469,
// INV-16). No setting has a default (A330). The adapters it cannot build at all are gaps that always appear: Confirmations, Planner and the two Service fields.
func buildDecisions(in decisionInputs) (decisionParts, error) {
	var p decisionParts
	intents, intentsMissing, err := decisionIntents(in)
	if err != nil {
		return decisionParts{}, err
	}
	rec, recMissing, err := decisionRecorder(in)
	if err != nil {
		return decisionParts{}, err
	}
	p.Intents, p.Recorder = intents, rec
	add := func(adapter string, missing []string) {
		if len(missing) > 0 {
			p.Gaps = append(p.Gaps, decisionGap{Adapter: adapter, Missing: missing})
		}
	}
	add("Intents", intentsMissing)
	add("Confirmations", []string{needRequestRead, needRequestEvents, needConfirmPolicy})
	add("Planner", []string{needGraphReader, needIntentSlots, needPlanContexts, needRegistry})
	add("Recorder", recMissing)
	add(gapServiceRunnable, []string{needRunnableAdapt})
	add(gapServiceHandoffs, []string{needHandoffAdapter})
	return p, nil
}

// fill sets the adapters this lane built on a TrustAdapters and leaves every other field as it is: a gap leaves its field unset, and an unset field is named by /readyz.
func (p decisionParts) fill(a TrustAdapters) TrustAdapters {
	if p.Intents != nil {
		a.Intents = p.Intents
	}
	if p.Recorder != nil {
		a.Recorder = p.Recorder
	}
	return a
}

// absent is true for a nil input, a typed nil inside an interface included (no quiet nil, INV-03).
func absent(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return r.IsNil()
	}
	return false
}

// decisionIntents builds the admitted-intent store adapter (REQ-TRU-003): the verified pack's bytes, the tenant's own Trust login and the read bound of EnvTrustDBTimeout.
func decisionIntents(in decisionInputs) (intake.Intents, []string, error) {
	var missing []string
	if absent(in.Pools) {
		missing = append(missing, needTrustLogins)
	}
	if len(in.PackJSON) == 0 {
		missing = append(missing, needPackBytes)
	}
	var timeout time.Duration
	if in.Env.Getenv(EnvTrustDBTimeout) == "" {
		missing = append(missing, EnvTrustDBTimeout)
	} else {
		var err error
		if timeout, err = recorderSettingMS(in.Env, EnvTrustDBTimeout); err != nil {
			return nil, nil, err
		}
	}
	if len(missing) > 0 {
		return nil, missing, nil
	}
	si, err := trustport.NewStoredIntents(in.Pools, in.PackJSON, timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: the admitted-intent store adapter: %w", err)
	}
	return si, nil, nil
}

// decisionRecorder builds the Decision Record writer (REQ-TRU-008, REQ-TRU-010) from decisionRecorderConfig. The DeclinedRequestID mapping is noema#892's: nothing here touches it.
func decisionRecorder(in decisionInputs) (intake.Recorder, []string, error) {
	cfg, missing, err := decisionRecorderConfig(in)
	if err != nil || len(missing) > 0 {
		return nil, missing, err
	}
	ra, err := trustport.NewRecorderAdapter(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: the Recorder: %w", err)
	}
	return ra, nil, nil
}

// decisionRecorderConfig is the Recorder's configuration: the three lifetimes of loadRecorderSettings, the escalation set of the pack's escalation graph, and the seams no adapter on
// main fills. With anything absent it returns the names of what is absent and no configuration.
func decisionRecorderConfig(in decisionInputs) (trustport.RecorderConfig, []string, error) {
	var missing []string
	present := 0
	for _, n := range []string{EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS, EnvTrustTxTimeoutMS} {
		if in.Env.Getenv(n) == "" {
			missing = append(missing, n)
			continue
		}
		present++
		if _, err := recorderSettingMS(in.Env, n); err != nil {
			return trustport.RecorderConfig{}, nil, err
		}
	}
	var cfg trustport.RecorderConfig
	graph := in.Env.Getenv(EnvTrustEscalationGraph)
	switch {
	case graph == "":
		missing = append(missing, EnvTrustEscalationGraph)
	case in.Pack != nil:
		set, err := trustpack.EscalationSetFromPack(in.Pack, graph)
		if err != nil {
			return trustport.RecorderConfig{}, nil, fmt.Errorf("cmd/core: %s: %w", EnvTrustEscalationGraph, err)
		}
		cfg.Escalation = set
	}
	for _, need := range []struct {
		absent bool
		text   string
	}{{in.Pack == nil, needPack}, {absent(in.Keys), needGrantKeys}, {absent(in.Begin), needRecordLogin}, {absent(in.Facts), needSealedFacts}, {absent(in.Appender), needRecordSigners}} {
		if need.absent {
			missing = append(missing, need.text)
		}
	}
	if present == 3 {
		s, err := loadRecorderSettings(in.Env) // grant at most record (A503)
		if err != nil {
			return trustport.RecorderConfig{}, nil, err
		}
		cfg.GrantLifetime, cfg.RecordLifetime, cfg.TxTimeout = s.GrantLifetime, s.RecordLifetime, s.TxTimeout
	}
	if len(missing) > 0 {
		return trustport.RecorderConfig{}, missing, nil
	}
	if absent(in.Clock) {
		return trustport.RecorderConfig{}, nil, errors.New("cmd/core: the Recorder needs the clock: it is not configured")
	}
	entropy := in.Entropy
	if entropy == nil {
		entropy = rand.Reader
	}
	cfg.Begin, cfg.Facts, cfg.Keys, cfg.Appender, cfg.Clock, cfg.IDs, cfg.Entropy = in.Begin, in.Facts, in.Keys, in.Appender, in.Clock, ids.New(in.Clock, entropy), entropy
	return cfg, nil, nil
}
