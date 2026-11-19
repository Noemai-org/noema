package decide

import "errors"

// ErrPolicySetInvalid is returned by CompositePolicyVersion for a set that cannot be bound: no policy version, or one whose policy ID or version is empty or outside the binding's
// character set.
var ErrPolicySetInvalid = errors.New("decide: the policy version set cannot be bound")

// CompositePolicyVersion is not built yet.
func CompositePolicyVersion(refs []PolicyVersionRef) (string, error) { return "", nil }

// CompositePolicyVersion is not built yet.
func (b Basis) CompositePolicyVersion() (string, error) { return "", nil }
