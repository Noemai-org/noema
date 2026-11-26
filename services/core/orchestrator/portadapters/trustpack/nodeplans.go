package trustpack

// PlanSlot is one slot declaration of an intent node, as the pack file writes it: the node's slot is the single authority of a slot's type and resource kind (A454, interim, relayed).
// ResourceKind is "" when the slot declares none.
type PlanSlot struct {
	Name         string
	Type         string
	ResourceKind string
}

// PlanParam is one declared param of an action graph node, as the pack file writes it. A member the file does not write is its zero value; the package does not decide which source
// the param has (a param with none, or with more than one, is for the reader to refuse). ConstJSON is the JSON text of the constant. HasMap is true when the file writes a map param,
// whose slot is MapFrom and whose values are MapValues.
type PlanParam struct {
	Name           string
	Slot           string
	ConstJSON      string
	HasMap         bool
	MapFrom        string
	MapValues      map[string]string
	Fact           string
	PolicyParam    string
	EvidencePacket bool
}

// PlanStep is one node of an action graph with what the plan needs of it, in the graph file's order.
type PlanStep struct {
	NodeID         string
	CapabilityID   string
	Preconditions  []string
	Postconditions []string
	Params         []PlanParam
}

// NodePlan is the action graph an intent node names, with the node's slot declarations: what the Planner reads (A551, A684).
type NodePlan struct {
	GraphID      string
	GraphVersion string
	Slots        []PlanSlot
	Steps        []PlanStep
}

// NodePlans is the stub of the red commit: it answers nothing.
func (p *Pack) NodePlans() (map[string]NodePlan, error) {
	return nil, ErrConfig
}
