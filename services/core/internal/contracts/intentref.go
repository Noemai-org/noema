package contracts

import (
	"errors"
	"regexp"
	"strconv"
)

// ErrIntentRef is returned for a string that is not an intent reference.
var ErrIntentRef = errors.New("contracts: not an intent reference, int_<id>@<version>")

// intentRefRe is the reference `int_<id>@<version>` (common.v1.json#IntentRef; PERD 20.1): the intent ID of the contract (`int_` and 2 to 32 letters or digits), an at sign, and the
// version as a positive integer of at most nine digits with no leading zero, so that it always fits an int. The contract's own pattern takes any digits after the at sign; this form
// is the strict subset that Trust reads and the Interpreter port builds. `\A` and `\z` anchor the whole string: a trailing newline is not accepted.
var intentRefRe = regexp.MustCompile(`\A(int_[0-9A-Za-z]{2,32})@([1-9][0-9]{0,8})\z`)

// ParseIntentRef splits an intent reference into the intent ID (with its `int_` prefix) and the version. It is the one parser of the form, hand-written beside the generated
// CommonV1IntentRef (ruling A319): every consumer calls it and none keeps a pattern of its own. Anything else, the empty string included, is ErrIntentRef and the zero values.
func ParseIntentRef(ref string) (intentID string, version int, err error) {
	m := intentRefRe.FindStringSubmatch(ref)
	if m == nil {
		return "", 0, ErrIntentRef
	}
	v, convErr := strconv.Atoi(m[2])
	if convErr != nil {
		return "", 0, ErrIntentRef
	}
	return m[1], v, nil
}
