package intakesources_test

// Proves the readers behind the list entities: the sealed snapshot's items and the payload store, each read as the tenant's own Trust login (REQ-SEC-004, INV-11, INV-17), with the
// bound of a payload from configuration (AGENT-16), fixed error texts and the cause kept (AGENT-13), and a foreign row answered exactly as an absent one (REQ-TRU-003). Real PostgreSQL.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
)

func TestListReadersHideTheDriversTextAndKeepTheCause(t *testing.T) {
	boom := errors.New("connection refused by db-host-17.internal as login trust_rw__ten_aa")
	failing := loginFunc(func(string) (intakesources.Querier, error) { return failingLogin{boom}, nil })
	items, err := intakesources.NewSnapshotItems(failing, readTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := items.Items(ctx(t), tenantA, snapID); !errors.Is(err, intakesources.ErrStore) || !errors.Is(err, boom) || strings.Contains(err.Error(), "db-host-17") {
		t.Errorf("snapshot items: %v; want ErrStore with the cause reachable and no driver text", err)
	}
	store, err := intakesources.NewPayloadStore(failing, readTimeout, maxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx(t), tenantA, "pay_1"); !errors.Is(err, intakesources.ErrStore) || !errors.Is(err, boom) || strings.Contains(err.Error(), "db-host-17") {
		t.Errorf("payload store: %v; want ErrStore with the cause reachable and no driver text", err)
	}
	noLogin := loginFunc(func(string) (intakesources.Querier, error) { return nil, boom })
	items, _ = intakesources.NewSnapshotItems(noLogin, readTimeout)
	if _, err := items.Items(ctx(t), tenantA, snapID); !errors.Is(err, intakesources.ErrStore) || strings.Contains(err.Error(), "db-host-17") {
		t.Errorf("a login that cannot be had: %v", err)
	}
}

func TestListReadersNeedADeadlineAndAConfiguration(t *testing.T) {
	w := newWorld(t)
	items, err := intakesources.NewSnapshotItems(w, readTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := items.Items(context.Background(), tenantA, snapID); !errors.Is(err, intakesources.ErrNoDeadline) {
		t.Errorf("no deadline: %v", err)
	}
	store, err := intakesources.NewPayloadStore(w, readTimeout, maxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background(), tenantA, "pay_1"); !errors.Is(err, intakesources.ErrNoDeadline) {
		t.Errorf("no deadline: %v", err)
	}
	if _, err := intakesources.NewSnapshotItems(nil, readTimeout); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("no logins: %v", err)
	}
	if _, err := intakesources.NewSnapshotItems(w, 0); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a timeout of zero: %v", err)
	}
	for name, build := range map[string]func() error{
		"no logins":        func() error { _, err := intakesources.NewPayloadStore(nil, readTimeout, maxPayload); return err },
		"a timeout of 0":   func() error { _, err := intakesources.NewPayloadStore(w, 0, maxPayload); return err },
		"a bound of zero":  func() error { _, err := intakesources.NewPayloadStore(w, readTimeout, 0); return err },
		"a negative bound": func() error { _, err := intakesources.NewPayloadStore(w, readTimeout, -1); return err },
	} {
		if err := build(); !errors.Is(err, intakesources.ErrConfig) {
			t.Errorf("payload store, %s: %v; want ErrConfig", name, err)
		}
	}
	var noItems *intakesources.DBSnapshotItems
	if _, err := noItems.Items(ctx(t), tenantA, snapID); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a nil reader: %v", err)
	}
	var noStore *intakesources.PayloadStore
	if _, err := noStore.Read(ctx(t), tenantA, "pay_1"); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a nil store: %v", err)
	}
}

func TestPayloadStoreReadsOnlyTheTenantsOwnPayloadAndBoundsItsSize(t *testing.T) {
	w := newWorld(t)
	w.store(t, tenantB, []seed{caseItem("evr_9", "KNOWN", page(true))})
	w.store(t, tenantA, []seed{caseItem("evr_1", "KNOWN", page(true, caseRow("cas_1", subjectRef, "OPEN", 1)))})
	store, err := intakesources.NewPayloadStore(w, readTimeout, maxPayload)
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := store.Read(ctx(t), tenantA, "pay_ten_aa_evr_1"); err != nil || !strings.Contains(string(raw), "cas_1") {
		t.Errorf("the tenant's own payload: %q, %v", raw, err)
	}
	_, foreign := store.Read(ctx(t), tenantA, "pay_ten_bb_evr_9")
	_, absent := store.Read(ctx(t), tenantA, "pay_nothing")
	if !errors.Is(foreign, intakesources.ErrNoPayload) || !errors.Is(absent, intakesources.ErrNoPayload) || foreign.Error() != absent.Error() {
		t.Errorf("a foreign payload reads as an absent one: %v / %v", foreign, absent)
	}
	all, _ := intakesources.NewPayloadStore(superuser{w}, readTimeout, maxPayload)
	if _, err := all.Read(ctx(t), tenantA, "pay_ten_bb_evr_9"); !errors.Is(err, intakesources.ErrNoPayload) {
		t.Errorf("tenant A through a login that sees every row: %v; want ErrNoPayload", err)
	}
	small, _ := intakesources.NewPayloadStore(w, readTimeout, 10)
	if raw, err := small.Read(ctx(t), tenantA, "pay_ten_aa_evr_1"); !errors.Is(err, intakesources.ErrPayloadTooLarge) || raw != nil {
		t.Errorf("a payload over the bound: %q, %v; want ErrPayloadTooLarge and nothing", raw, err)
	}
}

func TestSnapshotItemsGiveTheStoredItemsOfTheSealedSnapshot(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true)), complaintItem("evr_2", "STALE", page(true)))
	items, err := intakesources.NewSnapshotItems(w, readTimeout)
	if err != nil {
		t.Fatal(err)
	}
	got, err := items.Items(ctx(t), tenantA, snapID)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	byID := map[string]intakesources.SnapshotItem{got[0].EvidenceID: got[0], got[1].EvidenceID: got[1]}
	if i := byID["evr_1"]; i.Source != caseSource || i.ResourceScope != "case:TX88213" || i.Status != "KNOWN" || i.PayloadRef != "pay_ten_aa_evr_1" || i.PayloadHash != canonHash(t, page(true)) {
		t.Errorf("item evr_1: %+v", i)
	}
	if byID["evr_2"].Status != "STALE" {
		t.Errorf("item evr_2: %+v", byID["evr_2"])
	}
}
