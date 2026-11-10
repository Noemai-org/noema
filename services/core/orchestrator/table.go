package orchestrator

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed transitions.yaml
var transitionsYAML string

// Table is the transition table of PERD section 28.2, read from transitions.yaml (REQ-STM-001). Any arrow it does not list is illegal.
type Table struct {
	states []State
	known  map[State]bool
	exits  map[State][]State
	arrow  map[Transition]bool
	rows   map[string]bool // from, to and trigger: one arrow may be listed once per trigger (AWAITING_CONFIRMATION to EVALUATING: ACCEPT, EXPIRED)
}

var (
	versionLine = regexp.MustCompile(`^version: 1$`)
	sourceLine  = regexp.MustCompile(`^source: "PERD §28\.2"$`)
	statesLine  = regexp.MustCompile(`^states: \[([A-Z][A-Z_]*(?:, [A-Z][A-Z_]*)*)\]$`)
	rowLine     = regexp.MustCompile(`^  - \{from: ([A-Z][A-Z_]*), to: ([A-Z][A-Z_]*), on: "([^"]+)"\}$`)
)

// LoadTable reads the transitions.yaml embedded in the package. The file is restricted YAML, one flow mapping per line, read here without a YAML library; any
// line of another shape, an unknown state, a repeated state or a repeated row is an error, so a table that was edited into something this reader does not
// understand fails closed instead of allowing less or more than it says.
func LoadTable() (*Table, error) { return parseTable(transitionsYAML) }

func parseTable(src string) (*Table, error) {
	t := &Table{known: map[State]bool{}, exits: map[State][]State{}, arrow: map[Transition]bool{}, rows: map[string]bool{}}
	var sawVersion, sawSource, inRows bool
	for n, line := range strings.Split(src, "\n") {
		bad := func(why string) error { return fmt.Errorf("orchestrator: transitions.yaml line %d: %s", n+1, why) }
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case line == "transitions:":
			if len(t.states) == 0 {
				return nil, bad("transitions before states")
			}
			inRows = true
		case versionLine.MatchString(line):
			sawVersion = true
		case sourceLine.MatchString(line):
			sawSource = true
		case statesLine.MatchString(line):
			if len(t.states) != 0 || inRows {
				return nil, bad("states given twice or after transitions")
			}
			for _, s := range strings.Split(statesLine.FindStringSubmatch(line)[1], ", ") {
				if t.known[State(s)] {
					return nil, bad("state " + s + " repeated")
				}
				t.known[State(s)] = true
				t.states = append(t.states, State(s))
			}
		case inRows && rowLine.MatchString(line):
			m := rowLine.FindStringSubmatch(line)
			a := Transition{From: State(m[1]), To: State(m[2])}
			if !t.known[a.From] || !t.known[a.To] {
				return nil, bad("an arrow names a state that is not declared")
			}
			if t.rows[m[0]] {
				return nil, bad("row repeated")
			}
			t.rows[m[0]] = true
			if !t.arrow[a] {
				t.arrow[a] = true
				t.exits[a.From] = append(t.exits[a.From], a.To)
			}
		default:
			return nil, bad("not a line this reader understands")
		}
	}
	if !sawVersion || !sawSource || len(t.states) == 0 || len(t.arrow) == 0 {
		return nil, fmt.Errorf("orchestrator: transitions.yaml lacks its version, source, states or transitions")
	}
	return t, nil
}

// States are the states of the table, in file order.
func (t *Table) States() []State { return append([]State(nil), t.states...) }

// Allowed reports whether the arrow is listed. A state the table does not know is never allowed, as a source or as a target.
func (t *Table) Allowed(from, to State) bool { return t.arrow[Transition{From: from, To: to}] }

// Exits are the states one state may move to, in file order. A terminal state has none.
func (t *Table) Exits(from State) []State { return append([]State(nil), t.exits[from]...) }
