package main

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/plangraphs"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// compose_trust_wiring.go is the part of the final composition that does not need the events route (STM-01; A643, A550, A551, A612, A512, interim, relayed, not yet committed; REQ-STM-001,
// REQ-TRU-009, REQ-INT-001; INV-12, INV-14, INV-16; AGENT-13, AGENT-16). It builds the Trust inputs of lane 2 (buildTrustInputs) and the decisions of lane 3 (buildDecisions) over the ONE
// manifest-verified pack and the ONE set of Trust logins the composition opens, and says what is still missing. A part whose input is absent is a named gap with fixed text and no value
// (A469, A486); a part whose input is present and invalid stops the process. No setting is added: it reads the settings the lanes already name.

// The fixed texts of the gaps that this file names itself (the decisions lane's are in wire_decisions.go). They say what an adapter lacks, never a value.
const (
	needWriteRule       = "the pack's write rule over the stored intent's slot values (not on main)"
	needCandidatesRead  = "the principal-scoped candidates read (evidence/candidates_api is empty)"
	needLabeller        = "the candidate labeller over the pack's disambiguation data (not built)"
	needScopedRead      = "the principal-scoped enterprise read (the candidates read)"
	needEvidenceMapping = "the evidence mapping and the rule set of the Understanding half"
	needScreen          = "the injection screen of the payload reader (screen, correlation source, alert)"
	needListEntities    = "the list entities of the evidence mapping (none is listed, or the pack and the mapping are absent)"
	needReconcilerData  = "the pack's escalation reason of a classification and the capabilities allowed under a freeze"
	needClockRoutes     = "the clock routes and the anchor events of the pack"
	// needTrustLimits is the Clocks gap's reason. The limits file (A612, interim, relayed, not yet committed) holds numbers the Clocks adapter needs, and its loader is noema#910, which is
	// not on main. Until it merges Clocks is a named gap. When the loader lands, a refused result must stop the start or leave Clocks a named gap that FAILS CLOSED: never a default and
	// never a number written here (AGENT-16). TestTheLimitsFileIsANamedGapThatFailsClosedAndNeverADefault pins this comment.
	needTrustLimits = "the Trust limits file (trust_limits.json: no loader on main)"
)

// wiredAdapters are the TrustAdapters fields this wiring is responsible for, in the order of the struct.
var wiredAdapters = []string{"Intents", "Writes", "Candidates", "Ownership", "Evidence", "Reconciler", "Policies", "Clocks", "Confirmations", "Planner", "Recorder"}

// trustWiringIn is everything the composition hands to the wiring. Pools is the one set of per-tenant Trust logins (trust_rw), opened once by the composition and shared with Trust
// itself. The parts production does not have yet (the enterprise read, the injection screen, the list convention, the Record write login, the sealed-facts source) are left zero, and the
// adapters that need them are named gaps; a test passes SYNTHETIC ones through the same fields.
type trustWiringIn struct {
	Env     Env
	Clock   clock.Clock
	Pack    *trustpack.Pack
	Tenants []string
	Pools   map[string]*pgxpool.Pool
	Keys    *grants.Keys
	// Understanding is what buildUnderstanding made (the rule set and the one evidence mapping, A333): the Reconciler's own, never a second mapping.
	Understanding   UnderstandingParts
	OwnershipReader ownershipport.ScopedReader
	Screen          ScreenDeps
	ListEntities    map[string]intakesources.ListSpec
	Begin           trustport.TxBeginner
	Facts           trustport.RecordFactSource
}

// trustWired is what the wiring built and why the rest is not.
type trustWired struct {
	Inputs    trustInputs
	Decisions decisionParts
	// Reasons is, for each adapter of wiredAdapters that is not built, the fixed texts of what it lacks.
	Reasons map[string][]string
}

// givenSetting reports whether a setting is present; a present one is read by read, and an invalid one is an error that names the setting and never the value (A469).
func givenSetting[T any](env Env, name string, read func(Env, string) (T, error)) (v T, given bool, err error) {
	if env.Getenv == nil || env.Getenv(name) == "" {
		return v, false, nil
	}
	v, err = read(env, name)
	return v, err == nil, err
}

