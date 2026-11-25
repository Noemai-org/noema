package controlplane

import (
	"context"
	"errors"
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
	// ErrNotBuilt is what the red stage of EXE-06 returns.
	ErrNotBuilt = errors.New("controlplane: not built")
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

// Set turns the tenant's switch on and records KILL_SWITCH_CHANGED naming the caller.
func (s *Switch) Set(ctx context.Context, tenantID, correlationID string, c Caller) error {
	return ErrNotBuilt
}

// Clear turns the tenant's switch off and records KILL_SWITCH_CHANGED and SECURITY_EVENT KILL_SWITCH_CLEARED. Only RoleAdmin may; any other caller gets ErrForbidden and
// SECURITY_EVENT KILL_SWITCH_CLEAR_REFUSED, and nothing changes.
func (s *Switch) Clear(ctx context.Context, tenantID, correlationID string, c Caller) error {
	return ErrNotBuilt
}
