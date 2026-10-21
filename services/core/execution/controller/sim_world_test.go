// The deterministic simulation for the spend-and-dispatch protocol (D7, PERD section 27.3). It is test support:
// World implements the controller's ports in memory, with the guarantees the real ledger gets from a SERIALIZABLE
// Postgres transaction. It is NOT a substitute for the real Postgres and mock-case runs. Everything is SYNTHETIC.
package controller_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// Fixed synthetic identifiers: one logical action on one transaction.
const (
	TenantID    = "ten_sim"
	PrincipalID = "usr_sim"
	KeyID       = "sim-key-1"
	Capability  = "sim.cap.create_case"
	TargetRef   = "sim.transaction:TX1"
)

// RecordEntry is one appended Record event, hash-chained to the one before.
type RecordEntry struct {
	Seq     int
	Type    string
	GrantID string
	Detail  string
	Hash    string
}

// Dispatch is one write attempt as the target saw it.
type Dispatch struct {
	Key     string
	GrantID string
	Attempt int64
	// RecordedFirst is true when the idempotency record and DISPATCH_ATTEMPTED existed before the call.
	RecordedFirst bool
	AtSeq         int
}

type idemRecord struct {
	Key      string
	Action   string
	Attempts []attempt
}

type attempt struct {
	GrantID string
	Number  int64
}

// FaultMode is what the network does to one dispatch.
type FaultMode int

// The modes. Only some apply the effect at the target.
const (
	FaultNone            FaultMode = iota // delivered, acknowledged
	FaultLostResponse                     // delivered and applied, response lost: timeout
	FaultDroppedRequest                   // never delivered: timeout
	FaultServerErrBefore                  // 5xx, not applied
	FaultServerErrAfter                   // applied, then 5xx
	FaultRejected                         // 4xx, not applied
	FaultConnReset                        // applied or not, the caller sees a reset (applied)
	faultCount
)

// World is the whole simulated environment.
type World struct {
	mu    sync.Mutex
	Clock *clock.Manual
	Keys  *crypto.KeyRing

	// Authoritative state read by E3 to E7 (the test mutates these).
	IntentHash      string
	DecisionAllowed bool
	KillSwitch      bool
	TargetVersion   string
	RevalChanged    []string

	// Ledger state.
	spentGrants map[string]bool
	spentNonces map[string]bool
	voids       map[string]controller.VoidReason
	idem        map[string]*idemRecord
	receipts    map[string]controller.Receipt // grantID#attempt
	Record      []RecordEntry
	Dispatches  []Dispatch
	Recovery    []string // grants that produced a RecoveryRequest

	// Target (the case system): effects per key.
	Effects map[string]int
	Refs    map[string]string

	// Network.
	faults func() FaultMode // next fault for each dispatch
	nextID int
	stored map[string]controller.SignedGrant
}

// NewWorld builds a world with a reliable network. SetFaults changes it.
func NewWorld(secret []byte) *World {
	w := &World{
		Clock:           clock.NewManual(time.Date(2026, 10, 1, 9, 50, 0, 0, time.UTC)),
		Keys:            crypto.NewKeyRing(map[string][]byte{KeyID: secret}),
		IntentHash:      hashOf("intent-1"),
		DecisionAllowed: true,
		TargetVersion:   "v_1",
		spentGrants:     map[string]bool{}, spentNonces: map[string]bool{}, voids: map[string]controller.VoidReason{},
		idem: map[string]*idemRecord{}, receipts: map[string]controller.Receipt{}, Effects: map[string]int{}, Refs: map[string]string{},
	}
	w.faults = func() FaultMode { return FaultNone }
	return w
}

// SetFaults installs the function that picks the fault of each dispatch.
func (w *World) SetFaults(f func() FaultMode) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.faults = f
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PlanParams are the parameters of the one write; its hash is bound into every grant.
func PlanParams() map[string]any {
	return map[string]any{"subject": TargetRef, "reason": "CREDIT_MISSING", "queue": "SYNTH_QUEUE"}
}

