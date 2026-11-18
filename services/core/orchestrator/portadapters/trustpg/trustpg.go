// Package trustpg is the Postgres side of the Trust adapter of noema#710 (STM-01, REQ-TRU-009, REQ-RCV-003): the intent, the decision and grant records and the dispatch count it
// reads, each as the tenant's own login. This file is the red stub: nothing is built yet.
package trustpg

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

var (
	ErrConfig = errors.New("trustpg: not built")
	ErrNoPool = errors.New("trustpg: not built (no pool)")
)

type Pools struct{}

func NewPools(map[string]*pgxpool.Pool) (Pools, error) { return Pools{}, ErrConfig }

type Intents struct{}

func NewIntents(Pools, time.Duration) (*Intents, error) { return nil, ErrConfig }
func (*Intents) Get(context.Context, string, string, int) (intake.AdmittedIntent, error) {
	return intake.AdmittedIntent{}, ErrConfig
}

type Records struct{}

func NewRecords(Pools, time.Duration) (*Records, error) { return nil, ErrConfig }
func (*Records) Decision(context.Context, string, string) (trustport.DecisionRecord, error) {
	var d trustport.DecisionRecord
	return d, ErrConfig
}
func (*Records) Grant(context.Context, string, string) (trustport.GrantRecord, error) {
	var g trustport.GrantRecord
	return g, ErrConfig
}

type KeyDispatches struct{}

func NewKeyDispatches(reconstruct.TenantDBs) (*KeyDispatches, error) { return nil, ErrConfig }
func (*KeyDispatches) Used(context.Context, intake.DispatchKey) (int, error) {
	return 0, ErrConfig
}
