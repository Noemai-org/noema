// Package injection is the advisory injection detector of the Evidence layer (EVD-06 slice 2, ruling A292; REQ-CTX-007, REQ-SEC-007; AGENT-09, AGENT-13, AGENT-16; SoT §29).
//
// An enterprise string (a narration, a beneficiary name, a case note) is data, never an instruction. The control for that is the typed, delimited data block of REQ-CTX-007, which is
// not built here. This package adds a heuristic signal on top: it matches the string against a list of patterns that is pack data and, when one matches, raises
// SECURITY_EVENT of kind INJECTION_DETECTED. Detection is ADVISORY ONLY. It takes no evidence status, classification or decision as input and returns none, it blocks no read, and a
// detector that finds nothing or fails changes nothing: safety does not depend on it.
//
// A Finding names the field NAME and the pattern ID and carries no part of the value (AGENT-13). A value is cut to the field cap, after its control characters are removed and
// before any pattern runs, so the work is bounded whatever the input; every pattern is RE2, which matches in linear time.
//
// May import: the standard library, record/chain, platform/* and pgx (the Recorder adapter only).
//
// Must never: import trust, execution or outcome; read the clock, a file or the network; write an evidence status; hold a threshold (the bounds come from the caller, AGENT-16).
package injection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
)

var errPack = errors.New("injection patterns")

// label is the only label a pattern file carries in the POC: every pack is SYNTHETIC (AGENT-25).
const label = "SYNTHETIC"

// idRe is the shape of a pattern id. Letters, dots and underscores only: no digit, so an id can never trip the Record's payload guard (A184).
var idRe = regexp.MustCompile(`^[a-z][a-z_.]{2,63}$`)

// Limits bound a pattern file and a field before anything is decoded or matched. The numbers come from the caller (configuration, AGENT-16); this package holds none. A limit that is
// not positive refuses to load, so a zero value never means "unbounded".
type Limits struct {
	MaxBytes      int // the size of the document
	MaxDepth      int // nesting of objects and arrays, the root object counting as 1
	MaxPatterns   int // the number of patterns
	MaxPatternLen int // the length of one regular expression, in bytes
	MaxFieldRunes int // the cap on one enterprise string, in characters (REQ-CTX-007: 256 [REC])
}

func (l Limits) positive() bool {
	return l.MaxBytes > 0 && l.MaxDepth > 0 && l.MaxPatterns > 0 && l.MaxPatternLen > 0 && l.MaxFieldRunes > 0
}

type patternSpec struct {
	ID    string `json:"id"`
	Regex string `json:"regex"`
	Note  string `json:"note"`
}

type file struct {
	PackID   string        `json:"pack_id"`
	Version  int           `json:"version"`
	Label    string        `json:"label"`
	Note     string        `json:"note"`
	Patterns []patternSpec `json:"patterns"`
}

type pattern struct {
	id string
	re *regexp.Regexp
}

// Detector is a loaded, validated pattern list. It is immutable and safe for concurrent use.
type Detector struct {
	id       string
	patterns []pattern
	maxRunes int
}

// ID is the pack's identity and version, `<pack_id>@<version>`: the event records which list ran.
func (d *Detector) ID() string {
	if d == nil {
		return ""
	}
	return d.id
}

// PatternIDs lists the ids of the patterns in the file's order. The slice is the caller's own.
func (d *Detector) PatternIDs() []string {
	if d == nil {
		return nil
	}
	out := make([]string, len(d.patterns))
	for i, p := range d.patterns {
		out[i] = p.id
	}
	return out
}

// Parse loads a pattern file. expected is the `<pack_id>@<version>` the caller expects. Anything missing, repeated, unknown, not RE2 or able to match the empty string is an error:
// the file is refused, never half-loaded.
func Parse(data []byte, expected string, limits Limits) (*Detector, error) {
	if !limits.positive() {
		return nil, fmt.Errorf("%w: the limits must be positive", errPack)
	}
	if len(data) > limits.MaxBytes {
		return nil, fmt.Errorf("%w: the document is too large", errPack)
	}
	if err := checkMembers(data, limits.MaxDepth); err != nil {
		return nil, err
	}
	var f file
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: not a pattern document", errPack)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: more than one document", errPack)
	}
	if f.PackID == "" || f.Version < 1 || strings.TrimSpace(f.Note) == "" || len(f.Patterns) == 0 {
		return nil, fmt.Errorf("%w: needs a pack_id, a positive version, a note and patterns", errPack)
	}
	if f.Label != label {
		return nil, fmt.Errorf("%w: the label must be %s", errPack, label)
	}
	id := f.PackID + "@" + strconv.Itoa(f.Version)
	if id != expected {
		return nil, fmt.Errorf("%w: the file is %q, not %q as the caller expects", errPack, id, expected)
	}
	if len(f.Patterns) > limits.MaxPatterns {
		return nil, fmt.Errorf("%w: more than %d patterns", errPack, limits.MaxPatterns)
	}
	seen := map[string]bool{}
	out := make([]pattern, 0, len(f.Patterns))
	for i, p := range f.Patterns {
		re, err := checkPattern(p, limits.MaxPatternLen)
		if err != nil {
			return nil, fmt.Errorf("%w: patterns[%d]: %v", errPack, i, err)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("%w: patterns[%d]: the id %s is repeated", errPack, i, p.ID)
		}
		seen[p.ID] = true
		out = append(out, pattern{id: p.ID, re: re})
	}
	return &Detector{id: id, patterns: out, maxRunes: limits.MaxFieldRunes}, nil
}

