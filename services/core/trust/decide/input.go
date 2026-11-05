package decide

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

// ErrInputInvalid is returned by InputHash for an input that cannot be recorded: a hash that is not "sha256:" and 64 lower-case hex
// characters, an empty pack version or identifier, or a missing clock reading.
var ErrInputInvalid = errors.New("decide: the decision input is invalid")

// PolicyVersionRef names one policy version that took part in a decision: the version set of REQ-TRU-007.
type PolicyVersionRef struct {
	PolicyID string
	Version  string
}

// DecisionInput is everything a decision is a pure function of (REQ-TRU-007, INV-21): the Intent Object, the sealed evidence snapshot,
// the policy versions, the pack version and the clock reading. Nothing else may influence a decision: not the order of a map, not a
// random value, not the wall clock, not the network. SnapshotHash is empty for a refusal made before any evidence was read (T1 to T6).
type DecisionInput struct {
	IntentHash     string // sha256: of the Intent Object
	SnapshotHash   string // the evidence_set_hash of the sealed snapshot; empty before any evidence is read
	PolicyVersions []PolicyVersionRef
	PackVersion    string
	EvaluatedAt    int64 // the clock reading, Unix seconds, passed by the caller
}

var sha256Form = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

const inputHashDomain = "noema/decision-input/v1"

// InputHash is the decision input hash recorded with the decision, so that a replay can show it started from the same inputs. It is
// "sha256:" and the SHA-256 of these fields in this order, each as LP(x), a 4-byte big-endian length then the UTF-8 bytes: the domain
// tag; the intent hash; the snapshot hash (empty allowed); the pack version; the clock reading in decimal; the number of distinct policy
// versions in decimal; then each distinct (policy ID, version) pair, sorted by policy ID and then version, as LP(id) LP(version).
// The order and repeats of the policy versions never change the hash. Every value is an identifier, a hash or an integer, so the form
// needs no escaping; the length prefix keeps characters from moving between fields.
func InputHash(in DecisionInput) (string, error) {
	if !sha256Form.MatchString(in.IntentHash) || (in.SnapshotHash != "" && !sha256Form.MatchString(in.SnapshotHash)) ||
		in.PackVersion == "" || in.EvaluatedAt <= 0 {
		return "", ErrInputInvalid
	}
	seen := map[PolicyVersionRef]bool{}
	var refs []PolicyVersionRef
	for _, r := range in.PolicyVersions {
		if r.PolicyID == "" || r.Version == "" {
			return "", ErrInputInvalid
		}
		if !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].PolicyID != refs[j].PolicyID {
			return refs[i].PolicyID < refs[j].PolicyID
		}
		return refs[i].Version < refs[j].Version
	})
	h := sha256.New()
	lp := func(s string) {
		h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(s))))
		h.Write([]byte(s))
	}
	lp(inputHashDomain)
	lp(in.IntentHash)
	lp(in.SnapshotHash)
	lp(in.PackVersion)
	lp(fmt.Sprint(in.EvaluatedAt))
	lp(strconv.Itoa(len(refs)))
	for _, r := range refs {
		lp(r.PolicyID)
		lp(r.Version)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
