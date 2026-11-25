// Package priorityrouting reads the pack's priority routing member: for each SLA phase the pack names, a priority and the owner queue the case goes to (PCK-09; ruling A644,
// interim, relayed, not yet committed).
//
// The member is pack data (SoT 51). The core holds no phase name, no queue name and no value: the pack names them (INV-14). This package refuses a member that is not exactly its
// shape, and hands out a read-only view that gives copies (A551). It reads no file, network or database; the bytes come from `platform/packload`, which has verified them against the
// manifest, so a pack that fails verification yields no bytes and therefore no table.
package priorityrouting

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
)

// Priority is the closed vocabulary of the member.
type Priority string

const (
	High   Priority = "HIGH"
	Normal Priority = "NORMAL"
	Low    Priority = "LOW"
)

// ErrTable is the refusal for a member that cannot be trusted. The text names the rule, never the content (AGENT-13).
var ErrTable = errors.New("priorityrouting: the member is not usable")

var (
	nameRe    = regexp.MustCompile(`\A[A-Z][A-Z0-9_]{0,63}\z`) // a phase name and an owner queue, as priority_routing.v1.json spells them
	tableIDRe = regexp.MustCompile(`\A[a-z][a-z0-9_]*(\.[a-z0-9_]+)*\z`)
	versionRe = regexp.MustCompile(`\A[1-9][0-9]{0,8}\z`)
)

const (
	label     = "SYNTHETIC" // every pack of the POC is synthetic (AGENT-25)
	maxPhases = 64
	maxNote   = 4000
)

// Entry is what a phase routes to. A phase the member does not list has no Entry: Lookup gives the zero Entry and false (A660).
type Entry struct {
	Priority   Priority
	OwnerQueue string
}

// Table is the verified, read-only view of the member. Its zero value and a nil *Table list no phase.
type Table struct {
	entries map[string]Entry
}

type entryDoc struct {
	Priority   *string `json:"priority"`
	OwnerQueue *string `json:"owner_queue"`
}

type tableDoc struct {
	TableID *string                    `json:"table_id"`
	Version json.RawMessage            `json:"version"`
	Label   *string                    `json:"label"`
	Note    *string                    `json:"note"`
	Phases  map[string]json.RawMessage `json:"phases"`
}

// Parse reads the member exactly as priority_routing.v1 has it, and refuses the whole of it otherwise: an unknown or repeated member (at any depth), another label than SYNTHETIC, a
// priority that is not exactly HIGH, NORMAL or LOW, an empty or malformed owner queue, a phase name off its shape, or no phase at all. It keeps nothing of the caller's bytes.
func Parse(raw []byte) (*Table, error) {
	bad := ErrTable
	if repeatsAMember(raw) {
		return nil, bad
	}
	var doc tableDoc
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, bad
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, bad
	}
	if doc.TableID == nil || !tableIDRe.MatchString(*doc.TableID) || len(*doc.TableID) < 3 || len(*doc.TableID) > 128 ||
		!versionRe.MatchString(string(doc.Version)) || doc.Label == nil || *doc.Label != label ||
		len(doc.Phases) == 0 || len(doc.Phases) > maxPhases {
		return nil, bad
	}
	if doc.Note != nil && (*doc.Note == "" || len(*doc.Note) > maxNote) {
		return nil, bad
	}
	t := &Table{entries: make(map[string]Entry, len(doc.Phases))}
	for phase, body := range doc.Phases {
		if !nameRe.MatchString(phase) {
			return nil, bad
		}
		var e entryDoc
		ed := json.NewDecoder(bytes.NewReader(body))
		ed.DisallowUnknownFields()
		if err := ed.Decode(&e); err != nil || e.Priority == nil || e.OwnerQueue == nil {
			return nil, bad
		}
		p := Priority(*e.Priority)
		if p != High && p != Normal && p != Low {
			return nil, bad
		}
		if !nameRe.MatchString(*e.OwnerQueue) {
			return nil, bad
		}
		t.entries[phase] = Entry{Priority: p, OwnerQueue: *e.OwnerQueue}
	}
	return t, nil
}

// Lookup gives what a phase routes to and true, or, for a phase the member does not list, the zero Entry and false (A660: fail-closed, no silent NORMAL; packcheck requires an
// entry for every clock status, so an unlisted phase at run time is a fault the caller must treat as such). The result is a copy.
func (t *Table) Lookup(phase string) (Entry, bool) {
	if t != nil {
		if e, ok := t.entries[phase]; ok {
			return e, true
		}
	}
	return Entry{}, false
}

// Phases lists the phases the member routes, sorted. It is a copy: changing it changes nothing.
func (t *Table) Phases() []string {
	if t == nil {
		return []string{}
	}
	out := make([]string, 0, len(t.entries))
	for p := range t.entries {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// repeatsAMember reports whether any object in the document names a member twice, at any depth (encoding/json keeps the last one and says nothing), or the document is not one value.
func repeatsAMember(raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if walk(dec) != nil {
		return true
	}
	_, err := dec.Token()
	return !errors.Is(err, io.EOF)
}

func walk(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := k.(string)
			if seen[key] {
				return ErrTable
			}
			seen[key] = true
			if err := walk(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := walk(dec); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token()
	return err
}
