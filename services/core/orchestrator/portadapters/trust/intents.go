package trustport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

// The store adapters of Trust's intake (STM-01 follow-up; REQ-TRU-003, REQ-AMB-001, REQ-SEC-004; INV-06, INV-11, INV-12, INV-17): intake.Intents over the admitted-intent store
// (migration 0026) and the kill-switch half of intake.Writes. Each reads as the session's own tenant: the tenant is an argument the intake takes from the authenticated session,
// the login is that tenant's own trust_rw login (the database binds the rows to it, A136), and the adapter binds the tenant again in every statement, so neither alone is relied on.

var (
	// ErrNoPool is returned for a tenant that has no login. No other tenant's login is used in its place.
	ErrNoPool = errors.New("trustport: no login is configured for this tenant")
	// ErrStore is returned when the store could not be read. It is an error, never a refusal: the intake ends UNKNOWN, not REJECT (REQ-NFR-002). It carries no driver text, which can
	// name the server and the login (AGENT-13); a context error that caused it is wrapped alongside.
	ErrStore = errors.New("trustport: the store could not be read")
	// ErrStoredIntent is returned for a stored intent the loaded pack does not describe (another pack version, a node it does not know) or whose owned slots do not name one target.
	ErrStoredIntent = errors.New("trustport: the stored intent is not usable")
	// ErrPackData is returned for pack data the adapter cannot trust.
	ErrPackData = errors.New("trustport: the pack data is not usable")
)

// Querier is what the store adapters need of a tenant's own login. *pgxpool.Pool is one.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TenantPools hands out a tenant's own login, or an error: it never falls back to another tenant's login or to a default (the directory of file-mounted per-tenant logins of
// cmd/core is one; PoolsOf adapts a map of pools).
type TenantPools interface {
	For(tenantID string) (Querier, error)
}

type poolMap map[string]*pgxpool.Pool

// PoolsOf makes TenantPools of one pool per tenant. A tenant that is not in the map, or maps to no pool, is ErrNoPool.
func PoolsOf(m map[string]*pgxpool.Pool) TenantPools { return poolMap(maps.Clone(m)) }

func (p poolMap) For(tenantID string) (Querier, error) {
	if pool, ok := p[tenantID]; ok && pool != nil {
		return pool, nil
	}
	return nil, ErrNoPool
}

var (
	intentIDRe    = regexp.MustCompile(`^int_[0-9A-Za-z_]{2,64}$`)
	resourceRefRe = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$`)
	packIDRe      = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	semverRe      = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

// nodeInfo is what the pack says about one intent node that the view of an admitted intent needs: its required slots and the slots the principal must own (the trust profile's
// `authority.principal_must_own`).
type nodeInfo struct {
	required []string
	owned    []string
}

type packData struct {
	PackID      string `json:"pack_id"`
	PackVersion string `json:"pack_version"`
	IntentNodes []struct {
		NodeID       string  `json:"node_id"`
		TrustProfile *string `json:"trust_profile"`
		Slots        []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"slots"`
	} `json:"intent_nodes"`
	TrustProfiles []struct {
		ProfileID string `json:"profile_id"`
		Authority struct {
			PrincipalMustOwn []string `json:"principal_must_own"`
		} `json:"authority"`
	} `json:"trust_profiles"`
}

func packErr(why string) error { return fmt.Errorf("%w: %s", ErrPackData, why) }

