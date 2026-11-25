package main

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

// compose_clocks.go builds the Clocks input (T12) of the Trust intake and checks the pack's trust limits at start-up (STM-01; A612, A684 step 1, A544, A447, A345 S3; interim, relayed, not
// yet committed; REQ-STM-001, REQ-TRU-006, REQ-OPS-003; INV-14, INV-16, INV-19, INV-21; AGENT-13, AGENT-16). The limits are read from the bytes the manifest pinned (packload.Pack.Limits)
// through trustlimits.LoadPackFile, which accepts exactly trust-limits.v2. A refusal is an error and the process does not start; there is no default and no setting (A330). The core names
// no pack file: the one pinned file that is exactly trust-limits.v2 is the limits file, and zero or several of them is a refusal. The at-risk fraction, the anchor mapping and the phase
// routes are those of that file; the clocks are the pack's. Anchors are read over the sealed snapshot's lists, which need the list convention, the payload source and the injection
// screen: without any of them Clocks is a named gap, as Policies is.

// trustLimitsOf loads the pack's trust limits. wantPackID is the pack's own id, never the limits file's. The error text is fixed: no path, no value.
func trustLimitsOf(v packload.Pack) (trustlimits.Limits, contracts.TrustLimitsV2, error) {
	var head struct {
		PackID string `json:"pack_id"`
	}
	if err := json.Unmarshal(v.Pack, &head); err != nil || head.PackID == "" {
		return trustlimits.Limits{}, contracts.TrustLimitsV2{}, inputsFail("the pack's id cannot be read to check its trust limits", err)
	}
	var found trustlimits.Limits
	var foundPath string
	var lastErr error
	count := 0
	for _, path := range sortedKeys(v.Limits) {
		l, err := trustlimits.LoadPackFile(v.Limits, path, head.PackID)
		if err != nil {
			lastErr = err
			continue
		}
		found, foundPath = l, path
		count++
	}
	if count != 1 {
		return trustlimits.Limits{}, contracts.TrustLimitsV2{}, inputsFail("the pack pins no single file that is exactly trust-limits.v2", lastErr)
	}
	var doc contracts.TrustLimitsV2
	if err := doc.UnmarshalJSON(v.Limits[foundPath]); err != nil {
		return trustlimits.Limits{}, contracts.TrustLimitsV2{}, inputsFail("the pack's trust limits cannot be read", err)
	}
	return found, doc, nil
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// clocksParts is what the adapter is made of.
type clocksParts struct {
	Pack  []byte
	Limit trustlimits.Limits
	Doc   contracts.TrustLimitsV2
	Lists intakesources.EvidenceLists
}

// newClocksAdapter makes the Clocks adapter: the pack's clock book, the at-risk fraction of the limits, the anchors over the lists with the limits' mapping, and the limits' phase routes
// (none: nothing is routed, and the intake escalates a breached clock itself, A225).
func newClocksAdapter(p clocksParts) (intake.Clocks, error) {
	book, err := intakeadapters.LoadClockBook(p.Pack)
	if err != nil {
		return nil, inputsFail("the pack's clocks are not usable", err)
	}
	atRisk, err := intakesources.AtRiskFromLimits(p.Limit)
	if err != nil {
		return nil, inputsFail("the at-risk fraction of the pack's limits is not usable", err)
	}
	events := map[string]intakesources.AnchorEvent{}
	if p.Doc.AnchorEntities != nil {
		for _, e := range *p.Doc.AnchorEntities {
			events[e.Event] = intakesources.AnchorEvent{Entity: e.Entity, Field: e.Field}
		}
	}
	anchors, err := intakesources.NewAnchors(p.Lists, events)
	if err != nil {
		return nil, inputsFail("the clock anchors cannot be read from the lists", err)
	}
	routes := map[clocks.Phase]string{}
	if p.Doc.PhaseRoutes != nil {
		for _, r := range *p.Doc.PhaseRoutes {
			routes[clocks.Phase(r.Phase)] = r.ReasonCode
		}
	}
	c, err := intakeadapters.NewClocks(intakeadapters.ClocksConfig{Book: book, Anchors: anchors, AtRisk: atRisk, Routes: routes})
	if err != nil {
		return nil, inputsFail("the Clocks adapter cannot be built", err)
	}
	return c, nil
}

// buildClocksInput builds T12. The limits are checked first, so that a bad file is an error whatever else is missing (A612); without the verified pack, the anchor mapping, the list
// convention, the mapping, the payload source or the screen it is a gap. An anchor entity that the lists do not serve is an error: the clocks could never be read.
func buildClocksInput(in trustInputsIn) (intake.Clocks, error) {
	if len(in.Verified.Pack) == 0 {
		return nil, nil
	}
	limits, doc, err := trustLimitsOf(in.Verified)
	if err != nil {
		return nil, err
	}
	if doc.AnchorEntities == nil || in.Mapping == nil || len(in.ListEntities) == 0 || !in.screened() {
		return nil, nil
	}
	pools, err := in.pools()
	if err != nil {
		return nil, err
	}
	items, err := intakesources.NewSnapshotItems(queryLogins{pools}, in.Timeout)
	if err != nil {
		return nil, inputsFail("the snapshot item reader cannot be built", err)
	}
	return newClocksAdapter(clocksParts{Pack: in.Verified.Pack, Limit: limits, Doc: doc, Lists: intakesources.EvidenceLists{
		Mapping: in.Mapping, Items: items, Payloads: in.Payloads, Entities: in.ListEntities,
		Screen: in.Screen.Screen, Correlation: in.Screen.Correlation, Alert: in.Screen.Alert,
	}})
}

// checkPackLimits is the start-up refusal of A612: when the pack settings are all given it loads the pack through its manifest and requires its trust limits to load. With none, or some, of
// the settings there is no pack to check (the named gaps say so, A469, A486). The error never carries a path or a value.
func checkPackLimits(env Env) error {
	if env.Getenv == nil {
		return nil
	}
	dir, manifest, bound := env.Getenv(EnvPackDir), env.Getenv(EnvPackManifest), env.Getenv(EnvPackMaxBytes)
	if dir == "" || manifest == "" || bound == "" {
		return nil
	}
	maxBytes, err := packBound(env, bound)
	if err != nil {
		return err
	}
	v, err := packload.Load(dir, manifest, maxBytes)
	if err != nil {
		return fmt.Errorf("cmd/core: %s: the pack cannot be loaded: %w", EnvPackDir, err)
	}
	_, _, err = trustLimitsOf(v)
	return err
}
