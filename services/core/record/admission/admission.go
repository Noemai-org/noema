// Package admission is the Record-backed read behind the orchestrator's Admissions port (STM-01; A161, A169): it finds one event of a tenant's chain by its ID and returns the
// facts the orchestrator compares before it runs a turn. It reads as the tenant's own record_read login, so the database, not this code, decides whose chain is read (DEBT-04);
// it verifies the chain before it trusts a row; it decides nothing.
//
// May import: the standard library, pgx, record/chain, record/reconstruct (its TenantDBs and TxBeginner).
//
// Must never: write, take a tenant from anything but the pool it was given for the tenant, or serve a row of a chain that does not verify.
package admission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

const (
	errPrefix       = "admission: "
	rollbackTimeout = 5 * time.Second // the bound of a rollback after the read, on its own deadline
)

var (
	// ErrRequest marks a call that cannot be honoured; nothing was read.
	ErrRequest = errors.New(errPrefix + "the request is not valid")
	// ErrNotFound is the answer for an event that does not exist in this tenant's chain, whether it never existed or belongs to another tenant.
	ErrNotFound = errors.New(errPrefix + "no such event in this tenant's Record")
	// ErrIntegrity marks a chain that does not verify: nothing is served from it.
	ErrIntegrity = errors.New(errPrefix + "the Record does not verify")
)

var (
	tenantRe = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)
	eventRe  = regexp.MustCompile(`\Arecev_[0-9A-Za-z]{2,32}\z`)
)

// Found is what the Record holds for one event: the facts the orchestrator compares.
type Found struct {
	EventType     string
	TenantID      string // the tenant whose chain was read and verified
	PrincipalID   string // the actor after its kind ("customer:usr_01" gives "usr_01"); empty when the actor names no person
	CorrelationID string
	RequestID     string // the payload's request_id; empty when the event carries none
}

// Reader reads through one pool per tenant, each logged in as that tenant's record_read.
type Reader struct {
	DBs reconstruct.TenantDBs
}

// Lookup finds the event with this ID in the tenant's chain.
func (r *Reader) Lookup(ctx context.Context, tenantID, eventID string) (Found, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Found{}, fmt.Errorf("%w: the context needs a deadline", ErrRequest)
	}
	if r == nil || r.DBs == nil {
		return Found{}, fmt.Errorf("%w: the reader is not configured", ErrRequest)
	}
	if !tenantRe.MatchString(tenantID) || !eventRe.MatchString(eventID) {
		return Found{}, fmt.Errorf("%w: a tenant or event ID is outside its pattern", ErrRequest)
	}
	db, err := r.DBs.For(tenantID)
	if err != nil {
		return Found{}, fmt.Errorf("%sno reader for the tenant: %w", errPrefix, err)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Found{}, fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	rows, err := chain.ReadChain(ctx, tx)
	if err != nil {
		return Found{}, fmt.Errorf("%sread: %w", errPrefix, err)
	}
	if _, err := chain.VerifyRows(tenantID, rows); err != nil {
		return Found{}, fmt.Errorf("%w: %w", ErrIntegrity, err)
	}
	for _, row := range rows {
		if row.EventID != eventID {
			continue
		}
		return Found{EventType: row.EventType, TenantID: tenantID, PrincipalID: principalOf(row.Actor), CorrelationID: row.CorrelationID, RequestID: requestOf(row.Payload)}, nil
	}
	return Found{}, ErrNotFound
}

// principalOf is the part of an actor after its kind. An actor with no kind (a service identity such as core-edge) names no person.
func principalOf(actor string) string {
	_, id, ok := strings.Cut(actor, ":")
	if !ok {
		return ""
	}
	return id
}

// requestOf is the string request_id of a payload, or empty.
func requestOf(payload *string) string {
	if payload == nil {
		return ""
	}
	var p struct {
		RequestID any `json:"request_id"`
	}
	if json.Unmarshal([]byte(*payload), &p) != nil {
		return ""
	}
	s, _ := p.RequestID.(string)
	return s
}
