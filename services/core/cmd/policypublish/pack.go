package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

// pack.go reads the policy document of a pack, as data (SoT 51: a pack is read, never imported), and checks it with the policy loader of Trust (REQ-TRU-004), against the
// vocabulary the pack itself declares: the typed entities and variables a condition may name, and the capabilities a rule may apply to. The policy ID and the version are the
// document's own; nothing else supplies them.

// maxPackBytes bounds the pack file that is read: an operating limit of this command, not a product threshold.
const maxPackBytes = 8 << 20

var (
	// errPackRead marks a pack that could not be read: nothing was judged.
	errPackRead = errors.New("policypublish: the pack file cannot be read")
	// errPackInvalid marks a pack whose policy document is refused.
	errPackInvalid = errors.New("policypublish: the pack's policy document is refused")
)

type fieldType struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type descriptor struct {
	Entity string               `json:"entity"`
	Fields map[string]fieldType `json:"fields"`
}

// packFile holds the parts of a pack that a policy needs. A pack has many other keys, which are not read here.
type packFile struct {
	Policy       json.RawMessage `json:"policy"`
	Capabilities []struct {
		ID string `json:"capability_id"`
	} `json:"capabilities"`
	Descriptors []descriptor         `json:"evidence_descriptors"`
	Variables   map[string]fieldType `json:"predicate_variables"`
}

func typeOf(f fieldType) (predicate.Type, error) {
	switch f.Type {
	case "Boolean":
		return predicate.Boolean, nil
	case "String":
		return predicate.String, nil
	case "Integer":
		return predicate.Integer, nil
	case "Timestamp":
		return predicate.Timestamp, nil
	case "Duration":
		return predicate.Duration, nil
	case "Reference":
		return predicate.Reference, nil
	case "Enum":
		if len(f.Values) == 0 {
			return predicate.Type{}, errors.New("an Enum needs values")
		}
		return predicate.EnumOf(f.Values...), nil
	}
	return predicate.Type{}, fmt.Errorf("unknown field type %q", f.Type)
}

// readPack reads name inside dir and nowhere else: a name that leaves the directory, by a separator or by a link, is refused by the root.
func readPack(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errPackRead
	}
	defer func() { _ = root.Close() }() // read-only handle: nothing is flushed, so a close error cannot change what was read
	f, err := root.Open(name)
	if err != nil {
		return nil, errPackRead
	}
	defer func() { _ = f.Close() }() // read-only handle, as above
	raw, err := io.ReadAll(io.LimitReader(f, maxPackBytes+1))
	if err != nil || len(raw) > maxPackBytes {
		return nil, errPackRead
	}
	return raw, nil
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errPackInvalid, fmt.Sprintf(format, a...))
}

// vocabulary builds the scope and the targets a policy is checked against from the pack's own declarations.
func vocabulary(top packFile) (policy.Vocabulary, error) {
	sc := predicate.Scope{Entities: map[string]map[string]predicate.Type{}, Variables: map[string]predicate.Type{}}
	for _, d := range top.Descriptors {
		if _, dup := sc.Entities[d.Entity]; dup {
			return policy.Vocabulary{}, invalid("two descriptors for entity %q", d.Entity)
		}
		fields := map[string]predicate.Type{}
		for name, ft := range d.Fields {
			t, err := typeOf(ft)
			if err != nil {
				return policy.Vocabulary{}, invalid("%s.%s: %v", d.Entity, name, err)
			}
			fields[name] = t
		}
		sc.Entities[d.Entity] = fields
	}
	for name, ft := range top.Variables {
		t, err := typeOf(ft)
		if err != nil {
			return policy.Vocabulary{}, invalid("variable %s: %v", name, err)
		}
		sc.Variables[name] = t
	}
	v := policy.Vocabulary{Scope: sc}
	for _, c := range top.Capabilities {
		v.Targets = append(v.Targets, c.ID)
	}
	return v, nil
}

// loadPolicy returns the pack's policy document, checked, and its document hash (policy.DocumentHash of the bytes of the pack's "policy" member, the value a loader recomputes from the
// same file). A pack that cannot be read is errPackRead; a document or vocabulary the loader refuses is errPackInvalid.
func loadPolicy(dir, name string) (policy.Version, string, error) {
	raw, err := readPack(dir, name)
	if err != nil {
		return policy.Version{}, "", err
	}
	var top packFile
	if err := json.Unmarshal(raw, &top); err != nil {
		return policy.Version{}, "", invalid("the pack is not a JSON document")
	}
	if len(top.Policy) == 0 || string(top.Policy) == "null" {
		return policy.Version{}, "", invalid("the pack has no policy")
	}
	vocab, err := vocabulary(top)
	if err != nil {
		return policy.Version{}, "", err
	}
	if len(vocab.Targets) == 0 {
		return policy.Version{}, "", invalid("the pack registers no capability")
	}
	v, err := policy.Parse(top.Policy, vocab)
	if err != nil {
		return policy.Version{}, "", fmt.Errorf("%w: %v", errPackInvalid, err)
	}
	hash, err := policy.DocumentHash(top.Policy)
	if err != nil {
		return policy.Version{}, "", fmt.Errorf("%w: %v", errPackInvalid, err)
	}
	return v, hash, nil
}