// PlanFor is the stored plan step every grant of the simulation names.
func (w *World) PlanFor() controller.PlanStep {
	return controller.PlanStep{PlanHash: hashOf("plan-1"), Step: 1, CapabilityID: Capability, Registered: true, AdapterExists: true,
		Params: PlanParams(), TimeoutMS: 3000, Conditional: true,
		KeyFields: controller.KeyFields{TransactionID: "TX1", LogicalAction: "sim.action.create_case", ReasonCode: "CREDIT_MISSING", PolicyScope: "sim.pol", LogicalResolutionEpoch: "0"}}
}

// IssueGrant stands in for Trust: it signs a fresh grant for the plan. n makes the grant ID and nonce unique.
func (w *World) IssueGrant(n int, notBefore, expires time.Time) controller.SignedGrant {
	ph, err := crypto.HashOf(PlanParams())
	if err != nil {
		panic(err)
	}
	g := controller.Grant{SchemaVersion: "1.0", GrantID: fmt.Sprintf("grt_%d", n), DecisionID: "dec_1", PrincipalID: PrincipalID,
		IntentHash: w.IntentHash, CapabilityID: Capability, TargetRef: TargetRef, ParamsHash: ph, EvidenceSetHash: hashOf("evidence-1"),
		PlanHash: hashOf("plan-1"), PlanStep: 1, TargetVersions: []controller.TargetVersion{{ResourceRef: "sim.case_scope:TX1", Source: "sim.src.case", VersionToken: "v_1"}},
		PolicyVersion: "sim.pol@1.0", PackVersion: "sim@1.0.0", NotBefore: notBefore.UTC().Format(time.RFC3339), ExpiresAt: expires.UTC().Format(time.RFC3339),
		Nonce: fmt.Sprintf("n_%04x", n), SingleUse: true}
	canon, err := crypto.CanonicalJSON(g)
	if err != nil {
		panic(err)
	}
	sig, err := w.Keys.SignWith(KeyID, canon)
	if err != nil {
		panic(err)
	}
	return controller.SignedGrant{Grant: g, Sig: controller.Sig{KeyID: KeyID, Value: sig}}
}

// ---- GrantSource ----

// Stored grants, by ID.
var _ controller.GrantSource = (*World)(nil)

// Store registers a grant so Load finds it.
func (w *World) Store(g controller.SignedGrant) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stored == nil {
		w.stored = map[string]controller.SignedGrant{}
	}
	w.stored[g.Grant.GrantID] = g
}

// Load implements controller.GrantSource.
func (w *World) Load(_ context.Context, _, grantID string) (controller.SignedGrant, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	g, ok := w.stored[grantID]
	if !ok {
		return controller.SignedGrant{}, fmt.Errorf("grant %s not found", grantID)
	}
	return g, nil
}

// ---- Binder and Revalidator ----

// LatestIntentHash implements controller.Binder.
func (w *World) LatestIntentHash(context.Context, controller.Grant) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.IntentHash, nil
}

// DecisionStillAllowed implements controller.Binder.
func (w *World) DecisionStillAllowed(context.Context, controller.Grant) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.DecisionAllowed, nil
}

// Plan implements controller.Binder.
func (w *World) Plan(context.Context, controller.Grant) (controller.PlanStep, error) {
	return w.PlanFor(), nil
}

// WritesEnabled implements controller.Binder.
func (w *World) WritesEnabled(context.Context, string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.KillSwitch, nil
}

// Revalidate implements controller.Revalidator: it fails when the target version moved or a fact changed.
func (w *World) Revalidate(_ context.Context, g controller.Grant, _ controller.PlanStep) (controller.Revalidation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	changed := append([]string(nil), w.RevalChanged...)
	if len(g.TargetVersions) > 0 && g.TargetVersions[0].VersionToken != w.TargetVersion {
		changed = append(changed, "target_version")
	}
	return controller.Revalidation{Passed: len(changed) == 0, ChangedFacts: changed}, nil
}

// ---- IDs ----

// ReceiptID implements controller.IDs with a counter (the simulation is deterministic).
func (w *World) ReceiptID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextID++
	return fmt.Sprintf("rcp_%d", w.nextID)
}

// ---- Ledger ----

