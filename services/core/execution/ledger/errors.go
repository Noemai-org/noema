package ledger

import (
	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// fixedError is an error as callers of the ledger see it (AGENT-13, noema#748 review, Architect ruling A347, relayed, interim): a FIXED PUBLIC TEXT, with the cause kept for
// errors.Is and errors.As and never printed. The tenant's ID and every other identifier, the database driver's message (SQLSTATE text, server and login details), paths and key
// material are not in Error(); a caller that needs the database's code, the context's error or a sentinel asks the chain (errors.As to *pgconn.PgError, errors.Is). cause may be
// nil: a sentinel made with newFixed.
type fixedError struct {
	text  string
	cause error
}

func (e *fixedError) Error() string { return e.text }

func (e *fixedError) Unwrap() error { return e.cause }

// fixed is a failure with a fixed text around the error that caused it.
func fixed(text string, cause error) error { return &fixedError{text: errPrefix + text, cause: cause} }

// newFixed is a sentinel with a fixed text and no cause.
func newFixed(text string) *fixedError { return &fixedError{text: errPrefix + text} }

// publicSpendError is the last step of Spend: whatever leaves it has a fixed text. An error that already has one (a fixedError at the top of the chain, and the controller's
// own ErrKeyCollision, a sentinel with a text of its own) goes out as it is; anything else, such as the database's error from the insert into grant_spend, the begin or the
// commit, or the Record's error (which names the tenant and the driver), is wrapped, and stays in the chain as the cause.
func publicSpendError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*fixedError); ok || err == controller.ErrKeyCollision {
		return err
	}
	return fixed("the spend could not be completed", err)
}
