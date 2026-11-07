package orchestrator

import (
	"errors"
	"fmt"
)

// State is an investigation state of PERD section 28.2. The set of states is the one in transitions.yaml; a value outside it is never a source or a target.
type State string

// Transition is one arrow of the table.
type Transition struct{ From, To State }

// Investigation is the projection row that a command changes: its identity, its state and the version that optimistic locking compares (REQ-STM-001).
type Investigation struct {
	ID      string
	State   State
	Version int64
}

// Command asks for one transition of one investigation. ExpectedVersion is the version the caller read; it is not a hint.
type Command struct {
	To              State
	ExpectedVersion int64
}

// Change is what one applied command did. It is the content of the INVESTIGATION_STATE_CHANGED Record event, which the persistence slice appends in the same
// transaction that stores the new row.
type Change struct {
	ID                     string
	From, To               State
	FromVersion, ToVersion int64
}

// Guard decides whether a listed transition may be taken now. The guards for Trust, Execution and Outcome inputs (a grant, a verdict, a receipt) are supplied by
// the slices that own those inputs, through interfaces; a guard receives the investigation and the command as they were.
type Guard func(Investigation, Command) error

// Machine applies commands against a table. It holds no state of its own.
type Machine struct {
	Table  *Table
	Guards map[Transition]Guard
}

// ErrStaleVersion is returned when a command was made against another version than the investigation's own. The caller reads again.
var ErrStaleVersion = errors.New("orchestrator: stale state version")

// ErrGuardRefused wraps the error of a guard that refused a listed transition.
var ErrGuardRefused = errors.New("orchestrator: guard refused the transition")

// ErrNoTable is returned by a Machine that has no table: it refuses everything.
var ErrNoTable = errors.New("orchestrator: the machine has no transition table")

// IllegalTransitionError is returned for an arrow that is not in the table, an unknown state included. It is never applied; the caller raises the alert and S1
// triage of REQ-STM-001.
type IllegalTransitionError struct{ From, To State }

func (e *IllegalTransitionError) Error() string {
	return fmt.Sprintf("orchestrator: illegal transition from %q to %q", e.From, e.To)
}

// Apply applies one command and returns the investigation as it is afterwards and the change. On any error the investigation is returned as it came and the
// change is empty. Apply is a pure function of its arguments and the guards: persistence compares and swaps the version in the database (the next slice).
//
// The order is fixed: the version first (a stale caller may only look illegal), then the table (a guard never runs for an unlisted arrow, so a guard can refuse
// a transition but never allow one), then the guard.
func (m *Machine) Apply(inv Investigation, cmd Command) (Investigation, Change, error) {
	if m == nil || m.Table == nil {
		return inv, Change{}, ErrNoTable
	}
	if cmd.ExpectedVersion != inv.Version {
		return inv, Change{}, fmt.Errorf("%w: command made against version %d, investigation is at %d", ErrStaleVersion, cmd.ExpectedVersion, inv.Version)
	}
	if !m.Table.Allowed(inv.State, cmd.To) {
		return inv, Change{}, &IllegalTransitionError{From: inv.State, To: cmd.To}
	}
	if g := m.Guards[Transition{From: inv.State, To: cmd.To}]; g != nil {
		if err := g(inv, cmd); err != nil {
			return inv, Change{}, fmt.Errorf("%w: %w", ErrGuardRefused, err)
		}
	}
	next := Investigation{ID: inv.ID, State: cmd.To, Version: inv.Version + 1}
	return next, Change{ID: inv.ID, From: inv.State, To: cmd.To, FromVersion: inv.Version, ToVersion: next.Version}, nil
}
