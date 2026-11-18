package reconciler

// The two things Classify's answer (an id and a class) cannot show: a trusted row that the payload gave no field for is UNKNOWN, and an item expires at its read time plus the limit that was applied (INV-19, INV-23).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

type fixed map[string]any

func (f fixed) Payload(context.Context, string, layer.Item) (map[string]any, error) { return f, nil }

func internal(t *testing.T, p Payloads) *Reconciler {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "packs", "fintech")
	raw, err := os.ReadFile(filepath.Join(dir, "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := reconcile.ParseRuleSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	mraw, err := os.ReadFile(filepath.Join(dir, "evidence_mapping", "mapping.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mapping.Parse(mraw, rs, []string{"fin.src.core_ledger", "fin.src.switch", "fin.src.destination"}, "fin.map.v1@1", mapping.Limits{MaxBytes: 1 << 20, MaxDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(Config{RuleSet: rs, Mapping: m, Payloads: p, Clock: clock.NewManual(time.Unix(0, 0))})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func item() layer.Item {
	return layer.Item{EvidenceID: "evd_a", PayloadRef: "pay_a", PayloadHash: "sha256:" + string(make([]byte, 0)), Status: contracts.EnumsV1FactStatusKNOWN, Source: "fin.src.core_ledger",
		ReadAt: time.Date(2026, 11, 17, 9, 0, 0, 999000000, time.UTC), EffectiveAt: time.Date(2026, 11, 17, 8, 59, 0, 0, time.UTC), EffectiveFreshnessSeconds: 300}
}

func TestATrustedRowThePayloadGaveNoFieldIsUnknownAndOneItDidGiveIsKnown(t *testing.T) {
	ev, err := internal(t, fixed{"entries": []any{}}).rows(context.Background(), "ten_demo", item())
	if err != nil || len(ev) != 1 || ev[0].Status != reconcile.Unknown || len(ev[0].Fields) != 0 {
		t.Fatalf("rows %+v, err %v", ev, err)
	}
	ev, err = internal(t, fixed{"entries": []any{map[string]any{"entry_type": "DEBIT", "posting_status": "POSTED"}}}).rows(context.Background(), "ten_demo", item())
	if err != nil || len(ev) != 1 || ev[0].Status != reconcile.Known || len(ev[0].Fields) != 1 || !ev[0].Required {
		t.Fatalf("rows %+v, err %v", ev, err)
	}
}

func TestAnItemExpiresAtItsReadTimePlusTheLimitThatWasApplied(t *testing.T) {
	it := item()
	if got, want := expiry(it), time.Date(2026, 11, 17, 9, 5, 0, 0, time.UTC).Unix(); got != want {
		t.Fatalf("expiry %d, want %d (read time 09:00:00.999 plus 300 s, in whole seconds)", got, want)
	}
	it.EffectiveFreshnessSeconds = 1
	if got, want := expiry(it), time.Date(2026, 11, 17, 9, 0, 1, 0, time.UTC).Unix(); got != want {
		t.Fatalf("expiry %d, want %d", got, want)
	}
}