func (w *World) appendLocked(typ, grantID, detail string) {
	prev := ""
	if n := len(w.Record); n > 0 {
		prev = w.Record[n-1].Hash
	}
	sum := sha256.Sum256([]byte(prev + "|" + typ + "|" + grantID + "|" + detail))
	w.Record = append(w.Record, RecordEntry{Seq: len(w.Record) + 1, Type: typ, GrantID: grantID, Detail: detail, Hash: hex.EncodeToString(sum[:])})
}

// Append implements controller.Ledger.
func (w *World) Append(_ context.Context, _ string, events ...controller.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range events {
		w.appendLocked(e.Type, e.GrantID, e.Detail)
	}
	return nil
}

// Spend implements controller.Ledger: all or nothing, under one lock.
func (w *World) Spend(_ context.Context, cmd controller.SpendCommand) (controller.SpendResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.KillSwitch {
		return controller.SpendResult{WritesDisabled: true}, nil
	}
	if w.spentGrants[cmd.Grant.GrantID] || w.spentNonces[cmd.Grant.Nonce] || w.voids[cmd.Grant.GrantID] != "" {
		return controller.SpendResult{Replay: true}, nil
	}
	rec := w.idem[cmd.IdempotencyKey]
	if rec == nil {
		rec = &idemRecord{Key: cmd.IdempotencyKey, Action: cmd.LogicalAction}
		w.idem[cmd.IdempotencyKey] = rec
	}
	n := int64(len(rec.Attempts) + 1)
	rec.Attempts = append(rec.Attempts, attempt{GrantID: cmd.Grant.GrantID, Number: n})
	w.spentGrants[cmd.Grant.GrantID], w.spentNonces[cmd.Grant.Nonce] = true, true
	w.appendLocked("GRANT_SPENT", cmd.Grant.GrantID, cmd.IdempotencyKey)
	w.appendLocked("DISPATCH_ATTEMPTED", cmd.Grant.GrantID, fmt.Sprintf("%s#%d", cmd.IdempotencyKey, n))
	return controller.SpendResult{Spent: true, Attempt: n}, nil
}

// Void implements controller.Ledger: once per grant.
func (w *World) Void(_ context.Context, g controller.Grant, reason controller.VoidReason) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.voids[g.GrantID] != "" || w.spentGrants[g.GrantID] {
		return nil
	}
	w.voids[g.GrantID] = reason
	w.appendLocked("GRANT_VOIDED", g.GrantID, string(reason))
	w.Recovery = append(w.Recovery, g.GrantID)
	return nil
}

// Complete implements controller.Ledger: the first receipt for a grant and attempt wins.
func (w *World) Complete(_ context.Context, r controller.Receipt) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := fmt.Sprintf("%s#%d", r.GrantID, r.Attempt)
	if _, exists := w.receipts[id]; exists {
		return false, nil
	}
	w.receipts[id] = r
	w.appendLocked("RECEIPT", r.GrantID, string(r.State))
	return true, nil
}

// Dispatching implements controller.Ledger.
func (w *World) Dispatching(context.Context, string) ([]controller.Pending, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []controller.Pending
	for _, rec := range w.idem {
		for _, a := range rec.Attempts {
			if _, done := w.receipts[fmt.Sprintf("%s#%d", a.GrantID, a.Number)]; !done {
				out = append(out, controller.Pending{GrantID: a.GrantID, TenantID: TenantID, IdempotencyKey: rec.Key, LogicalAction: rec.Action, Attempt: a.Number})
			}
		}
	}
	return out, nil
}

// ---- Dispatcher: the target and the network ----