// readPack reads what the adapter needs from the pack's data (packs are data, never code: SoT 51; the required slots, the owned slots and the pack's identity are the pack's, never a
// literal here, INV-14). It refuses data it cannot trust: not one JSON object, a pack id or version off its shape, no nodes, a node, slot or profile that is repeated or has no name, a
// profile a node names that the pack lacks, and an owned slot the node does not have.
func readPack(raw []byte) (id, version string, nodes map[string]nodeInfo, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var f packData
	if err := dec.Decode(&f); err != nil {
		return "", "", nil, packErr("the pack is not a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", "", nil, packErr("the pack has data after the object")
	}
	if !packIDRe.MatchString(f.PackID) || !semverRe.MatchString(f.PackVersion) {
		return "", "", nil, packErr("the pack id or version is off its shape")
	}
	if len(f.IntentNodes) == 0 {
		return "", "", nil, packErr("the pack has no intent nodes")
	}
	profiles := map[string][]string{}
	for _, p := range f.TrustProfiles {
		if _, dup := profiles[p.ProfileID]; dup || p.ProfileID == "" {
			return "", "", nil, packErr("a trust profile has no id or is repeated")
		}
		profiles[p.ProfileID] = p.Authority.PrincipalMustOwn
	}
	nodes = map[string]nodeInfo{}
	for _, n := range f.IntentNodes {
		if _, dup := nodes[n.NodeID]; dup || n.NodeID == "" {
			return "", "", nil, packErr("an intent node has no id or is repeated")
		}
		slots := map[string]bool{}
		info := nodeInfo{}
		for _, s := range n.Slots {
			if s.Name == "" || slots[s.Name] {
				return "", "", nil, packErr("a slot has no name or is repeated")
			}
			slots[s.Name] = true
			if s.Required {
				info.required = append(info.required, s.Name)
			}
		}
		if n.TrustProfile != nil {
			owned, ok := profiles[*n.TrustProfile]
			if !ok {
				return "", "", nil, packErr("a node names a trust profile the pack lacks")
			}
			seen := map[string]bool{}
			for _, name := range owned {
				if !slots[name] || seen[name] {
					return "", "", nil, packErr("a trust profile owns a slot its node lacks, or twice")
				}
				seen[name] = true
				info.owned = append(info.owned, name)
			}
		}
		sort.Strings(info.required)
		sort.Strings(info.owned)
		nodes[n.NodeID] = info
	}
	return f.PackID, f.PackVersion, nodes, nil
}

// StoredIntents implements intake.Intents over the admitted-intent store: Get and LatestVersion read the intent of the session's tenant, and Verify reads it again and runs the
// store's own check on it. It also satisfies this package's Intents, the Get Decide needs. Build it with NewStoredIntents.
type StoredIntents struct {
	pools       TenantPools
	timeout     time.Duration
	packID      string
	packVersion string
	nodes       map[string]nodeInfo
}

var (
	_ intake.Intents = (*StoredIntents)(nil)
	_ Intents        = (*StoredIntents)(nil)
)

// NewStoredIntents builds the adapter. What cmd/core must pass:
//
//   - pools: one trust_rw login per tenant, the directory of EnvTrustDSNDir (A228) that cmd/core already loads for the payload reader; each login is the tenant's own, so the
//     database sees only that tenant's rows. Never an administrator's or an orchestrator's login.
//   - pack: the bytes of the pack file the process loaded (packs/fintech_v1.json), the same bytes the Interpreter's gate and `RequiredFromPack` were built from.
//   - timeout: the deadline of one read, from configuration (AGENT-16); it applies on top of the caller's context.
//
// A missing part is ErrConfig and pack data it cannot trust is ErrPackData.
func NewStoredIntents(pools TenantPools, pack []byte, timeout time.Duration) (*StoredIntents, error) {
	if pools == nil || timeout <= 0 {
		return nil, ErrConfig
	}
	id, version, nodes, err := readPack(pack)
	if err != nil {
		return nil, err
	}
	return &StoredIntents{pools: pools, timeout: timeout, packID: id, packVersion: version, nodes: nodes}, nil
}

func (s *StoredIntents) usable() error {
	if s == nil || s.pools == nil || s.timeout <= 0 || s.nodes == nil {
		return ErrConfig
	}
	return nil
}

func storeErr(ctx context.Context) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("%w: %w", ErrStore, cerr)
	}
	return ErrStore
}

func (s *StoredIntents) querier(tenantID string) (Querier, error) {
	if err := s.usable(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, ErrSession
	}
	q, err := s.pools.For(tenantID)
	if err != nil {
		return nil, fmt.Errorf("trustport: the tenant's login: %w", err)
	}
	return q, nil
}

func checkRef(tenantID, intentID string, version int) error {
	switch {
	case tenantID == "":
		return ErrSession
	case !intentIDRe.MatchString(intentID) || version < 1:
		return fmt.Errorf("%w: the intent id or the version", ErrIntentRef)
	}
	return nil
}

