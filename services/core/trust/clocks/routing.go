package clocks

// Routing of a clock status (TRU-06; PERD 23.5, A632, A640, A644, interim, relayed, not yet committed). STUB for the red commit: nothing is answered.

// Priority is the closed vocabulary of the pack's priority routing.
type Priority string

// Entry is what a phase routes to: a priority and the owner queue. An empty OwnerQueue means no re-route.
type Entry struct {
	Priority   Priority
	OwnerQueue string
}

// PriorityLookup is the consumer-side view of the pack's priority routing member.
type PriorityLookup interface {
	Lookup(phase string) (Entry, bool)
}

// Evaluation is a Status with the routing its phase gives.
type Evaluation struct {
	Status     Status
	Priority   Priority
	OwnerQueue string
	Raised     bool
}

// Prioritise is not built yet.
func Prioritise(Status, Priority, PriorityLookup) (Evaluation, error) { return Evaluation{}, nil }

// Reroute is not built yet.
func (Evaluation) Reroute() bool { return true }

// EventPayload is not built yet.
func (Evaluation) EventPayload() map[string]any { return nil }