// Dispatch implements controller.Dispatcher. The target applies a create once per idempotency key; a repeat
// returns the existing reference (a 409 duplicate or a 200 with the original, REQ-EXE-007).
func (w *World) Dispatch(_ context.Context, req controller.DispatchRequest) controller.TransportResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	grantID := ""
	if rec := w.idem[req.IdempotencyKey]; rec != nil && int(req.Attempt) <= len(rec.Attempts) {
		grantID = rec.Attempts[req.Attempt-1].GrantID
	}
	recorded := false
	for _, e := range w.Record {
		if e.Type == "DISPATCH_ATTEMPTED" && e.GrantID == grantID {
			recorded = true
		}
	}
	w.Dispatches = append(w.Dispatches, Dispatch{Key: req.IdempotencyKey, GrantID: grantID, Attempt: req.Attempt, RecordedFirst: recorded && grantID != "", AtSeq: len(w.Record)})

	mode := w.faults()
	stale := len(req.ExpectedFrom) > 0 && req.ExpectedFrom[0].VersionToken != w.TargetVersion
	if stale {
		return controller.TransportResult{Class: controller.TransportVersionConflict, RawCode: "409"}
	}
	apply := func() string {
		if ref, ok := w.Refs[req.IdempotencyKey]; ok {
			return ref
		}
		w.Effects[req.IdempotencyKey]++
		ref := fmt.Sprintf("CMP-%d", len(w.Refs)+1)
		w.Refs[req.IdempotencyKey] = ref
		return ref
	}
	switch mode {
	case FaultDroppedRequest:
		return controller.TransportResult{Class: controller.TransportTimeout}
	case FaultServerErrBefore:
		return controller.TransportResult{Class: controller.TransportServer5xx, RawCode: "503"}
	case FaultRejected:
		return controller.TransportResult{Class: controller.TransportRejected4xx, RawCode: "422"}
	case FaultLostResponse:
		apply()
		return controller.TransportResult{Class: controller.TransportTimeout}
	case FaultServerErrAfter:
		apply()
		return controller.TransportResult{Class: controller.TransportServer5xx, RawCode: "500"}
	case FaultConnReset:
		apply()
		return controller.TransportResult{Class: controller.TransportConnectionReset}
	}
	existing := w.Refs[req.IdempotencyKey] != ""
	ref := apply()
	if existing {
		return controller.TransportResult{Class: controller.TransportDuplicate409, ExternalAck: ref, RawCode: "409"}
	}
	return controller.TransportResult{Class: controller.TransportAck2xx, ExternalAck: ref, RawCode: "201"}
}

// ---- inspection (the truth the tests check) ----

// Snapshot is a consistent copy of what the properties read.
type Snapshot struct {
	Effects    map[string]int
	Dispatches []Dispatch
	Record     []RecordEntry
	Receipts   map[string]controller.Receipt
	Spent      map[string]bool
	Pending    int
	Voids      map[string]controller.VoidReason
	Attempts   map[string][]string // key -> grant IDs in attempt order
}

// Snapshot copies the state under the lock.
func (w *World) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := Snapshot{Effects: map[string]int{}, Receipts: map[string]controller.Receipt{}, Spent: map[string]bool{}, Voids: map[string]controller.VoidReason{}, Attempts: map[string][]string{}}
	for k, v := range w.Effects {
		s.Effects[k] = v
	}
	for k, v := range w.receipts {
		s.Receipts[k] = v
	}
	for k, v := range w.spentGrants {
		s.Spent[k] = v
	}
	for k, v := range w.voids {
		s.Voids[k] = v
	}
	for k, rec := range w.idem {
		for _, a := range rec.Attempts {
			s.Attempts[k] = append(s.Attempts[k], a.GrantID)
			if _, done := w.receipts[fmt.Sprintf("%s#%d", a.GrantID, a.Number)]; !done {
				s.Pending++
			}
		}
	}
	s.Dispatches = append(s.Dispatches, w.Dispatches...)
	s.Record = append(s.Record, w.Record...)
	return s
}

// RecordVerdict appends the stand-in for an Outcome verdict, which a second attempt must follow (REQ-EXE-006).
func (w *World) RecordVerdict(key, verdict string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.appendLocked("VERDICT_STANDIN", "", key+":"+verdict)
}

// Truth is the case-system effect count for the key, read without any fault (the oracle-free truth endpoint).
func (w *World) Truth(key string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Effects[key]
}

// Mutate runs f under the lock, for the test's changes to the authoritative state.
func (w *World) Mutate(f func(*World)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	f(w)
}

// ChainValid recomputes the Record hash chain.
func ChainValid(rec []RecordEntry) bool {
	prev := ""
	for i, e := range rec {
		sum := sha256.Sum256([]byte(prev + "|" + e.Type + "|" + e.GrantID + "|" + e.Detail))
		if e.Seq != i+1 || e.Hash != hex.EncodeToString(sum[:]) {
			return false
		}
		prev = e.Hash
	}
	return true
}
