package intakesources_test

// Helpers of the list tests. Everything here is SYNTHETIC (AGENT-25). A sealed snapshot is written the way the sealer writes it: the items and their payloads by the tenant's own Evidence
// Layer login, the snapshot by the tenant's own Trust login, whose guard recomputes the set hash (migrations 0008, 0020). The mapping is the REAL evidence mapping of the shipped pack
// (fin.map.v1@2), typed by the pack's own evidence descriptors, as a composition builds it.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/injection"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const (
	caseSource   = "fin.src.case_system"
	ledgerSource = "fin.src.core_ledger"
	caseScopeOf  = "case:"
	complScopeOf = "complaint_ref:"
	snapID       = "evs_s1"
	snapCorr     = "corr_s1"
	maxPayload   = 1 << 20
)

const packDir = "../../../../../../packs"

// seed is one evidence item to store with its payload.
type seed struct {
	id, source, scope, status string
	payload                   map[string]any
	hash                      string // when set, stored as the item's payload hash although it is not the hash of the stored payload
}

func caseItem(id, status string, payload map[string]any) seed {
	return seed{id: id, source: caseSource, scope: caseScopeOf + "TX88213", status: status, payload: payload}
}

func complaintItem(id, status string, payload map[string]any) seed {
	return seed{id: id, source: caseSource, scope: complScopeOf + "TX88213", status: status, payload: payload}
}

