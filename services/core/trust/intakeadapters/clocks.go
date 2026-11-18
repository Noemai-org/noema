package intakeadapters

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// phasesBySeverity is the phases of a clock, most severe first: the order in which the routing is read, so that a breached clock is routed before an at-risk one.
var phasesBySeverity = []clocks.Phase{clocks.Breached, clocks.AtRisk, clocks.Running, clocks.NotStarted}

// ClockBook is the clocks the pack declares and the intent nodes they apply to: a node's trust profile names its clocks, and the pack's `clocks` define them (SoT 15.3, PERD 23.5).
// A clock is data and carries its own label; the book holds none of its own.
type ClockBook struct {
	byNode map[string][]contracts.ClockV1 // intent node to the clocks that apply to it, in the profile's order; a node with none is present with an empty list
}

// LoadClockBook loads the clocks of a pack file, checked when it loads: each clock is a clock of the contract with a duration of at least one second and a label that is SYNTHETIC
// or VALIDATED, each clock, profile and node appears once, a profile names only defined clocks, each once, and a node names a defined profile or none. Anything else is ErrPack
// and no book: the adapters never guess which clock applies.
func LoadClockBook(pack []byte) (*ClockBook, error) {
	top, err := topLevel(pack, "clocks", "trust_profiles", "intent_nodes")
	if err != nil {
		return nil, err
	}
	var defs []contracts.ClockV1
	var profiles []struct {
		ID     string   `json:"profile_id"`
		Clocks []string `json:"clocks"`
	}
	var nodes []struct {
		ID           string  `json:"node_id"`
		TrustProfile *string `json:"trust_profile"`
	}
	for key, into := range map[string]any{"clocks": &defs, "trust_profiles": &profiles, "intent_nodes": &nodes} {
		if err := json.Unmarshal(top[key], into); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrPack, key, err)
		}
	}
	clockByID := map[string]contracts.ClockV1{}
	for _, c := range defs {
		if _, dup := clockByID[string(c.ClockId)]; dup || c.ClockId == "" || c.AnchorEvent == "" || c.DurationSeconds < 1 ||
			(c.Label != contracts.ClockV1LabelSYNTHETIC && c.Label != contracts.ClockV1LabelVALIDATED) {
			return nil, fmt.Errorf("%w: the clock %q is defined twice or is not usable", ErrPack, c.ClockId)
		}
		clockByID[string(c.ClockId)] = c
	}
	clocksOfProfile := map[string][]contracts.ClockV1{}
	for _, p := range profiles {
		if _, dup := clocksOfProfile[p.ID]; dup || p.ID == "" {
			return nil, fmt.Errorf("%w: the trust profile %q is empty or defined twice", ErrPack, p.ID)
		}
		list := []contracts.ClockV1{}
		seen := map[string]bool{}
		for _, id := range p.Clocks {
			c, ok := clockByID[id]
			if !ok || seen[id] {
				return nil, fmt.Errorf("%w: the trust profile %s names the clock %q, which is not defined or is named twice", ErrPack, p.ID, id)
			}
			seen[id] = true
			list = append(list, c)
		}
		clocksOfProfile[p.ID] = list
	}
	book := &ClockBook{byNode: map[string][]contracts.ClockV1{}}
	for _, n := range nodes {
		if _, dup := book.byNode[n.ID]; dup || n.ID == "" {
			return nil, fmt.Errorf("%w: the intent node %q is empty or defined twice", ErrPack, n.ID)
		}
		book.byNode[n.ID] = []contracts.ClockV1{}
		if n.TrustProfile == nil {
			continue
		}
		list, ok := clocksOfProfile[*n.TrustProfile]
		if !ok {
			return nil, fmt.Errorf("%w: the node %s names the trust profile %q, which is not defined", ErrPack, n.ID, *n.TrustProfile)
		}
		book.byNode[n.ID] = list
	}
	return book, nil
}

// Anchors gives the time of a clock's anchor event for a sealed snapshot, in Unix seconds; happened is false when the event has not happened (NOT_STARTED, never assumed). A source
// that cannot tell returns an error and never "not happened". There is no production implementation yet: the anchor events (for example a complaint being logged) are on the
// snapshot's case-system evidence and on the Record, and nothing reads them for Trust (see status/TRU-adapters-policies-clocks.md).
type Anchors interface {
	AnchorTime(ctx context.Context, tenantID, snapshotID, anchorEvent string) (at int64, happened bool, err error)
}

