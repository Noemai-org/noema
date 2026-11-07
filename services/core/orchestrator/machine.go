package orchestrator

import (
	"errors"
)

var errNotBuilt = errors.New("orchestrator: not built")

// State is an investigation state of PERD section 28.2.
type State string

// Transition is one arrow of the table.
type Transition struct{ From, To State }

// Table is the transition table loaded from transitions.yaml.
type Table struct{}

// LoadTable reads the embedded transitions.yaml.
func LoadTable() (*Table, error) { return nil, errNotBuilt }

// States are the states of the table, in file order.
func (t *Table) States() []State { return nil }

// Allowed reports whether the arrow is listed.
func (t *Table) Allowed(from, to State) bool { return false }

// Exits are the states reachable from one state.
func (t *Table) Exits(from State) []State { return nil }

// Investigation is the projection row that a command changes.
type Investigation struct {
	ID      string
	State   State
	Version int64
}

// Command asks for one transition of one investigation.
type Command struct {
	To              State
	ExpectedVersion int64
}

// Change is what one applied command did.
type Change struct {
	ID                     string
	From, To               State
	FromVersion, ToVersion int64
}

// Guard decides whether a listed transition may be taken now.
type Guard func(Investigation, Command) error

// Machine applies commands.
type Machine struct {
	Table  *Table
	Guards map[Transition]Guard
}

// ErrStaleVersion is returned when the command was made against an older version.
var ErrStaleVersion = errors.New("orchestrator: stale state version")

// ErrGuardRefused wraps the error of a guard that refused a listed transition.
var ErrGuardRefused = errors.New("orchestrator: guard refused the transition")

// IllegalTransitionError is returned for an arrow that is not in the table.
type IllegalTransitionError struct{ From, To State }

func (e *IllegalTransitionError) Error() string { return "orchestrator: illegal transition" }

// Apply applies one command.
func (m *Machine) Apply(inv Investigation, cmd Command) (Investigation, Change, error) {
	return inv, Change{}, errNotBuilt
}
