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
	"strconv"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// Errors returned by this package. Callers compare with errors.Is.
var (
	// ErrNotAllow is returned for any decision that is not an ALLOW with a complete basis: only an ALLOW issues a grant (REQ-TRU-008).
	ErrNotAllow = errors.New("grants: only an ALLOW decision with its basis issues a grant")
	// ErrNoPermission is returned when the step carries no freeze Permission, or one issued for another capability or target (REQ-RES-002): a grant is
	// never built for a frozen target and a capability that is not allowed under a freeze.
	ErrNoPermission = errors.New("grants: no freeze permission for this capability and target")
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

// canonical is the JCS form of the grant without its sig; the signature covers it under the grant's domain label (crypto.GrantSigningMessage).
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
	canon, err := canonical(g)
	if err != nil || !k.VerifyBytes(g.Sig, crypto.GrantSigningMessage(canon)) {
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

// NewIssuer refuses to start without keys, an ID source, an entropy source and a validity of at least one second. The validity (REQ-TRU-008 default
// 10 minutes, ADOPTED) comes from configuration, not from here.
func NewIssuer(keys *Keys, ids IDs, entropy io.Reader, validity time.Duration) (*Issuer, error) {
	if keys == nil || ids == nil || entropy == nil || validity < time.Second {
		return nil, errors.New("grants: an issuer needs keys, an ID source, an entropy source and a validity of at least one second (timestamps have whole seconds)")
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
	Permission          reconcile.Permission // the proof from FreezeGate.Permit that a grant may be built for CapabilityID on this target (REQ-RES-002)
	PolicyID            string               // which policy the grant is for; needed only when the decision's basis names more than one policy
	ConfirmationEventID string               // optional: the confirmation this grant follows
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
	if !s.Permission.Valid() || s.Permission.Capability() != s.CapabilityID || s.Permission.Target() != s.TargetRef {
		return contracts.DecisionGrantV1{}, ErrNoPermission
	}
	if !shapes["policy"].MatchString(basis.PackVersion) {
		return contracts.DecisionGrantV1{}, fmt.Errorf("%w: pack version", ErrStep)
	}
	policy, ok := boundPolicy(basis, s.PolicyID)
	if !ok || s.PlanStep < 0 || !shapes["policy"].MatchString(policy) {
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
		PolicyVersion:  policy, PackVersion: basis.PackVersion,
		NotBefore: contracts.CommonV1Timestamp(at.Format(timeLayout)), ExpiresAt: contracts.CommonV1Timestamp(at.Add(i.validity).Format(timeLayout)),
		Nonce: "n_" + hex.EncodeToString(n[:]), SingleUse: true,
	}
	if s.ConfirmationEventID != "" {
		c := contracts.CommonV1ConfirmationEventId(s.ConfirmationEventID)
		g.ConfirmationEventId = &c
	}
	canon, err := canonical(g)
	if err != nil {
		return contracts.DecisionGrantV1{}, fmt.Errorf("%w: %v", ErrStep, err)
	}
	// the signature covers the domain label, a 0x00 byte and the JCS bytes (ruling of 8 Nov 2026 on noema#292)
	sig, err := i.keys.ring.SignWith(i.keys.signing, crypto.GrantSigningMessage(canon))
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
		{"hash", string(s.EvidenceSetHash)}, {"hash", string(s.PlanHash)},
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

// boundPolicy returns policy_id@version for the grant: the exact policy it was decided under (ruling of 5 Nov 2026). Versions compare only
// within one policy_id, as dotted numbers (3.10 is newer than 3.9): several versions of the same policy in one basis (an AT_ADMISSION and an
// AT_DECISION rule) give the newest of that policy. The newest across policy IDs is never picked: when the basis names more than one policy
// the caller must say which one the grant is for, and a policy the basis does not name is refused. A version that is not dotted numbers makes
// the result unusable (false), not a guess. The grant is voided if any version named in the basis is superseded (REQ-TRU-005; TRU-08).
func boundPolicy(b decide.Basis, policyID string) (string, bool) {
	named := map[string]bool{}
	for _, r := range b.PolicyRules {
		named[r.PolicyID] = true
	}
	switch {
	case policyID == "" && len(named) == 1:
		for id := range named {
			policyID = id
		}
	case policyID == "" || !named[policyID]:
		return "", false
	}
	var best string
	var bestNums []int
	for _, r := range b.PolicyRules {
		if r.PolicyID != policyID {
			continue
		}
		nums, ok := dotted(r.Version)
		if !ok {
			return "", false
		}
		if bestNums == nil || newer(nums, bestNums) > 0 {
			best, bestNums = r.Version, nums
		}
	}
	if len(bestNums) == 0 {
		return "", false
	}
	return policyID + "@" + best, true
}

// dotted parses "3.10.1" into its numbers. A component is ASCII digits only, so a sign, a space or another script's digit is refused; a leading
// zero is allowed because the grant and policy-version contracts allow it ("03.01" is the number 3 then 1, and 3.010 is newer than 3.9).
func dotted(v string) ([]int, bool) {
	var out []int
	for _, part := range strings.Split(v, ".") {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

// newer compares two number lists: positive when a is newer than b, zero when equal (a missing trailing part counts as zero).
func newer(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}