// row reads one stored version of the tenant's intent. No row, whoever's it is, is intake.ErrNotFound; a failure to read is ErrStore.
func (s *StoredIntents) row(ctx context.Context, tenantID, intentID string, version int) (intentstore.Admitted, error) {
	if err := checkRef(tenantID, intentID, version); err != nil {
		return intentstore.Admitted{}, err
	}
	q, err := s.querier(tenantID)
	if err != nil {
		return intentstore.Admitted{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var a intentstore.Admitted
	var doc string
	err = q.QueryRow(ctx, `SELECT tenant_id, intent_id, version, COALESCE(supersedes, ''), intent_node, intent_hash, correlation_id, json::text
		FROM admitted_intent WHERE tenant_id = $1 AND intent_id = $2 AND version = $3`, tenantID, intentID, version).
		Scan(&a.TenantID, &a.IntentID, &a.Version, &a.Supersedes, &a.IntentNode, &a.IntentHash, &a.CorrelationID, &doc)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return intentstore.Admitted{}, intake.ErrNotFound
	case err != nil:
		return intentstore.Admitted{}, storeErr(ctx)
	case a.TenantID != tenantID || a.IntentID != intentID || a.Version != version:
		// A store that answers with another row than the one asked for is not an answer; it names nothing of that row.
		return intentstore.Admitted{}, intake.ErrNotFound
	}
	a.JSON = []byte(doc)
	return a, nil
}

// Get reads the admitted intent of the session's tenant at one version and builds what Trust needs of it from the stored document and the pack: the principal the document names, the
// view T7 admits it by, and the one target its owned slots resolve to. An intent that does not exist and another tenant's are intake.ErrNotFound, the same answer (REQ-TRU-003).
// It does not verify: T1 asks Verify next.
func (s *StoredIntents) Get(ctx context.Context, tenantID, intentID string, version int) (intake.AdmittedIntent, error) {
	a, err := s.row(ctx, tenantID, intentID, version)
	if err != nil {
		return intake.AdmittedIntent{}, err
	}
	obj, err := decode(a.JSON)
	if err != nil {
		return intake.AdmittedIntent{}, fmt.Errorf("%w: the document does not decode", ErrStoredIntent)
	}
	return s.build(a, obj)
}

// LatestVersion is the newest stored version of the tenant's intent; an intent the tenant does not have is intake.ErrNotFound.
func (s *StoredIntents) LatestVersion(ctx context.Context, tenantID, intentID string) (int, error) {
	if err := checkRef(tenantID, intentID, 1); err != nil {
		return 0, err
	}
	q, err := s.querier(tenantID)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var v int
	err = q.QueryRow(ctx, `SELECT version FROM admitted_intent WHERE tenant_id = $1 AND intent_id = $2 ORDER BY version DESC LIMIT 1`, tenantID, intentID).Scan(&v)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, intake.ErrNotFound
	case err != nil:
		return 0, storeErr(ctx)
	case v < 1:
		return 0, fmt.Errorf("%w: a version below 1", ErrBadAnswer)
	}
	return v, nil
}

// Verify reads the stored row again and runs the store's own check on it (intentstore.Verify: the document is the canonical form, its hash is the one stored, and the row's
// identity, version, supersedes, correlation, node and status are the document's), and holds the AdmittedIntent Trust carries to the stored one: the same hash, node, correlation,
// principal, target and view. Anything else is intake.ErrInvalid, and so is an intent the store does not hold. The document carries no signature in the POC (A166: the marker
// `unsigned`), so there is none to check; the hash covers the whole document, the marker included.
func (s *StoredIntents) Verify(ctx context.Context, a intake.AdmittedIntent) error {
	row, err := s.row(ctx, a.TenantID, a.IntentID, a.Version)
	if errors.Is(err, intake.ErrNotFound) {
		return fmt.Errorf("%w: the store holds no such intent", intake.ErrInvalid)
	}
	if err != nil {
		return err
	}
	obj, err := decode(row.JSON)
	if err != nil {
		return fmt.Errorf("%w: the document does not decode", intake.ErrInvalid)
	}
	canon, err := canonical(row.JSON)
	if err != nil {
		return fmt.Errorf("%w: the document has no canonical form", intake.ErrInvalid)
	}
	row.JSON = canon // the database returns jsonb's text; the hash covers the canonical form
	if err := intentstore.Verify(row); err != nil {
		return fmt.Errorf("%w: %w", intake.ErrInvalid, err)
	}
	stored, err := s.build(row, obj)
	if err != nil {
		return err
	}
	if !sameIntent(a, stored) {
		return fmt.Errorf("%w: the admitted intent is not the stored one", intake.ErrInvalid)
	}
	return nil
}

func sameIntent(a, b intake.AdmittedIntent) bool {
	return a.TenantID == b.TenantID && a.IntentID == b.IntentID && a.Version == b.Version && a.IntentNode == b.IntentNode && a.IntentHash == b.IntentHash &&
		a.CorrelationID == b.CorrelationID && a.PrincipalID == b.PrincipalID && a.TargetRef == b.TargetRef && a.View.Version == b.View.Version &&
		slices.Equal(a.View.MaterialFlags, b.View.MaterialFlags) && maps.Equal(a.View.RequiredSlots, b.View.RequiredSlots)
}

func decode(text []byte) (contracts.IntentObjectV3, error) {
	var o contracts.IntentObjectV3
	err := json.Unmarshal(text, &o)
	return o, err
}

// canonical is the RFC 8785 form of the stored document, with numbers kept exact; it has none for a number the form refuses.
func canonical(text []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return crypto.CanonicalJSON(v)
}

