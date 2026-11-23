package main

// Test helpers of lane 2 of the G1 final composition (STM-01, A537, interim, relayed, not yet committed): the real fintech pack through its real manifest, the Understanding parts built
// from it as buildUnderstanding builds them, lazily connecting pools (nothing is dialled) and a candidates read that scopes by the request. Every name starts with ti so that the other
// lanes' helpers cannot collide. All data is SYNTHETIC.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const tiManifest = "fintech/manifest.v1.json"

func tiRoot() string { return filepath.Join("..", "..", "..", "..", "packs") }

func tiVerified(t *testing.T) packload.Pack {
	t.Helper()
	p, err := packload.Load(tiRoot(), tiManifest, 1<<20)
	if err != nil {
		t.Fatalf("the real pack does not load through its manifest: %v", err)
	}
	return p
}

func tiPack(t *testing.T) *trustpack.Pack {
	t.Helper()
	p, err := trustpack.LoadPack(os.DirFS(tiRoot()), "fintech_v1.json")
	if err != nil {
		t.Fatalf("the real pack does not load: %v", err)
	}
	return p
}

// tiParts are the rule set and the evidence mapping, built from the verified bytes the way buildUnderstanding builds them (the same functions).
func tiParts(t *testing.T, v packload.Pack) (*reconcile.RuleSet, *mapping.Mapping) {
	t.Helper()
	rs, err := reconcile.ParseRuleSet(v.ReconRules)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := evidenceSources(v.Pack)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := mapping.ScopeFromDescriptors(v.Pack)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mapping.Parse(v.EvidenceMapping, rs, sources, v.EvidenceMappingID, mapping.Limits{MaxBytes: 1 << 20, MaxDepth: 64}, mapping.WithScope(scope))
	if err != nil {
		t.Fatal(err)
	}
	return rs, m
}

// tiPools makes one lazily connecting pool per tenant; nothing is dialled.
func tiPools(t *testing.T, tenants ...string) map[string]*pgxpool.Pool {
	t.Helper()
	out := map[string]*pgxpool.Pool{}
	for _, tenant := range tenants {
		p, err := pgxpool.New(context.Background(), "postgres://trust_rw_"+strings.TrimPrefix(tenant, "ten_")+"@127.0.0.1:1/noema?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		out[tenant] = p
	}
	return out
}

type tiRawPayloads struct{}

func (tiRawPayloads) Read(context.Context, string, string) ([]byte, error) {
	return nil, os.ErrNotExist
}

var _ payloadread.PayloadReader = tiRawPayloads{}

// tiIn is a complete input over the real pack: every part present.
func tiIn(t *testing.T) trustInputsIn {
	t.Helper()
	v := tiVerified(t)
	rs, m := tiParts(t, v)
	return trustInputsIn{
		Pack: tiPack(t), Verified: v, Tenants: []string{"ten_aa", "ten_bb"}, Pools: tiPools(t, "ten_aa", "ten_bb"), Timeout: 750 * time.Millisecond,
		Clock: clock.NewManual(time.Unix(1_800_000_000, 0)), RuleSet: rs, Mapping: m, Payloads: tiRawPayloads{}, Screen: tiScreen(), OwnershipReader: &tiCandidates{},
	}
}

// tiCandidates is a reader of the candidates contract: it scopes its rows by the tenant and the principal of the request, filters by resource kind and stops at the contract's twenty.
type tiOwned struct {
	tenant, principal, ref string
}

type tiCandidates struct {
	mu    sync.Mutex
	rows  []tiOwned
	calls []contracts.CandidatesRequestV1
	fail  error
}

func (s *tiCandidates) Read(ctx context.Context, req contracts.CandidatesRequestV1) (contracts.CandidatesResponseV1, error) {
	if err := ctx.Err(); err != nil {
		return contracts.CandidatesResponseV1{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	if s.fail != nil {
		return contracts.CandidatesResponseV1{}, s.fail
	}
	var out []contracts.CandidatesResponseV1Candidate
	for i, r := range s.rows {
		if contracts.CommonV1TenantId(r.tenant) != req.TenantId || contracts.CommonV1PrincipalId(r.principal) != req.PrincipalRef || !strings.HasPrefix(r.ref, req.ResourceKind+":") {
			continue
		}
		id := strings.TrimPrefix(r.ref, req.ResourceKind+":")
		out = append(out, contracts.CandidatesResponseV1Candidate{
			CandidateId: "cand_" + id + string(rune('a'+i)), ResourceRef: contracts.CommonV1ResourceRef(r.ref), AmountMinor: 1000, Currency: contracts.MoneyV1CurrencyCodeNGN,
			OccurredAt: "2026-11-01T09:00:00Z", CounterpartyDisplay: "Synthetic Shop", AccountLast4: "1234", Direction: contracts.CandidatesResponseV1CandidateDirectionOUTGOING,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CandidateId < out[j].CandidateId })
	if len(out) > 20 {
		out = out[:20]
	}
	return contracts.CandidatesResponseV1{SchemaVersion: "1.0", TenantId: req.TenantId, CorrelationId: req.CorrelationId, Candidates: out}, nil
}

func (s *tiCandidates) reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func tiDeadline(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

// tiScreen is the injection screen of the payload reader: a recording screen that finds nothing (the same test double the payload reader's tests use).
func tiScreen() ScreenDeps { return spDeps(&spScreen{}) }