func wholeBytes(env Env, name string) (int64, error) { return wholeNumber(env, name, " of bytes") }

// wireTrustInputs builds the Trust inputs and the decisions from what the composition has. It dials nothing. An absent input leaves its adapter nil and named in Reasons; an invalid one
// is an error whose text names the setting or the part and carries no value, tenant or word of the pack.
func wireTrustInputs(in trustWiringIn) (trustWired, error) {
	timeout, timeoutGiven, err := givenSetting(in.Env, EnvTrustDBTimeout, recorderSettingMS)
	if err != nil {
		return trustWired{}, err
	}
	bound, boundGiven, err := givenSetting(in.Env, EnvPayloadMaxBytes, wholeBytes)
	if err != nil {
		return trustWired{}, err
	}
	lists := in.ListEntities
	if lists == nil && in.Pack != nil && in.Understanding.Mapping != nil {
		if lists, err = listEntitiesFromDescriptors(in.Pack.PackBytes(), in.Understanding.Mapping); err != nil { // A545: from the mapping and the pack's descriptors
			return trustWired{}, err
		}
	}
	ti := trustInputsIn{
		Pack: in.Pack, Tenants: in.Tenants, Pools: in.Pools, Timeout: timeout, Clock: in.Clock, RuleSet: in.Understanding.RuleSet, Mapping: in.Understanding.Mapping,
		Screen: in.Screen, ListEntities: lists, OwnershipReader: in.OwnershipReader,
	}
	if in.Pack != nil {
		ti.Verified = packload.Pack{Pack: in.Pack.PackBytes()} // the one source of the pack's bytes (A548, INV-16)
	}
	if timeoutGiven && boundGiven && len(in.Pools) > 0 {
		ti.Payloads = &trustPayloads{pools: in.Pools, maxBytes: bound} // over the shared logins: no second set
	}
	inputs, err := buildTrustInputs(ti)
	if err != nil {
		return trustWired{}, err
	}
	di, err := decisionInputsFrom(in)
	if err != nil {
		return trustWired{}, err
	}
	decisions, err := buildDecisions(di)
	if err != nil {
		return trustWired{}, err
	}
	w := trustWired{Inputs: inputs, Decisions: decisions, Reasons: map[string][]string{}}
	w.reasons(in, timeoutGiven, boundGiven, len(lists) > 0)
	return w, nil
}

// reasons fills Reasons for every adapter that is still unbuilt.
func (w *trustWired) reasons(in trustWiringIn, timeoutGiven, boundGiven, haveLists bool) {
	for _, g := range w.Decisions.Gaps {
		if slices.Contains(wiredAdapters, g.Adapter) {
			w.Reasons[g.Adapter] = slices.Clone(g.Missing)
		}
	}
	unbuilt := w.Unbuilt()
	add := func(adapter string, texts ...string) {
		if slices.Contains(unbuilt, adapter) {
			w.Reasons[adapter] = append(w.Reasons[adapter], texts...)
		}
	}
	var read []string // what the two database-backed inputs share
	if in.Pack == nil {
		read = append(read, needPack)
	}
	if in.Understanding.Mapping == nil || in.Understanding.RuleSet == nil {
		read = append(read, needEvidenceMapping)
	}
	if !screenGiven(in.Screen) {
		read = append(read, needScreen)
	}
	if len(in.Pools) == 0 {
		read = append(read, needTrustLogins)
	}
	if !timeoutGiven {
		read = append(read, EnvTrustDBTimeout)
	}
	if !boundGiven {
		read = append(read, EnvPayloadMaxBytes)
	}
	add("Writes", needWriteRule)
	add("Candidates", needCandidatesRead, needLabeller)
	add("Ownership", needScopedRead)
	add("Evidence", read...)
	add("Reconciler", needReconcilerData)
	policies := slices.Clone(read)
	if !haveLists {
		policies = append(policies, needListEntities)
	}
	add("Policies", policies...)
	add("Clocks", needTrustLimits, needClockRoutes)
}

func screenGiven(d ScreenDeps) bool {
	return !isNilValue(d.Screen) && !isNilValue(d.Alert) && d.Correlation != nil
}

// fill sets every adapter of the wiring that was built on a and leaves every other field as it was.
func (w trustWired) fill(a TrustAdapters) TrustAdapters {
	return w.Decisions.fill(w.Inputs.fill(a))
}