// build makes the AdmittedIntent of a stored row and its decoded document, for the loaded pack. The identity is the document's, which the gate wrote from the session (INV-12).
// Every ambiguity flag the document carries is material to Trust (the safer reading: the pack's always-material list is not narrowed to, as the orchestrator holds any flag for
// clarification too), a required slot the document lacks is ABSENT, and a status that is not one of the four, or an absent status on any slot but a Reference, is UNRESOLVED.
func (s *StoredIntents) build(a intentstore.Admitted, obj contracts.IntentObjectV3) (intake.AdmittedIntent, error) {
	if string(obj.Pack.PackId) != s.packID || string(obj.Pack.PackVersion) != s.packVersion {
		return intake.AdmittedIntent{}, fmt.Errorf("%w: the intent was admitted under another pack or version", ErrStoredIntent)
	}
	node, ok := s.nodes[a.IntentNode]
	if !ok {
		return intake.AdmittedIntent{}, fmt.Errorf("%w: the pack does not describe the intent node", ErrStoredIntent)
	}
	principal := string(obj.Principal.PrincipalId)
	if principal == "" {
		return intake.AdmittedIntent{}, fmt.Errorf("%w: the document names no principal", ErrStoredIntent)
	}
	required := make(map[string]decide.SlotStatus, len(node.required))
	for _, name := range node.required {
		raw, present := obj.Slots[name]
		if !present {
			required[name] = decide.SlotAbsent
			continue
		}
		required[name] = statusOf(json.RawMessage(raw))
	}
	target, err := s.target(node, obj)
	if err != nil {
		return intake.AdmittedIntent{}, err
	}
	var flags []string
	if len(obj.AmbiguityFlags) > 0 {
		flags = slices.Clone(obj.AmbiguityFlags)
		sort.Strings(flags)
	}
	return intake.AdmittedIntent{TenantID: a.TenantID, IntentID: a.IntentID, Version: a.Version, IntentNode: a.IntentNode, IntentHash: a.IntentHash, CorrelationID: a.CorrelationID,
		PrincipalID: principal, TargetRef: target, View: decide.IntentView{Version: a.Version, MaterialFlags: flags, RequiredSlots: required}}, nil
}

// statusOf reads a slot's status as the Interpreter port does (EL's read of noema#661): a `status` that is the string RESOLVED is resolved, another of the three words is that, and
// anything else, an unreadable slot included, is unresolved; a slot with no `status` is resolved only when it is a Reference, the one type the contract gives none.
func statusOf(raw json.RawMessage) decide.SlotStatus {
	var head map[string]json.RawMessage
	if json.Unmarshal(raw, &head) != nil || head == nil {
		return decide.SlotUnresolved
	}
	var word string
	if st, has := head["status"]; has {
		if json.Unmarshal(st, &word) != nil {
			return decide.SlotUnresolved
		}
		switch decide.SlotStatus(word) {
		case decide.SlotResolved, decide.SlotAmbiguous, decide.SlotAbsent:
			return decide.SlotStatus(word)
		}
		return decide.SlotUnresolved
	}
	if json.Unmarshal(head["type"], &word) == nil && word == string(contracts.IntentSlotV2SlotTypeReference) {
		return decide.SlotResolved
	}
	return decide.SlotUnresolved
}

// target is the one target the node's owned slots resolve to: the `resolved_ref` of each owned slot that is resolved. None resolved is no target; the same reference twice is that
// one; two different references are ErrStoredIntent, because the port has one TargetRef and T14 lets a step act on that target alone. A resolved owned slot with no usable reference
// is ErrStoredIntent too: T8 would find nothing to check ownership of, and the intake treats an intent with no target as owning nothing to check.
func (s *StoredIntents) target(node nodeInfo, obj contracts.IntentObjectV3) (string, error) {
	var refs []string
	for _, name := range node.owned {
		raw, present := obj.Slots[name]
		if !present || statusOf(json.RawMessage(raw)) != decide.SlotResolved {
			continue
		}
		var head struct {
			ResolvedRef string `json:"resolved_ref"`
		}
		if json.Unmarshal(raw, &head) != nil || !resourceRefRe.MatchString(head.ResolvedRef) {
			return "", fmt.Errorf("%w: a resolved owned slot names no target", ErrStoredIntent)
		}
		if !slices.Contains(refs, head.ResolvedRef) {
			refs = append(refs, head.ResolvedRef)
		}
	}
	switch len(refs) {
	case 0:
		return "", nil
	case 1:
		return refs[0], nil
	}
	return "", fmt.Errorf("%w: the owned slots name more than one target", ErrStoredIntent)
}