// ClocksConfig is what NewClocks needs. Every part but Routes is required; there are no defaults.
type ClocksConfig struct {
	Book    *ClockBook
	Anchors Anchors
	// AtRisk is the at-risk fraction (ruling A205 Q1: provisional pack data; Trust holds no number). It is refused when it is not a fraction from zero to one and is never defaulted.
	AtRisk clocks.Ratio
	// Routes is the pack's routing of a clock phase to an escalation: phase to the pack reason code (`<vertical>.reason.<name>`) the escalation carries. A phase not in it routes
	// nowhere. The pack has no such data yet (see the status file): the composition root passes what the pack gives once it does, and nothing until then.
	Routes map[clocks.Phase]string
}

// Clocks implements intake.Clocks.
type Clocks struct{ c ClocksConfig }

var _ intake.Clocks = (*Clocks)(nil)

// NewClocks builds the adapter. A missing part, an unusable fraction, a route for a phase that is not one, or a route whose reason code is not a pack reason code is ErrConfig. The
// routes are copied.
func NewClocks(c ClocksConfig) (*Clocks, error) {
	if c.Book == nil || c.Anchors == nil || c.AtRisk.Den <= 0 || c.AtRisk.Num < 0 || c.AtRisk.Num > c.AtRisk.Den {
		return nil, ErrConfig
	}
	routes := make(map[clocks.Phase]string, len(c.Routes))
	for phase, code := range c.Routes {
		vertical, _, _ := strings.Cut(code, ".")
		if _, err := decide.Pack(vertical, code); err != nil || !slices.Contains(phasesBySeverity, phase) {
			return nil, ErrConfig
		}
		routes[phase] = code
	}
	c.Routes = routes
	return &Clocks{c: c}, nil
}

func (c *Clocks) usable(q intake.ClockQuery) error {
	if c == nil || c.c.Book == nil {
		return ErrConfig
	}
	if q.TenantID == "" || q.IntentNode == "" {
		return fmt.Errorf("%w: a tenant and an intent node are required", ErrQuery)
	}
	return nil
}

// Applicable answers the clocks that apply to the intent node, in the order of the node's trust profile, each with the time of its anchor event (nil when it has not happened), and
// the at-risk fraction of the configuration. A node the pack does not know is an error, not "no clock"; a node with no clock needs no snapshot and gets an empty set. An anchor the
// source cannot give, or gives as a time that is not one, is an error and no set.
func (c *Clocks) Applicable(ctx context.Context, q intake.ClockQuery) (intake.ClockSet, error) {
	if err := c.usable(q); err != nil {
		return intake.ClockSet{}, err
	}
	list, known := c.c.Book.byNode[q.IntentNode]
	if !known {
		return intake.ClockSet{}, fmt.Errorf("%w: the pack does not know the intent node", ErrQuery)
	}
	set := intake.ClockSet{AtRisk: c.c.AtRisk}
	if len(list) == 0 {
		return set, nil
	}
	if q.SnapshotID == "" {
		return intake.ClockSet{}, fmt.Errorf("%w: a snapshot is required to read an anchor", ErrQuery)
	}
	for _, clk := range list {
		at, happened, err := c.c.Anchors.AnchorTime(ctx, q.TenantID, q.SnapshotID, clk.AnchorEvent)
		if err != nil {
			return intake.ClockSet{}, fmt.Errorf("intakeadapters: the anchor of %s: %w", clk.ClockId, err)
		}
		inst := intake.ClockInstance{Clock: clk}
		if happened {
			if at < 1 {
				return intake.ClockSet{}, fmt.Errorf("%w: %s", ErrAnchor, clk.ClockId)
			}
			inst.Anchor = &at
		}
		set.Clocks = append(set.Clocks, inst)
	}
	return set, nil
}

// Route answers whether the pack's routing escalates for the statuses: the most severe phase among them that the routes name, with that route's reason code. A phase the routes do
// not name does not escalate, and a breached clock the pack does not route is not escalated here: the intake escalates it itself, with its own core code (A225). A phase that is none of
// the four is an error.
func (c *Clocks) Route(_ context.Context, q intake.ClockQuery, statuses []clocks.Status) (intake.ClockRouting, error) {
	if err := c.usable(q); err != nil {
		return intake.ClockRouting{}, err
	}
	for _, st := range statuses {
		if !slices.Contains(phasesBySeverity, st.Phase) {
			return intake.ClockRouting{}, fmt.Errorf("%w: %q is not a clock phase", ErrQuery, st.Phase)
		}
	}
	for _, phase := range phasesBySeverity {
		reason, routed := c.c.Routes[phase]
		if !routed {
			continue
		}
		for _, st := range statuses {
			if st.Phase == phase {
				return intake.ClockRouting{Escalate: true, ReasonCode: reason}, nil
			}
		}
	}
	return intake.ClockRouting{}, nil
}
