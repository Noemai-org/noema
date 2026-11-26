package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Role is who the control-plane listener authenticated. The zero value is no role.
type Role int

const (
	// RoleVerify is the verification login (C-VERIFY): it may set a tenant's switch and never clear it.
	RoleVerify Role = iota + 1
	// RoleAdmin is a signed-in person (C-ADMIN): it may set and clear.
	RoleAdmin
)

// Caller is the authenticated identity of one control-plane request.
type Caller struct {
	Role        Role
	PrincipalID string
}

// Errors of the control plane. Their text names no tenant, person or value (AGENT-13).
var (
	// ErrForbidden: the caller's role may not make this change. A refused clear is recorded as KILL_SWITCH_CLEAR_REFUSED.
	ErrForbidden = errors.New("controlplane: this caller may not make this change")
	// ErrCaller: the caller has no role or no principal. Nothing is written.
	ErrCaller = errors.New("controlplane: the caller is not identified")
	// ErrConfig: a missing part. There are no defaults (AGENT-16).
	ErrConfig = errors.New("controlplane: the control plane needs logins, an appender and a positive timeout")
)

// Beginner starts a transaction on one tenant's admin emitter login.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Logins gives the admin emitter login of one tenant, and no other tenant's.
type Logins interface {
	For(tenantID string) (Beginner, error)
}

// Switch sets and clears tenants' kill switches.
type Switch struct {
	Logins   Logins
	Appender *chain.Appender
	Timeout  time.Duration
}

const (
	emitter       = "admin"
	eventChanged  = "KILL_SWITCH_CHANGED"
	eventSecurity = "SECURITY_EVENT"
	kindCleared   = "KILL_SWITCH_CLEARED"
	kindRefused   = "KILL_SWITCH_CLEAR_REFUSED"
)

// Set turns the tenant's switch on and records KILL_SWITCH_CHANGED naming the caller. The change and its event are one transaction: if the Record cannot take the event, the switch
// does not change (EC-E16). Setting an already set switch is allowed and recorded, with previous true.
func (s *Switch) Set(ctx context.Context, tenantID, correlationID string, c Caller) error {
	if err := s.check(c); err != nil {
		return err
	}
	return s.apply(ctx, tenantID, correlationID, c, true)
}

// Clear turns the tenant's switch off and records KILL_SWITCH_CHANGED and SECURITY_EVENT KILL_SWITCH_CLEARED in the same transaction. Only RoleAdmin may; any other caller
// gets ErrForbidden and SECURITY_EVENT KILL_SWITCH_CLEAR_REFUSED, and nothing changes. If the refusal cannot be recorded the caller gets both errors.
func (s *Switch) Clear(ctx context.Context, tenantID, correlationID string, c Caller) error {
	if err := s.check(c); err != nil {
		return err
	}
	if c.Role != RoleAdmin {
		werr := s.inTx(ctx, tenantID, func(tx pgx.Tx) error {
			return s.record(ctx, tx, tenantID, correlationID, c, eventSecurity, kindRefused, map[string]any{"role": roleName(c.Role), "requested": "clear"})
		})
		return errors.Join(ErrForbidden, werr)
	}
	return s.apply(ctx, tenantID, correlationID, c, false)
}

func (s *Switch) check(c Caller) error {
	if s == nil || s.Logins == nil || s.Appender == nil || s.Timeout <= 0 {
		return ErrConfig
	}
	if (c.Role != RoleVerify && c.Role != RoleAdmin) || c.PrincipalID == "" {
		return ErrCaller
	}
	return nil
}

// apply moves the switch through the database function and appends the events, in one transaction of the tenant's admin login.
func (s *Switch) apply(ctx context.Context, tenantID, correlationID string, c Caller, on bool) error {
	return s.inTx(ctx, tenantID, func(tx pgx.Tx) error {
		var previous bool
		if err := tx.QueryRow(ctx, `SELECT kill_switch_apply_t($1)`, on).Scan(&previous); err != nil {
			return fmt.Errorf("controlplane: the switch could not be changed: %w", err)
		}
		payload := map[string]any{"kill_switch": on, "previous": previous, "role": roleName(c.Role)}
		if err := s.record(ctx, tx, tenantID, correlationID, c, eventChanged, "", payload); err != nil {
			return err
		}
		if !on {
			return s.record(ctx, tx, tenantID, correlationID, c, eventSecurity, kindCleared, map[string]any{"previous": previous, "role": roleName(c.Role)})
		}
		return nil
	})
}

func (s *Switch) record(ctx context.Context, tx pgx.Tx, tenantID, correlationID string, c Caller, eventType, kind string, payload map[string]any) error {
	actor := emitter // C-VERIFY is a service: no authenticated person, so the emitter's own identity
	if c.Role == RoleAdmin {
		actor = "admin:" + c.PrincipalID
	}
	if _, err := s.Appender.Append(ctx, tx, chain.Event{TenantID: tenantID, CorrelationID: correlationID, WrittenBy: emitter, Actor: actor, EventType: eventType, Kind: kind,
		ObjectRef: tenantID, Payload: payload}); err != nil {
		return fmt.Errorf("controlplane: the Record event could not be appended: %w", err)
	}
	return nil
}

// inTx runs f in one transaction of the tenant's own admin login, with the configured deadline. The transaction commits only when f succeeds.
func (s *Switch) inTx(ctx context.Context, tenantID string, f func(pgx.Tx) error) (err error) {
	b, err := s.Logins.For(tenantID)
	if err != nil {
		return fmt.Errorf("controlplane: no admin login for the tenant: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("controlplane: begin: %w", err)
	}
	defer func() {
		if err != nil {
			rb, stop := context.WithTimeout(context.WithoutCancel(ctx), s.Timeout)
			defer stop()
			_ = tx.Rollback(rb) // the original error is the one that matters
		}
	}()
	if err = f(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("controlplane: commit: %w", err)
	}
	return nil
}

func roleName(r Role) string {
	if r == RoleAdmin {
		return "ADMIN"
	}
	return "VERIFY"
}
