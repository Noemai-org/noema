// Package predicate is the outcome predicate language of SoT §43.7: a small, deterministic expression language over
// evidence fields. It has boolean logic, comparisons, exists, count, field access on evidence entities and time
// arithmetic. It has no loops, no calls other than exists, count and now, and no model. It performs no I/O and reads no
// clock: the caller passes the current time in the environment.
//
// Implements REQ-VER-002 (and the pack-load checks of REQ-AG-001). Reconciliation rules (REQ-RES-001), Action Graph
// pre- and postconditions and outcome definitions all use it.
//
// May import: the standard library, minus I/O, clock and randomness packages.
//
// Must never: import trust, execution, outcome or record; call a model; read the clock; print.
//
// The grammar and the shared corpus are in contracts/predicates/. The Python reference in tools/packcheck runs the same
// corpus, so the two cannot disagree on what is accepted.
package predicate

import "fmt"

// Code names a class of failure. The set is closed and shared with the Python reference.
type Code string

// Parse- and check-time codes. They are produced when a pack loads, never while a request is being decided.
const (
	CodeSyntax           Code = "SYNTAX"
	CodeUnknownName      Code = "UNKNOWN_NAME"
	CodeUnknownEntity    Code = "UNKNOWN_ENTITY"
	CodeUnknownField     Code = "UNKNOWN_FIELD"
	CodeUnknownEnumValue Code = "UNKNOWN_ENUM_VALUE"
	CodeTypeMismatch     Code = "TYPE_MISMATCH"
	CodeNotBoolean       Code = "NOT_BOOLEAN"
)

// Evaluation-time codes. A program that passed Check can still meet a null operand or overflow; the caller treats the
// error as UNKNOWN and fails closed.
const (
	CodeNullOperand Code = "NULL_OPERAND"
	CodeOverflow    Code = "OVERFLOW"
)

// Error is the only error type this package returns. Callers switch on Code, never on the message.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func fail(code Code, message string) *Error { return &Error{Code: code, Message: message} }