// Unbuilt names, in the order of TrustAdapters, the adapters of the wiring that are not built.
func (w trustWired) Unbuilt() []string {
	var out []string
	for _, n := range unsetAdapters(w.fill(TrustAdapters{})) {
		if slices.Contains(wiredAdapters, n) {
			out = append(out, n)
		}
	}
	return out
}

// memoPackSource makes the pack source answer once: the pack is loaded and verified one time for the whole composition, and every caller gets that one answer, a failure included (A548,
// INV-16). A second read of the directory could give another pack than the one the Trust adapters were built from.
func memoPackSource(src PackSource) PackSource {
	var once sync.Once
	var pack *trustpack.Pack
	var err error
	return func(env Env) (*trustpack.Pack, error) {
		once.Do(func() { pack, err = src(env) })
		return pack, err
	}
}

// stillUnbuilt keeps the names of an UnbuiltAdaptersError that are not yet built: a name that is a field of TrustAdapters stays only while the field is unset; any other name (the pack's
// own gaps) stays.
func stillUnbuilt(names []string, a TrustAdapters) []string {
	unset := unsetAdapters(a)
	fields := unsetAdapters(TrustAdapters{})
	var out []string
	for _, n := range names {
		if !slices.Contains(fields, n) || slices.Contains(unset, n) {
			out = append(out, n)
		}
	}
	return out
}

// planContextsFromRequest is plangraphs.PlanContexts over the fields of the intake's PlanRequest (A550): the capabilities to plan now, the winning PERMIT rule's params as T11 read them,
// the facts a continuation learned from verified results and the evidence packet, copied and nothing else. It reads no store and no pack.
type planContextsFromRequest struct{}

var _ plangraphs.PlanContexts = planContextsFromRequest{}

func (planContextsFromRequest) PlanContext(ctx context.Context, r intake.PlanRequest) (plangraphs.PlanContext, error) {
	if err := ctx.Err(); err != nil {
		return plangraphs.PlanContext{}, err
	}
	pc := plangraphs.PlanContext{Runnable: slices.Clone(r.Capabilities), Facts: maps.Clone(r.Facts), EvidencePacket: r.EvidencePacket}
	if r.PolicyParams != nil {
		pc.PolicyParams = make(map[string]map[string]string, len(r.PolicyParams))
		for capability, params := range r.PolicyParams {
			pc.PolicyParams[capability] = maps.Clone(params)
		}
	}
	return pc, nil
}

// decisionInputsFrom is what the decisions lane consumes from the wiring: the process inputs, the shared Trust logins and the pack's confirmation TTL ceiling (A561, A724).
func decisionInputsFrom(in trustWiringIn) (decisionInputs, error) {
	di := decisionInputs{Env: in.Env, Clock: in.Clock, Pack: in.Pack, Keys: in.Keys, Begin: in.Begin, Facts: in.Facts}
	if len(in.Pools) > 0 {
		di.Pools = trustport.PoolsOf(in.Pools)
	}
	ceiling, err := confirmationTTLCeiling(in.Pack)
	if err != nil {
		return decisionInputs{}, err
	}
	di.ConfirmationTTLCeiling = ceiling
	return di, nil
}

// confirmationTTLCeiling is the pack's `confirmation_ttl_ceiling_ms` as a duration, read from the verified trust limits the pack kept (trustlimits, A612; no number of this package, AGENT-16).
// A pack that kept no limits files, and no pack, give zero: the Writer then writes no CONFIRM request (fail closed) and the Confirmations gap stays named. Limits that are kept and do
// not load are an error, as the Clocks adapter's are.
func confirmationTTLCeiling(p *trustpack.Pack) (time.Duration, error) {
	if p == nil || len(p.LimitsFiles()) == 0 {
		return 0, nil
	}
	limits, _, err := trustLimitsOf(packload.Pack{Pack: p.PackBytes(), Limits: p.LimitsFiles()})
	if err != nil {
		return 0, err
	}
	ms, err := limits.ConfirmationTTLCeilingMS()
	if err != nil {
		return 0, inputsFail("the pack's confirmation TTL ceiling cannot be read", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}
