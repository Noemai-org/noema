package mapping

// The target version declaration of the evidence mapping, version 3 (EXP-01; rulings A550, A556, A665, A706, A710; REQ-RES-001, REQ-CTX-001; INV-17, INV-19, INV-21).
//
// A write that is conditional on the version of its target (INV-27) needs the version token the evidence was read at. The token is in the stored payload of the source that was read
// (A706: the maximum entry version, as a string), and so is the marker that says the read answered. Which source carries them, and under which payload members, is pack data, like the rest
// of the mapping (INV-14): `target_versions` is an optional list of `{source, token, complete}`. The mapping reads a token and decides nothing about a write. A payload that is not marked
// complete gives no token (the marker is looked at first), and a payload with no token, or a token that is not one non-empty text without a control character, gives none either: nothing is
// defaulted and nothing is guessed (INV-19).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrNoTargetVersion is returned when the mapping declares no target version for the source.
var ErrNoTargetVersion = errors.New("evidence mapping: no target version is declared for the source")

// ErrBadToken is returned when a payload marked complete has no usable version token: the member is missing, is not text, is empty or has a control character.
var ErrBadToken = errors.New("evidence mapping: the payload has no usable version token")

type targetSpec struct {
	Source   string `json:"source"`
	Token    string `json:"token"`    // the payload member that holds the version token
	Complete string `json:"complete"` // the payload member that says whether the read answered; the boolean true and nothing else
}

// parseTargetVersions reads the optional member. Absent is none. Present, it is a non-empty list of whole declarations of distinct sources of the pack, and nothing else is accepted.
func parseTargetVersions(raw json.RawMessage, known map[string]bool) (map[string]targetSpec, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var list []targetSpec
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&list); err != nil || list == nil || len(list) == 0 {
		return nil, fmt.Errorf("%w: target_versions is a non-empty list of declarations", errMapping)
	}
	out := make(map[string]targetSpec, len(list))
	for i, t := range list {
		switch {
		case t.Source == "" || t.Token == "" || t.Complete == "":
			return nil, fmt.Errorf("%w: target_versions[%d] names a source, a token member and a complete member", errMapping, i)
		case t.Token == t.Complete:
			return nil, fmt.Errorf("%w: target_versions[%d]: the complete marker must be a member other than the token", errMapping, i)
		case !known[t.Source]:
			return nil, fmt.Errorf("%w: target_versions[%d]: the source is not an evidence source of the pack", errMapping, i)
		}
		if _, twice := out[t.Source]; twice {
			return nil, fmt.Errorf("%w: target_versions[%d]: the source is declared twice", errMapping, i)
		}
		out[t.Source] = t
	}
	return out, nil
}

// DeclaresTargetVersion reports whether the mapping names a version token for the source.
func (m *Mapping) DeclaresTargetVersion(source string) bool {
	_, ok := m.targets[source]
	return ok
}

// TargetVersion gives the version token of a source's stored payload: ErrNoTargetVersion for a source the mapping does not declare, ErrIncomplete unless the payload's marker is the
// boolean true (so a token on a read that did not finish is never used), and ErrBadToken for a payload with no usable token. On an error the token is empty. It never panics on a
// payload of any shape.
func (m *Mapping) TargetVersion(source string, payload map[string]any) (string, error) {
	t, ok := m.targets[source]
	if !ok {
		return "", ErrNoTargetVersion
	}
	if done, ok := payload[t.Complete].(bool); !ok || !done {
		return "", ErrIncomplete
	}
	tok, ok := payload[t.Token].(string)
	if !ok || tok == "" || strings.ContainsFunc(tok, unicode.IsControl) {
		return "", ErrBadToken
	}
	return tok, nil
}