func checkPattern(p patternSpec, maxLen int) (*regexp.Regexp, error) {
	if !idRe.MatchString(p.ID) {
		return nil, errors.New("the id is not lower-case letters, dots and underscores, 3 to 64 long")
	}
	if strings.TrimSpace(p.Note) == "" {
		return nil, errors.New("a pattern needs a note")
	}
	if p.Regex == "" {
		return nil, errors.New("a pattern needs a regex")
	}
	if len(p.Regex) > maxLen {
		return nil, errors.New("the regex is too long")
	}
	re, err := regexp.Compile(p.Regex) // RE2: no look-around, no back-reference, linear-time matching
	if err != nil {
		return nil, errors.New("the regex is not RE2 syntax")
	}
	tree, err := syntax.Parse(p.Regex, syntax.Perl)
	if err != nil {
		return nil, errors.New("the regex is not RE2 syntax")
	}
	if minLen(tree) == 0 {
		return nil, errors.New("the regex can match the empty string")
	}
	return re, nil
}

// minLen is the length in characters of the shortest string the expression can match. It is zero exactly when the expression can match the empty string, on its own or at some
// position of any text (an anchor or a word boundary matches the empty string there).
func minLen(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar, syntax.OpNoMatch:
		return 1
	case syntax.OpCapture, syntax.OpPlus:
		return minLen(re.Sub[0])
	case syntax.OpRepeat:
		return re.Min * minLen(re.Sub[0])
	case syntax.OpConcat:
		n := 0
		for _, s := range re.Sub {
			n += minLen(s)
		}
		return n
	case syntax.OpAlternate:
		n := minLen(re.Sub[0])
		for _, s := range re.Sub[1:] {
			if m := minLen(s); m < n {
				n = m
			}
		}
		return n
	default: // OpEmptyMatch, the anchors, the word boundaries, OpStar and OpQuest
		return 0
	}
}

// checkMembers refuses a document that names a member twice at any depth (compared without regard to case, as the decoder does), names a member that is not exactly one of the
// document's, or nests deeper than maxDepth. The standard decoder keeps the last of a repeated member silently and accepts a member of the wrong case; a pattern file must mean one thing.
func checkMembers(data []byte, maxDepth int) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func(depth int, allowed map[string]bool) error
	walk = func(depth int, allowed map[string]bool) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		d, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		if depth+1 > maxDepth {
			return fmt.Errorf("%w: the document is nested too deep", errPack)
		}
		if d == '{' {
			seen := map[string]bool{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				key, _ := k.(string)
				fold := strings.ToLower(key)
				if seen[fold] {
					return fmt.Errorf("%w: the member %q is repeated", errPack, key)
				}
				seen[fold] = true
				if allowed != nil && !allowed[key] {
					return fmt.Errorf("%w: the member %q is not one of the document's", errPack, key)
				}
				if err := walk(depth+1, childMembers(depth, key)); err != nil {
					return err
				}
			}
		} else {
			for dec.More() {
				if err := walk(depth+1, childMembers(depth, "")); err != nil {
					return err
				}
			}
		}
		_, err = dec.Token()
		return err
	}
	if err := walk(0, rootMembers); err != nil {
		if errors.Is(err, errPack) {
			return err
		}
		return fmt.Errorf("%w: not a pattern document", errPack)
	}
	return nil
}

var (
	rootMembers    = map[string]bool{"pack_id": true, "version": true, "label": true, "note": true, "patterns": true}
	patternMembers = map[string]bool{"id": true, "regex": true, "note": true}
)

// childMembers says which members the value one level down may have: the elements of "patterns" (an array at depth 1) are pattern objects. Anything else nested is not part of the
// document and is refused by the decoder's own unknown-member and type checks, or by the depth bound.
func childMembers(depth int, key string) map[string]bool {
	if depth == 1 && key == "" {
		return patternMembers
	}
	return nil
}
