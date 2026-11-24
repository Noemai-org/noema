package main

// Helpers of the decisions lane tests (STM-01, A537 interim, relayed, recorded in docs#229). Every value is SYNTHETIC: the pack is the synthetic fintech pack under /packs, the
// numbers are test numbers and not the product's lifetimes, and no login, key or DSN is real. Names start with "dec" so that the other lanes' helpers cannot collide.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// decEnv is a full environment: the lifetimes, the read bound, the escalation graph and the core-trust signing key directory (one SYNTHETIC key, mode 0400, in a directory of the test).
func decEnv(t *testing.T, over map[string]string) Env {
	t.Helper()
	dir := t.TempDir()
	writeKey(t, dir, "core-trust.ed25519.1.key", strings.Repeat("c3", 32), 0o400)
	m := envMap{EnvTrustDBTimeout: "1000", EnvTrustGrantLifetimeMS: "7000", EnvTrustRecordLifetimeMS: "8000", EnvTrustTxTimeoutMS: "900", EnvTrustEscalationGraph: syntheticEscalationGraph,
		EnvTrustKeyDir: dir, EnvTrustKeyMaxActive: "2"}
	for k, v := range over {
		if v == "<unset>" {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	return Env{Getenv: m.get}
}

func decPack(t *testing.T) *trustpack.Pack {
	t.Helper()
	p, err := fixturePack(Env{})
	if err != nil {
		t.Fatalf("load the synthetic pack: %v", err)
	}
	return p
}

func decKeys(t *testing.T) *grants.Keys {
	t.Helper()
	k, err := (&loaded{}).load("/synthetic/grant-keys", 2)
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	return k
}

// decBegin and decFacts are the seams of the Recorder that no adapter on main fills; the tests give them so that the rest of the lane can be seen to build. Neither is ever called.
type decBegin struct{}

func (decBegin) Begin(context.Context, string) (pgx.Tx, error) { return nil, context.Canceled }

type decFacts struct{}

func (decFacts) Facts(context.Context, intake.RecordRequest) (trustport.RecordFacts, error) {
	return trustport.RecordFacts{}, context.Canceled
}

// decInputs is a full set of inputs: every adapter the lane can build is buildable from it.
func decInputs(t *testing.T, over map[string]string) decisionInputs {
	t.Helper()
	return decisionInputs{
		Env:   decEnv(t, over),
		Clock: clock.NewManual(time.Unix(1_800_000_000, 0)),
		Pools: trustport.PoolsOf(map[string]*pgxpool.Pool{}),
		Pack:  decPack(t),
		Keys:  decKeys(t),
		Begin: decBegin{},
		Facts: decFacts{},
	}
}

func decGap(p decisionParts, adapter string) (decisionGap, bool) {
	for _, g := range p.Gaps {
		if g.Adapter == adapter {
			return g, true
		}
	}
	return decisionGap{}, false
}
