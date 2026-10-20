package decide

import (
	"errors"
	"regexp"
)

// ReasonCode is a core code from the closed enum, or a pack code in the pack's namespace (PERD §23.3). The zero value is
// invalid, and the only constructors check the code.
type ReasonCode struct{ code string }

var coreCodes = map[string]bool{
	"PROHIBITED_CAPABILITY": true, "AMBIGUOUS_COUNTERPARTY": true, "DECLINED_BY_APPROVER": true, "POLICY_VERSION_MISMATCH": true,
	"INSUFFICIENT_EVIDENCE": true, "STALE_EVIDENCE": true, "INTENT_INVALID": true, "UNAUTHENTICATED": true, "WRITES_DISABLED": true,
	"OUT_OF_SCOPE": true, "NOT_AUTHORIZED": true, "NOT_FOUND_OR_NOT_AUTHORIZED": true, "PLAN_INVALID": true, "NO_PERMITTING_RULE": true,
	"RATE_LIMITED": true, "INTERNAL_ERROR": true, "MODE_NOT_ENABLED": true,
}

var (
	packName   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	packVertex = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	errReason  = errors.New("decide: invalid reason code")
)

// Core returns a core reason code, or an error when the code is not in the closed enum.
func Core(code string) (ReasonCode, error) {
	if !coreCodes[code] {
		return ReasonCode{}, errReason
	}
	return ReasonCode{code: code}, nil
}

// Pack returns a pack reason code "<vertical>.reason.<name>". The catalogue check (that the pack declares it) is made by
// packcheck when the pack loads; here the form and the namespace are enforced.
func Pack(vertical, code string) (ReasonCode, error) {
	prefix := vertical + ".reason."
	if !packVertex.MatchString(vertical) || len(code) <= len(prefix) || code[:len(prefix)] != prefix || !packName.MatchString(code[len(prefix):]) {
		return ReasonCode{}, errReason
	}
	return ReasonCode{code: code}, nil
}

// String is the code as written in a Decision Record.
func (r ReasonCode) String() string { return r.code }

// IsZero reports whether the code was never constructed.
func (r ReasonCode) IsZero() bool { return r.code == "" }

func checkReasons(rs []ReasonCode) error {
	for _, r := range rs {
		if r.IsZero() {
			return errReason
		}
	}
	return nil
}
