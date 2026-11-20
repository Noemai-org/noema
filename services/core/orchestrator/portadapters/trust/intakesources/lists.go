package intakesources

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// statusKnown is the one fact status of an evidence item (enums.v1 FactStatus) that a list may be read from: any other is not an answer (INV-19).
const statusKnown = "KNOWN"

// snapshotIDRe is the form migration 0020 gives context_snapshot.snapshot_id.
var snapshotIDRe = regexp.MustCompile(`\Aevs_[0-9A-Za-z]{2,32}\z`)

// SnapshotItem is one evidence item of a sealed snapshot, as stored: what is needed to find the item that holds a list and to read its payload as the one that was sealed.
type SnapshotItem struct {
	EvidenceID    string
	Source        string
	ResourceScope string
	Status        string // the stored fact status, not checked here against the enum
	PayloadRef    string
	PayloadHash   string
}

// SnapshotItems reads the items of one sealed snapshot of a tenant. A snapshot that does not exist, that is not sealed, or that another tenant owns is ErrNoSnapshot, the same answer
// for all three; a snapshot that is not exactly what was sealed is ErrSnapshotIntegrity. DBSnapshotItems is the real one.
type SnapshotItems interface {
	Items(ctx context.Context, tenantID, snapshotID string) ([]SnapshotItem, error)
}

// ListMapper reads the rows of a list entity of a source from one stored payload (the evidence mapping's list entities, ruling A438). *mapping.Mapping is the real one: build it with
// mapping.WithScope from the pack's evidence descriptors, or it refuses its list rows.
type ListMapper interface {
	ListRows(source, entity string, payload map[string]any) ([]predicate.Row, error)
}

// ListSpec says which item of a sealed snapshot holds one list entity: the item of this source whose resource scope starts with the prefix. The composition takes both from the pack and
// from the convention the evidence read gives its items; this package names neither.
type ListSpec struct {
	Source      string
	ScopePrefix string
}

// EvidenceLists is what the readers of list entities need. Entities names the list entities the readers serve; each other entity has no source here and is refused by name.
type EvidenceLists struct {
	Mapping  ListMapper
	Items    SnapshotItems
	Payloads payloadread.PayloadReader
	Entities map[string]ListSpec
}

// valid refuses a configuration that cannot read a list safely: a missing part, an entity with no name, source or prefix, and two entities of one source whose prefixes overlap, so
// that one item could be taken for either.
func (l EvidenceLists) valid() error {
	if l.Mapping == nil || l.Items == nil || l.Payloads == nil || len(l.Entities) == 0 {
		return fmt.Errorf("%w: the lists need a mapping, a snapshot reader, a payload reader and at least one entity", ErrConfig)
	}
	for name, spec := range l.Entities {
		if name == "" || spec.Source == "" || spec.ScopePrefix == "" {
			return fmt.Errorf("%w: a list entity needs a name, a source and a scope prefix", ErrConfig)
		}
		for other, o := range l.Entities {
			if other != name && o.Source == spec.Source && strings.HasPrefix(o.ScopePrefix, spec.ScopePrefix) {
				return fmt.Errorf("%w: the scope prefixes of two list entities of one source overlap", ErrConfig)
			}
		}
	}
	return nil
}

// own is a copy that the caller cannot change afterwards.
func (l EvidenceLists) own() EvidenceLists {
	l.Entities = maps.Clone(l.Entities)
	return l
}

// checkSnapshot refuses a tenant or a snapshot ID outside its shape, before any login is asked.
func checkSnapshot(tenantID, snapshotID string) error {
	if !chain.ValidTenantID(tenantID) || !snapshotIDRe.MatchString(snapshotID) {
		return fmt.Errorf("%w: the tenant or the snapshot is outside its shape", ErrQuery)
	}
	return nil
}

// rows gives the rows of one list entity from the items of a sealed snapshot. Fail closed (INV-19): the item that holds the list must be there exactly once and be KNOWN, its payload must
// be the one the snapshot sealed, and the mapping must read it as a list marked complete. Only a KNOWN item with a complete empty list gives no row, and that is "no such row". On any
// error there are no rows.
func (l EvidenceLists) rows(ctx context.Context, tenantID string, items []SnapshotItem, entity string) ([]predicate.Row, error) {
	spec, ok := l.Entities[entity]
	if !ok {
		return nil, fmt.Errorf("%w: the entity %s", ErrNoSource, entity)
	}
	var held []SnapshotItem
	for _, it := range items {
		if it.Source == spec.Source && strings.HasPrefix(it.ResourceScope, spec.ScopePrefix) {
			held = append(held, it)
		}
	}
	if len(held) != 1 {
		return nil, fmt.Errorf("%w: the list %s has %d items in the snapshot, not one", ErrListItem, entity, len(held))
	}
	it := held[0]
	if it.Status != statusKnown {
		return nil, fmt.Errorf("%w: the item of the list %s is not KNOWN", ErrListItem, entity)
	}
	payload, err := payloadread.Reader{Payloads: l.Payloads}.Payload(ctx, tenantID, layer.Item{EvidenceID: it.EvidenceID, PayloadRef: it.PayloadRef, PayloadHash: it.PayloadHash})
	if err != nil {
		return nil, wrapped(ErrListPayload, err)
	}
	rows, err := l.Mapping.ListRows(spec.Source, entity, payload)
	if err != nil {
		return nil, wrapped(ErrList, err)
	}
	return rows, nil
}
