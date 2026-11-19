package decide

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
)

// ErrPolicySetInvalid is returned by CompositePolicyVersion for a set that cannot be bound: no policy version, or one whose policy ID or version is empty or outside the binding's
// character set.
var ErrPolicySetInvalid = errors.New("decide: the policy version set cannot be bound")

// policyPart is the character set of one part (a policy ID or a version) of a policy version that may be bound: the character set of trust/confirm's binding (A-Z a-z 0-9 _ . : -)
// without "@", which joins the two parts, so that "a@b"+"c" and "a"+"b@c" can never be one string. None of these characters is escaped in JSON, which is what makes the canonical
// form below byte-exact without a general canonicaliser.
var policyPart = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// compositeDomain is the prefix of a composite policy version.
const compositeDomain = "sha256:"

// CompositePolicyVersion is the policy version a confirmation binds (A341, interim, SoT 43.2a, INV-25): the composite of EVERY policy version that took part in the evaluation. It is
// "sha256:" and the lower-case hex SHA-256 of the RFC 8785 form of the sorted, distinct list of the "policy_id@version" strings, as a JSON array of strings with no white space. The list
// is sorted as whole strings in byte order (the characters are ASCII, so this is also UTF-16 code unit order) and each string once, so neither the order nor the repeats of refs change
// the result (INV-21). The form is the content-hash form of the contracts (A367), "sha256:" and 64 hex characters, which the payload guard (A184) exempts as a typed value, and it is the same for one policy version as for many, with no special case.
//
// Example: refs pol_synth01@3 and pol_synth02@1 give the array ["pol_synth01@3","pol_synth02@1"] and the composite is "sha256:" and its SHA-256 hex. It uses only characters of
// trust/confirm's binding value set, so it can stand in the binding's policy_version.
//
// No policy version, an empty part, or a character outside A-Z a-z 0-9 _ . : - in a part is ErrPolicySetInvalid, never a composite.
//
// This is the one function every place that binds a policy version to a confirmation uses: the intake's FreshBinding, the Writer's CONFIRMATION_REQUESTED check and the binding of the
// ALLOW that follows a confirmation, so that they agree byte for byte.
func CompositePolicyVersion(refs []PolicyVersionRef) (string, error) {
	if len(refs) == 0 {
		return "", ErrPolicySetInvalid
	}
	seen := make(map[string]bool, len(refs))
	list := make([]string, 0, len(refs))
	for _, r := range refs {
		if !policyPart.MatchString(r.PolicyID) || !policyPart.MatchString(r.Version) {
			return "", ErrPolicySetInvalid
		}
		s := r.PolicyID + "@" + r.Version
		if !seen[s] {
			seen[s] = true
			list = append(list, s)
		}
	}
	sort.Strings(list)
	sum := sha256.Sum256([]byte(`["` + strings.Join(list, `","`) + `"]`))
	return compositeDomain + hex.EncodeToString(sum[:]), nil
}

// CompositePolicyVersion is the composite of the distinct (policy ID, version) pairs of the basis's rules (the function of the same name): two rules of one policy version are one
// member. A basis with no rule has none, which is ErrPolicySetInvalid.
func (b Basis) CompositePolicyVersion() (string, error) {
	refs := make([]PolicyVersionRef, 0, len(b.PolicyRules))
	for _, r := range b.PolicyRules {
		refs = append(refs, PolicyVersionRef{PolicyID: r.PolicyID, Version: r.Version})
	}
	return CompositePolicyVersion(refs)
}
