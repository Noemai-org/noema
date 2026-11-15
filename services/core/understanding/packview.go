package understanding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrPack is returned by NewPackView for pack data that cannot give a sound view. The error names the rule, never the content of the pack.
var ErrPack = errors.New(prefix + "the pack data is not usable")

// The slot types the gate knows (intent-slot.v1).
var packSlotType = map[string]bool{"Resource": true, "Quantity": true, "Party": true, "TimeRef": true, "Reference": true}

type packFile struct {
	PackID      string `json:"pack_id"`
	PackVersion string `json:"pack_version"`
	IntentNodes []struct {
		NodeID      string   `json:"node_id"`
		Pack        string   `json:"pack"`
		PackVersion string   `json:"pack_version"`
		Outcomes    []string `json:"outcome_definitions"`
		Slots       []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"slots"`
	} `json:"intent_nodes"`
	OutcomeDefinitions []struct {
		OutcomeID string `json:"outcome_id"`
	} `json:"outcome_definitions"`
}

type packView struct {
	id, version string
	nodes       map[string]Node
}

func packErr(why string) error { return fmt.Errorf("%w: %s", ErrPack, why) }

// NewPackView reads the gate's view of a pack from the pack's data (ruling A166, item 4: the intent nodes come from the mounted pack, never from the core's code). It reads
// the pack id and version, each intent node's slots by name and type, and the outcome definitions each node names, and refuses data that cannot be trusted as the gate's
// source of truth: a repeated node, slot or outcome, a node that belongs to another pack or version, a slot of a type the gate does not know, and an outcome the pack does
// not define. It keeps nothing of the caller's bytes, and Node hands out a copy.
func NewPackView(raw []byte) (PackView, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var f packFile
	if err := dec.Decode(&f); err != nil {
		return nil, packErr("the pack is not a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, packErr("the pack has data after the object")
	}
	if !verticalRe.MatchString(f.PackID) {
		return nil, packErr("the pack id is off its shape")
	}
	if !semverRe.MatchString(f.PackVersion) {
		return nil, packErr("the pack version is off its shape")
	}
	if len(f.IntentNodes) == 0 {
		return nil, packErr("the pack has no intent nodes")
	}
	defined := map[string]bool{}
	for _, o := range f.OutcomeDefinitions {
		if o.OutcomeID == "" || defined[o.OutcomeID] {
			return nil, packErr("an outcome definition has no id or repeats one")
		}
		defined[o.OutcomeID] = true
	}
	v := &packView{id: f.PackID, version: f.PackVersion, nodes: map[string]Node{}}
	for _, n := range f.IntentNodes {
		if !packIDRe.MatchString(n.NodeID) {
			return nil, packErr("an intent node id is missing or off its shape")
		}
		if _, dup := v.nodes[n.NodeID]; dup {
			return nil, packErr("an intent node is repeated")
		}
		if n.Pack != f.PackID || n.PackVersion != f.PackVersion {
			return nil, packErr("an intent node belongs to another pack or version")
		}
		node := Node{Slots: map[string]string{}, Outcomes: map[string]bool{}}
		for _, s := range n.Slots {
			if !lowerSnakeRe.MatchString(s.Name) {
				return nil, packErr("a slot name is off its shape")
			}
			if _, dup := node.Slots[s.Name]; dup {
				return nil, packErr("a slot is repeated in a node")
			}
			if !packSlotType[s.Type] {
				return nil, packErr("a slot has a type the gate does not know")
			}
			node.Slots[s.Name] = s.Type
		}
		for _, o := range n.Outcomes {
			if !defined[o] {
				return nil, packErr("a node names an outcome the pack does not define")
			}
			if node.Outcomes[o] {
				return nil, packErr("an outcome is repeated in a node")
			}
			node.Outcomes[o] = true
		}
		v.nodes[n.NodeID] = node
	}
	return v, nil
}

func (v *packView) Ref() (string, string) { return v.id, v.version }

func (v *packView) Node(id string) (Node, bool) {
	n, ok := v.nodes[id]
	if !ok {
		return Node{}, false
	}
	out := Node{Slots: make(map[string]string, len(n.Slots)), Outcomes: make(map[string]bool, len(n.Outcomes))}
	for k, t := range n.Slots {
		out.Slots[k] = t
	}
	for k := range n.Outcomes {
		out.Outcomes[k] = true
	}
	return out, true
}
