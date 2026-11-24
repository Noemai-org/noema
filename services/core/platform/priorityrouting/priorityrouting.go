// Package priorityrouting reads the pack's priority routing member: for each SLA phase the pack names, a priority and the owner queue the case goes to (PCK-09; ruling A644,
// interim, relayed, not yet committed).
package priorityrouting

import "errors"

// Priority is the closed vocabulary of the member.
type Priority string

const (
	High   Priority = "HIGH"
	Normal Priority = "NORMAL"
	Low    Priority = "LOW"
)

// ErrTable is the refusal for a member that cannot be trusted. The text names the rule, never the content.
var ErrTable = errors.New("priorityrouting: the member is not usable")

// Entry is what a phase routes to.
type Entry struct {
	Priority   Priority
	OwnerQueue string
}

// Table is the verified, read-only view of the member.
type Table struct{}

// Parse is not built yet.
func Parse(raw []byte) (*Table, error) { return nil, errors.New("not built") }

// Lookup is not built yet.
func (t *Table) Lookup(phase string) (Entry, bool) { return Entry{}, false }

// Phases is not built yet.
func (t *Table) Phases() []string { return nil }
