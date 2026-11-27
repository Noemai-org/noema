package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/signing"
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
	needPack           = "the verified pack"
	needGrantKeys      = "the grant keys"
	needRecordLogin    = "the Record write login of the core-trust identity"
	needSealedFacts    = "the sealed-facts source (snapshot reader, step facts, policy id)"
	needRequestRead    = "a read of a confirmation request by its id (the store has Latest only)"
	needRequestEvents  = "the Record events of the confirmation store (no confirm-request or confirm-event contract)"
	needConfirmPolicy  = "the pack's confirmation expiry and the capability an intent confirms (the pack gives the approver requirement only)"
	needRunnableAdapt  = "an intake.Runnable adapter over the pack's graph conditions"
	needHandoffAdapter = "an intake.Handoffs adapter over the pack's handoff routes"
)

// The settings of the core-trust Record signing keys (A548, interim, relayed, not yet committed): the decision records are signed on the Go side and verified by the database
// (migrations 0012 and 0019). Both are required with the other Recorder inputs and neither has a default (A330).
const (
	// EnvTrustKeyDir names the directory that holds the signing keys of the identity core-trust (core-trust.ed25519.<n>.key, mode 0400; execution/signing.LoadRing).
	EnvTrustKeyDir = "NOEMA_TRUST_KEY_DIR"
	// EnvTrustKeyMaxActive is how many keys of that identity may be mounted at once: the rotation overlap (REQ-SEC-005). A positive integer.
	EnvTrustKeyMaxActive = "NOEMA_TRUST_KEY_MAX_ACTIVE"
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
	Env     Env
	Clock   clock.Clock
	Pools   trustport.TenantPools
	Pack    *trustpack.Pack
	Keys    *grants.Keys
	Begin   trustport.TxBeginner
	Facts   trustport.RecordFactSource
	Entropy io.Reader
	// PlanTextMaxBytes is the bound of every string a slot gives the Planner (plangraphs.Adapter.MaxTextBytes), from NOEMA_PLAN_TEXT_MAX_BYTES (A705): zero is the gap.
	PlanTextMaxBytes int
}

// decisionParts is what the lane built. A nil adapter is in Gaps.
type decisionParts struct {
	Intents  intake.Intents
	Recorder intake.Recorder
	Planner  intake.Planner
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
	planner, plannerMissing, err := decisionPlanner(in, intents)
	if err != nil {
		return decisionParts{}, err
	}
	p.Planner = planner
	add("Planner", plannerMissing)
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
	if p.Planner != nil {
		a.Planner = p.Planner
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
	if in.Pack == nil {
		missing = append(missing, needPack)
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
	si, err := trustport.NewStoredIntents(in.Pools, in.Pack.PackBytes(), timeout) // the Pack is the one source of the pack's bytes (A548, INV-16)
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: the admitted-intent store adapter: %w", err)
	}
	return si, nil, nil
}

// decisionPlanner builds the Planner (compose_planner.go) from the pack, the Intents adapter above and the tenant's Trust login with the same read bound; the text bound has no
// source, so with PlanTextMaxBytes zero it is the one gap that remains once the rest is present.
func decisionPlanner(in decisionInputs, intents intake.Intents) (intake.Planner, []string, error) {
	var docs slotDocuments
	if !absent(in.Pools) && in.Env.Getenv(EnvTrustDBTimeout) != "" {
		timeout, err := recorderSettingMS(in.Env, EnvTrustDBTimeout)
		if err != nil {
			return nil, nil, err
		}
		if docs, err = newPoolDocuments(in.Pools, timeout); err != nil {
			return nil, nil, fmt.Errorf("cmd/core: the Planner's document reader: %w", err)
		}
	}
	return buildPlanner(plannerInputs{Pack: in.Pack, Intents: intents, Docs: docs, MaxTextBytes: in.PlanTextMaxBytes})
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
	ring, keyMissing, err := decisionSigningRing(in.Env)
	if err != nil {
		return trustport.RecorderConfig{}, nil, err
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
	}{{in.Pack == nil, needPack}, {absent(in.Keys), needGrantKeys}, {absent(in.Begin), needRecordLogin}, {absent(in.Facts), needSealedFacts}} {
		if need.absent {
			missing = append(missing, need.text)
		}
	}
	missing = append(missing, keyMissing...)
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
	cfg.Begin, cfg.Facts, cfg.Keys, cfg.Clock, cfg.IDs, cfg.Entropy = in.Begin, in.Facts, in.Keys, in.Clock, ids.New(in.Clock, entropy), entropy
	cfg.Appender = &chain.Appender{Clock: in.Clock, IDs: cfg.IDs, Signers: map[string]chain.Signer{trustRecordIdentity: ring}}
	return cfg, nil, nil
}

// trustRecordIdentity is the Record identity of Trust (the emitter of the decision records): the database checks each record's signature under an ACTIVE key of the written_by
// identity (migrations 0012 and 0019).
const trustRecordIdentity = "core-trust"

// decisionSigningRing reads the core-trust Record signing keys (A548, interim, relayed, not yet committed) through the shared key loader, execution/signing.LoadRing, as the
// orchestrator's and the edge's keys are read. Both settings are required together: an absent one is named and gives no ring; a present one that is invalid is fatal. An error names
// the setting and never the value or the path.
func decisionSigningRing(env Env) (*signing.Ring, []string, error) {
	var missing []string
	dir, overlap := env.Getenv(EnvTrustKeyDir), env.Getenv(EnvTrustKeyMaxActive)
	if dir == "" {
		missing = append(missing, EnvTrustKeyDir)
	}
	if overlap == "" {
		missing = append(missing, EnvTrustKeyMaxActive)
	}
	maxActive := 0
	if overlap != "" {
		n, err := parseKeyOverlap(EnvTrustKeyMaxActive, overlap)
		if err != nil {
			return nil, nil, err
		}
		maxActive = n
	}
	if len(missing) > 0 {
		return nil, missing, nil
	}
	ring, err := signing.LoadRing(trustRecordIdentity, dir, signing.Options{MaxActive: maxActive})
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: %s: the signing keys cannot be loaded (core-trust.ed25519.<n>.key files: regular, mode 0400, owned by this user, no more than %s)", EnvTrustKeyDir, EnvTrustKeyMaxActive)
	}
	return ring, nil, nil
}