func canonHash(t *testing.T, payload map[string]any) string {
	t.Helper()
	canon, err := crypto.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// store writes the items and their payloads as the tenant's Evidence Layer login and returns each item's stored hash, by ID.
func (w *world) store(t *testing.T, tenant string, items []seed) map[string]string {
	t.Helper()
	hashes := map[string]string{}
	for _, it := range items {
		h := it.hash
		if h == "" {
			h = canonHash(t, it.payload)
		}
		raw, err := json.Marshal(it.payload)
		if err != nil {
			t.Fatal(err)
		}
		ref := "pay_" + tenant + "_" + it.id
		p := w.evidence[tenant]
		if _, err := p.Exec(ctx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at) VALUES ($1, $2, $3::jsonb, $4, 'S2_internal', $5)`,
			ref, tenant, string(raw), "sha256:"+fillHex('a'), admittedAt); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if _, err := p.Exec(ctx(t), `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at, freshness_sla_seconds,
			authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
			VALUES ($1, $2, $3, $4, 'rec_synthetic', 'TRUST', $5, $5, 60, 'AUTHORITATIVE_FOR:case_existence_and_status', $6, 'prn_synthetic', 'S2_internal', $7, $8, $9, $5)`,
			it.id, tenant, snapCorr, it.source, admittedAt, it.scope, it.status, h, ref); err != nil {
			t.Fatalf("item: %v", err)
		}
		hashes[it.id] = h
	}
	return hashes
}

func fillHex(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

// seal stores the items and seals the snapshot over them as the tenant's Trust login (the database recomputes the hash).
func (w *world) seal(t *testing.T, tenant, snapshotID string, items ...seed) {
	t.Helper()
	hashes := w.store(t, tenant, items)
	ids, pairs := make([]string, 0, len(items)), make([]snapshot.Pair, 0, len(items))
	for _, it := range items {
		ids, pairs = append(ids, it.id), append(pairs, snapshot.Pair{EvidenceID: it.id, PayloadHash: hashes[it.id]})
	}
	sort.Strings(ids)
	hash, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.trust[tenant].Exec(ctx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, $3, $4, 'rec_synthetic', 'CONSISTENT', $5, $6)`, snapshotID, tenant, snapCorr, ids, hash, admittedAt); err != nil {
		t.Fatalf("seal: %v", err)
	}
}

// tamper runs a statement as the migrating login with the immutability trigger of the items switched off: it plays the part of someone with a database the application cannot reach.
func (w *world) tamper(t *testing.T, stmt string, args ...any) {
	t.Helper()
	for _, s := range []struct {
		q string
		a []any
	}{{`ALTER TABLE evidence_item DISABLE TRIGGER evidence_item_immutable`, nil}, {stmt, args}, {`ALTER TABLE evidence_item ENABLE TRIGGER evidence_item_immutable`, nil}} {
		if _, err := w.admin.Exec(ctx(t), s.q, s.a...); err != nil {
			t.Fatalf("%s: %v", s.q, err)
		}
	}
}

// The payloads of the case system, in the stand-in's page shape: `items` and the completeness marker `complete` (A438, A461).
func page(complete any, rows ...any) map[string]any {
	items := make([]any, 0, len(rows))
	items = append(items, rows...)
	return map[string]any{"items": items, "complete": complete}
}

func caseRow(id, tx, status string, epoch int) map[string]any {
	return map[string]any{"case_id": id, "transaction_ref": tx, "status": status, "created_by_intent": "int_e2e1", "owner_queue": "PAYMENTS_OPERATIONS", "epoch": epoch}
}

func complaintRow(clockStartedAt string) map[string]any {
	return map[string]any{"complaint_ref": "cmp_1", "clock_started_at": clockStartedAt}
}

const subjectRef = "fin.transaction:TX88213"

func unix(year int, month time.Month, day, hour int) int64 {
	return time.Date(year, month, day, hour, 0, 0, 0, time.UTC).Unix()
}

// realMapping is the evidence mapping of the shipped pack, with its list entities typed by the pack's evidence descriptors (mapping.WithScope), as a composition builds it.
func realMapping(t *testing.T) *mapping.Mapping {
	t.Helper()
	read := func(parts ...string) []byte {
		raw, err := os.ReadFile(filepath.Join(append([]string{packDir}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	rs, err := reconcile.ParseRuleSet(read("fintech", "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pack struct {
		Descriptors []struct {
			Entity string `json:"entity"`
			Fields map[string]struct {
				Type   string   `json:"type"`
				Values []string `json:"values"`
			} `json:"fields"`
		} `json:"evidence_descriptors"`
	}
	if err := json.Unmarshal(read("fintech_v1.json"), &pack); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]predicate.Type{"Reference": predicate.Reference, "Integer": predicate.Integer, "String": predicate.String, "Timestamp": predicate.Timestamp, "Boolean": predicate.Boolean}
	sc := predicate.Scope{Entities: map[string]map[string]predicate.Type{}, Variables: map[string]predicate.Type{}}
	for _, d := range pack.Descriptors {
		sc.Entities[d.Entity] = map[string]predicate.Type{}
		for name, f := range d.Fields {
			if f.Type == "Enum" {
				sc.Entities[d.Entity][name] = predicate.EnumOf(f.Values...)
			} else {
				sc.Entities[d.Entity][name] = kinds[f.Type]
			}
		}
	}
	m, err := mapping.Parse(read("fintech", "evidence_mapping", "mapping.v1.json"), rs,
		[]string{ledgerSource, "fin.src.switch", "fin.src.destination", caseSource}, "fin.map.v1@3", mapping.Limits{MaxBytes: 1 << 20, MaxDepth: 32}, mapping.WithScope(sc))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// listsOf is the composition of the readers over a Logins: the real snapshot reader, the real payload store, the real mapping.
func listsOf(t *testing.T, l intakesources.Logins) intakesources.EvidenceLists {
	t.Helper()
	items, err := intakesources.NewSnapshotItems(l, readTimeout)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := intakesources.NewPayloadStore(l, readTimeout, maxPayload)
	if err != nil {
		t.Fatal(err)
	}
	return intakesources.EvidenceLists{Mapping: realMapping(t), Items: items, Payloads: payloads, Screen: listCleanScreen{}, Correlation: listCorrelation, Alert: &listAlerts{}, Entities: map[string]intakesources.ListSpec{
		"case":          {Source: caseSource, ScopePrefix: caseScopeOf},
		"complaint_ref": {Source: caseSource, ScopePrefix: complScopeOf},
	}}
}

func factsWithLists(t *testing.T, l intakesources.Logins) *intakesources.Facts {
	t.Helper()
	f, err := intakesources.NewFacts(l, readTimeout, intakesources.WithLists(listsOf(t, l)))
	if err != nil {
		t.Fatalf("NewFacts: %v", err)
	}
	return f
}

func anchorsOf(t *testing.T, l intakesources.Logins) *intakesources.Anchors {
	t.Helper()
	a, err := intakesources.NewAnchors(listsOf(t, l), map[string]intakesources.AnchorEvent{"complaint_logged": {Entity: "complaint_ref", Field: "effective_at"}})
	if err != nil {
		t.Fatalf("NewAnchors: %v", err)
	}
	return a
}

// listCleanScreen is a screen that finds nothing (SYNTHETIC). Since A372 the payload Reader refuses every read without a screen, a correlation source and an alert (noema#780), so the list
// readers take all three; these tests read lists, and the screen has its own tests in evidence/payloadread.
type listCleanScreen struct{}

func (listCleanScreen) Screen(context.Context, injection.Subject, map[string]string) (injection.Result, error) {
	return injection.Result{}, nil
}

func listCorrelation(context.Context) string { return "cor_SYNTHETIC" }

// listAlerts records the alerts a flagged payload raises: names only, never a value.
type listAlerts struct{ flags []payloadread.Flag }

func (a *listAlerts) ScreenFlagged(_ context.Context, f payloadread.Flag) {
	a.flags = append(a.flags, f)
}
