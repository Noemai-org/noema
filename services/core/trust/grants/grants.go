package grants

import (
	"bytes"
	"crypto/aes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Errors returned by this package. Callers compare with errors.Is.
var (
	// ErrNotAllow is returned for any decision that is not an ALLOW with a complete basis: only an ALLOW issues a grant (REQ-TRU-008).
	ErrNotAllow = errors.New("grants: only an ALLOW decision with its basis issues a grant")
	// ErrStep is returned for a plan step that does not match the decision or does not satisfy the grant contract.
	ErrStep = errors.New("grants: the plan step is not valid for this decision")
	// ErrSignature is returned for a grant whose signature does not verify under an active key.
	ErrSignature = errors.New("grants: the signature does not verify")
	// ErrKeys is returned for a key set that cannot be used safely.
	ErrKeys = errors.New("grants: the key set is not usable")
)

// Key counts: one active key, or two during a rotation (REQ-SEC-005).
const (
	_ = iota
	keysOne
	keysTwo
)

const (
	minSecret  = sha256.Size // bytes; an HMAC-SHA-256 key shorter than its output is refused
	maxKeys    = keysTwo
	nonceBytes = aes.BlockSize // 128 bits from a CSPRNG (REQ-TRU-008)
	schemaV    = "1.0"
	timeLayout = "2006-01-02T15:04:05Z"
)

// Key is one HMAC key and its identifier.
type Key struct {
	ID     string
	Secret []byte
}

// Keys holds the active keys: the first signs, any of them verifies. During a rotation there are two; a retired key is simply absent, so
// a grant signed under it is rejected (REQ-SEC-005).
type Keys struct {
	ring    *crypto.KeyRing
	signing string
}

// NewKeys takes one or two keys, newest first. It refuses an empty or repeated ID, a secret shorter than 32 bytes, and two keys with
// the same secret (a rotation that did not change the key).
func NewKeys(keys []Key) (*Keys, error) {
	if len(keys) == 0 || len(keys) > maxKeys {
		return nil, fmt.Errorf("%w: %d keys", ErrKeys, len(keys))
	}
	m := make(map[string][]byte, len(keys))
	for _, k := range keys {
		if k.ID == "" || len(k.Secret) < minSecret {
			return nil, fmt.Errorf("%w: an empty ID or a short secret", ErrKeys)
		}
		if _, dup := m[k.ID]; dup {
			return nil, fmt.Errorf("%w: key %s is repeated", ErrKeys, k.ID)
		}
		for _, other := range m {
			if string(other) == string(k.Secret) {
				return nil, fmt.Errorf("%w: two keys share a secret", ErrKeys)
			}
		}
		m[k.ID] = k.Secret
	}
	return &Keys{ring: crypto.NewKeyRing(m), signing: keys[0].ID}, nil
}

// canonical is the JCS form of the grant without its sig: what is signed.
func canonical(g contracts.DecisionGrantV1) ([]byte, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // integers stay integers: a float never reaches a signature (INV-21)
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	delete(m, "sig")
	return crypto.CanonicalJSON(m)
}

// Verify checks the signature of a grant under the key it names, which must be active. Any failure is ErrSignature.
func (k *Keys) Verify(g contracts.DecisionGrantV1) error {
	msg, err := canonical(g)
	if err != nil || !k.VerifyBytes(g.Sig, msg) {
		return ErrSignature
	}
	return nil
}

// VerifyBytes verifies msg against sig under the key sig names.
func (k *Keys) VerifyBytes(sig contracts.CommonV1Sig, msg []byte) bool {
	return k.ring.VerifyWith(sig.KeyId, msg, sig.Value)
}

// IDs makes the grant ID.
type IDs interface {
	ID(prefix string) (string, error)
}

// Issuer builds and signs grants. The only place a DecisionGrant is built (the construct-decision-grant lint).
type Issuer struct {
	keys     *Keys
	ids      IDs
	entropy  io.Reader
	validity time.Duration
}

// NewIssuer refuses to start without keys, an ID source, an entropy source and a positive validity. The validity (REQ-TRU-008 default
// 10 minutes, ADOPTED) comes from configuration, not from here.
func NewIssuer(keys *Keys, ids IDs, entropy io.Reader, validity time.Duration) (*Issuer, error) {
	if keys == nil || ids == nil || entropy == nil || validity <= 0 {
		return nil, errors.New("grants: an issuer needs keys, an ID source, an entropy source and a positive validity")
	}
	return &Issuer{keys: keys, ids: ids, entropy: entropy, validity: validity}, nil
}

// Step is the plan step a grant is for, with the values it binds. Everything but the validity window, the nonce, the grant ID, the pack
// version and the signature is supplied by the caller from the plan and the sealed snapshot.
type Step struct {
	TenantID            string
	CorrelationID       string
	DecisionID          string
	PrincipalID         string
	IntentHash          contracts.CommonV1Hash
	CapabilityID        string
	TargetRef           string
	ParamsHash          contracts.CommonV1Hash
	EvidenceSetHash     contracts.CommonV1Hash
	PlanHash            contracts.CommonV1Hash
	PlanStep            int64
	TargetVersions      []contracts.DecisionGrantV1TargetVersionsItem
	PolicyVersion       string // policy_id@version; must be one the decision's basis names (INV-09)
	ConfirmationEventID string // optional: the confirmation this grant follows
}

// Issue builds, signs and returns the grant for one write plan step of an ALLOW decision. It issues nothing for any other outcome or for
// a decision without a complete basis, and it checks the result against the grant contract before returning it.
func (i *Issuer) Issue(d decide.Decision, s Step) (contracts.DecisionGrantV1, error) {
	if d.Outcome() != decide.Allow || !d.HasCompleteBasis() {
		return contracts.DecisionGrantV1{}, ErrNotAllow
	}
	basis := d.Basis()
	if err := s.check(); err != nil {
		return contracts.DecisionGrantV1{}, err
	}
	if !policyNamed(basis, s.PolicyVersion) || s.PlanStep < 0 {
		return contracts.DecisionGrantV1{}, fmt.Errorf("%w: policy version or plan step", ErrStep)
	}
	grantID, err := i.ids.ID("grt")
	if err != nil {
		return contracts.DecisionGrantV1{}, fmt.Errorf("grants: grant ID: %w", err)
	}
	var n [nonceBytes]byte
	if _, err := io.ReadFull(i.entropy, n[:]); err != nil {
		return contracts.DecisionGrantV1{}, fmt.Errorf("grants: nonce: %w", err)
	}
	at := time.Unix(basis.EvaluatedAt, 0).UTC()
	g := contracts.DecisionGrantV1{
		SchemaVersion: schemaV, TenantId: contracts.CommonV1TenantId(s.TenantID), CorrelationId: contracts.CommonV1CorrelationId(s.CorrelationID),
		GrantId: contracts.CommonV1GrantId(grantID), DecisionId: contracts.CommonV1DecisionId(s.DecisionID), PrincipalId: contracts.CommonV1PrincipalId(s.PrincipalID),
		IntentHash: s.IntentHash, CapabilityId: contracts.CommonV1PackId(s.CapabilityID), TargetRef: contracts.CommonV1ResourceRef(s.TargetRef),
		ParamsHash: s.ParamsHash, EvidenceSetHash: s.EvidenceSetHash, PlanHash: s.PlanHash, PlanStep: s.PlanStep,
		TargetVersions: append([]contracts.DecisionGrantV1TargetVersionsItem{}, s.TargetVersions...),
		PolicyVersion:  s.PolicyVersion, PackVersion: basis.PackVersion,
		NotBefore: contracts.CommonV1Timestamp(at.Format(timeLayout)), ExpiresAt: contracts.CommonV1Timestamp(at.Add(i.validity).Format(timeLayout)),
		Nonce: "n_" + hex.EncodeToString(n[:]), SingleUse: true,
	}
	if s.ConfirmationEventID != "" {
		c := contracts.CommonV1ConfirmationEventId(s.ConfirmationEventID)
		g.ConfirmationEventId = &c
	}
	msg, err := canonical(g)
	if err != nil {
		return contracts.DecisionGrantV1{}, fmt.Errorf("%w: %v", ErrStep, err)
	}
	sig, err := i.keys.ring.SignWith(i.keys.signing, msg)
	if err != nil {
		return contracts.DecisionGrantV1{}, fmt.Errorf("grants: sign: %w", err)
	}
	g.Sig = contracts.CommonV1Sig{KeyId: i.keys.signing, Value: sig}
	raw, err := json.Marshal(g)
	if err != nil {
		return contracts.DecisionGrantV1{}, fmt.Errorf("grants: %w", err)
	}
	var checked contracts.DecisionGrantV1
	if err := json.Unmarshal(raw, &checked); err != nil {
		return contracts.DecisionGrantV1{}, fmt.Errorf("%w: %v", ErrStep, err)
	}
	return g, nil
}

// The patterns of the identifiers a step supplies, from common.v1.json: the generated types check that a field is present, not what it
// looks like, so the grant checks the shapes itself before it signs anything.
var shapes = map[string]*regexp.Regexp{
	"tenant":      regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`),
	"correlation": regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`),
	"decision":    regexp.MustCompile(`^dec_[0-9A-Za-z]{2,32}$`),
	"principal":   regexp.MustCompile(`^usr_[0-9A-Za-z]{2,32}$`),
	"capability":  regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`),
	"target":      regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$`),
	"hash":        regexp.MustCompile(`^sha256:[0-9a-f]{64}$`),
	"confirm":     regexp.MustCompile(`^cfe_[0-9A-Za-z]{2,32}$`),
	"policy":      regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*@[0-9]+\.[0-9]+(\.[0-9]+)?$`),
}

func (s Step) check() error {
	for _, c := range []struct{ name, value string }{
		{"tenant", s.TenantID}, {"correlation", s.CorrelationID}, {"decision", s.DecisionID}, {"principal", s.PrincipalID},
		{"capability", s.CapabilityID}, {"target", s.TargetRef}, {"hash", string(s.IntentHash)}, {"hash", string(s.ParamsHash)},
		{"hash", string(s.EvidenceSetHash)}, {"hash", string(s.PlanHash)}, {"policy", s.PolicyVersion},
	} {
		if !shapes[c.name].MatchString(c.value) {
			return fmt.Errorf("%w: the %s is not in the form the contract requires", ErrStep, c.name)
		}
	}
	if s.ConfirmationEventID != "" && !shapes["confirm"].MatchString(s.ConfirmationEventID) {
		return fmt.Errorf("%w: the confirmation event ID", ErrStep)
	}
	for _, tv := range s.TargetVersions {
		if !shapes["target"].MatchString(string(tv.ResourceRef)) || !shapes["capability"].MatchString(string(tv.Source)) || tv.VersionToken == "" {
			return fmt.Errorf("%w: a target version", ErrStep)
		}
	}
	return nil
}

// policyNamed reports whether version (policy_id@version) is one of the policy versions the decision's basis names.
func policyNamed(b decide.Basis, version string) bool {
	for _, r := range b.PolicyRules {
		if r.PolicyID+"@"+r.Version == version {
			return true
		}
	}
	return false
}
