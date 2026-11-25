package clocks

import (
	"fmt"
	"regexp"
)

// Routing of a clock status (TRU-06; PERD 23.5, A632, A640, A644, interim, relayed, not yet committed).
//
// The routing priority derives from the phase through the pack's priority routing member, which the composition root offers through PriorityLookup. Priority may be raised and never
// lowered (DOWNGRADE_CASE_PRIORITY is prohibited, PERD 23.5). A phase the member does not list is NORMAL with no queue, and an empty queue means no re-route. This code holds no phase
// queue, priority table or number of its own (INV-14, AGENT-16); it reads no clock and does no I/O (INV-21).

// Priority is the closed vocabulary of the pack's priority routing: LOW, NORMAL and HIGH, in that order.
type Priority string

// The three priorities, lowest first.
const (
	PriorityLow    Priority = "LOW"
	PriorityNormal Priority = "NORMAL"
	PriorityHigh   Priority = "HIGH"
)

// priorities is the vocabulary in rank order: a priority's index is its rank.
var priorities = []Priority{PriorityLow, PriorityNormal, PriorityHigh}

// queueRe is the shape of an owner queue as the pack's routing member spells it.
var queueRe = regexp.MustCompile(`\A[A-Z][A-Z0-9_]{0,63}\z`)

// Entry is what a phase routes to. An empty OwnerQueue means no re-route.
type Entry struct {
	Priority   Priority
	OwnerQueue string
}

// PriorityLookup is the consumer-side view of the pack's priority routing member: the entry of a phase, and false for a phase the member does not list. The adapter over the verified
// pack is built at composition.
type PriorityLookup interface {
	Lookup(phase string) (Entry, bool)
}

// Evaluation is a clock Status with the routing its phase gives.
type Evaluation struct {
	Status     Status
	Priority   Priority
	OwnerQueue string
	// Raised is true when the phase raised the priority above the case's current one. It is false when there was no current priority.
	Raised bool
}

func rank(p Priority) int {
	for i, q := range priorities {
		if p == q {
			return i
		}
	}
	return -1
}

func validPhase(p Phase) bool {
	return p == NotStarted || p == Running || p == AtRisk || p == Breached
}

// Prioritise derives the routing of a status. current is the case's priority now, or empty when it has none. The result's priority is the greater of current and the phase's;
// an unlisted phase is NORMAL with no queue. A nil lookup, a phase that is not one of the four, a priority outside the vocabulary (the case's or the table's) and a queue that is not of
// the pack's shape are ErrInvalid with the zero Evaluation: nothing is guessed (INV-16).
func Prioritise(st Status, current Priority, table PriorityLookup) (Evaluation, error) {
	if table == nil {
		return Evaluation{}, fmt.Errorf("%w: no priority routing", ErrInvalid)
	}
	if !validPhase(st.Phase) {
		return Evaluation{}, fmt.Errorf("%w: clock %s has phase %q", ErrInvalid, st.ClockID, st.Phase)
	}
	if current != "" && rank(current) < 0 {
		return Evaluation{}, fmt.Errorf("%w: the case's priority is not a priority", ErrInvalid)
	}
	e, listed := table.Lookup(string(st.Phase))
	if !listed {
		e = Entry{Priority: PriorityNormal}
	}
	if rank(e.Priority) < 0 {
		return Evaluation{}, fmt.Errorf("%w: the routing of phase %s names a priority that is not one", ErrInvalid, st.Phase)
	}
	if e.OwnerQueue != "" && !queueRe.MatchString(e.OwnerQueue) {
		return Evaluation{}, fmt.Errorf("%w: the routing of phase %s names a queue that is not of the pack's shape", ErrInvalid, st.Phase)
	}
	ev := Evaluation{Status: st, Priority: e.Priority, OwnerQueue: e.OwnerQueue}
	if current != "" && rank(current) >= rank(e.Priority) {
		ev.Priority = current
	} else if current != "" {
		ev.Raised = true
	}
	return ev, nil
}

// Reroute is true when the routing names an owner queue. An empty queue is no re-route.
func (e Evaluation) Reroute() bool { return e.OwnerQueue != "" }

// EventPayload is the payload of the SLA_EVALUATED Record event (PERD 23.5, 32.2: all fields): the clock, its label, the phase, the times in Unix seconds, the elapsed and remaining
// seconds, and the routing. Members with no value are left out: a clock with no anchor has no started_at, deadline_at or remaining (never assumed), and no queue is no owner_queue.
// Numbers are int64 and the rest strings and booleans, so the canonical form holds integers only (PERD 32.1).
func (e Evaluation) EventPayload() map[string]any {
	p := map[string]any{
		"clock_id":        string(e.Status.ClockID),
		"label":           string(e.Status.Label),
		"phase":           string(e.Status.Phase),
		"elapsed":         e.Status.Elapsed,
		"priority":        string(e.Priority),
		"priority_raised": e.Raised,
		"reroute":         e.Reroute(),
	}
	if e.Status.StartedAt != nil {
		p["started_at"] = *e.Status.StartedAt
	}
	if e.Status.DeadlineAt != nil {
		p["deadline_at"] = *e.Status.DeadlineAt
	}
	if e.Status.Remaining != nil {
		p["remaining"] = *e.Status.Remaining
	}
	if e.OwnerQueue != "" {
		p["owner_queue"] = e.OwnerQueue
	}
	return p
}
