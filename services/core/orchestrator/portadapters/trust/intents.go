package trustport

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

var errStub = errors.New("trustport: not built")

// Querier is what the store adapters need of a tenant's own login.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TenantPools hands out a tenant's own login.
type TenantPools interface {
	For(tenantID string) (Querier, error)
}

// PoolsOf makes TenantPools of one pool per tenant.
func PoolsOf(m map[string]*pgxpool.Pool) TenantPools { return nil }

var (
	// ErrNoPool is returned for a tenant that has no login.
	ErrNoPool = errors.New("trustport: no login is configured for this tenant")
	// ErrStore is returned when the store could not be read.
	ErrStore = errors.New("trustport: the store could not be read")
	// ErrStoredIntent is returned for a stored intent the pack does not describe.
	ErrStoredIntent = errors.New("trustport: the stored intent is not usable")
	// ErrPackData is returned for pack data the adapter cannot trust.
	ErrPackData = errors.New("trustport: the pack data is not usable")
)

// StoredIntents implements intake.Intents.
type StoredIntents struct{}

var (
	_ intake.Intents = (*StoredIntents)(nil)
	_ Intents        = (*StoredIntents)(nil)
)

// NewStoredIntents builds the adapter.
func NewStoredIntents(pools TenantPools, pack []byte, timeout time.Duration) (*StoredIntents, error) {
	return &StoredIntents{}, nil
}

// Get is intake.Intents.Get.
func (s *StoredIntents) Get(context.Context, string, string, int) (intake.AdmittedIntent, error) {
	return intake.AdmittedIntent{}, errStub
}

// LatestVersion is intake.Intents.LatestVersion.
func (s *StoredIntents) LatestVersion(context.Context, string, string) (int, error) {
	return 0, errStub
}

// Verify is intake.Intents.Verify.
func (s *StoredIntents) Verify(context.Context, intake.AdmittedIntent) error { return errStub }
