package portread

// The case list and the complaint_ref list (TRU-07 portread slice; ruling A461, interim, relayed, not yet committed; A438; REQ-CTX-001, REQ-CTX-007, REQ-RES-001;
// INV-12, INV-19, INV-21; AGENT-09).
//
// The stand-in's case list is mock-case's CasePage: its items and its own `complete` marker. The complaint_ref list is its own list, read from FindComplaints, with its own
// marker. Each is recorded as one evidence item whose payload is `{"items": [...], "complete": <bool>}`, the keys the evidence mapping's list entities read (the pack's evidence mapping,
// ListRows). A list is KNOWN only when its own marker says it is the whole answer: an incomplete list is UNKNOWN, never an empty "none" (INV-19). An answer the reader cannot use
// is an UNKNOWN item with no payload, not an error and not a guess.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
)

// CaseSources are the two list sources of a case read: the case list and the complaint list, as the pack's evidence descriptors give them. They may name the same source id; the
// resource kinds tell the two lists apart.
type CaseSources struct{ Case, Complaint Source }

func (r *Reader) casesConfigured() error {
	switch {
	case r.Cases == nil || r.CaseRecords == nil || r.Rec == nil || r.Clock == nil:
		return errors.New(prefix + "the reader is not configured for the case lists")
	case r.ReadTimeout <= 0:
		return errors.New(prefix + "the reader needs a positive read timeout from configuration")
	case r.MaxPayloadBytes <= 0:
		return errors.New(prefix + "the reader needs a positive list size limit from configuration")
	case !readByAllowed[r.ReadBy]:
		return fmt.Errorf("%sread_by %q is not allowed", prefix, r.ReadBy)
	}
	for _, s := range []Source{r.CaseSources.Case, r.CaseSources.Complaint} {
		if err := s.check(); err != nil {
			return err
		}
	}
	return nil
}

// ReadCases reads the case list of one transaction and, from a usable complete case list, the complaint list of those cases, and records one evidence item for each. The scope is the
// authenticated session's tenant and principal, passed to the ports unchanged. A port that cannot answer is an item whose status says so, not an error. No complaint item is recorded
// when there is no case to read complaints for (the case read failed, or its list is incomplete, unusable or empty): the port has no global complaint search, and an absent item is not a
// fact. An error is returned only for a request or configuration the reader refuses before reading, and for a failure to record (nothing further is read after it, EC-E16); the items
// recorded before that failure are returned with the error.
func (r *Reader) ReadCases(ctx context.Context, scope adapters.Scope, correlationID, transactionRef string) ([]layer.Item, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New(prefix + "the context needs a deadline")
	}
	if err := r.casesConfigured(); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !principalRe.MatchString(scope.PrincipalID) {
		return nil, fmt.Errorf("%sthe session's principal does not match the contract", prefix)
	}
	if !recordRefRe.MatchString(transactionRef) {
		return nil, fmt.Errorf("%sthe transaction reference does not match the contract", prefix)
	}
	if correlationID == "" {
		return nil, errors.New(prefix + "a correlation id is required")
	}
	base := func(s Source) layer.Observation {
		return layer.Observation{TenantID: scope.TenantID, CorrelationID: correlationID, Source: s.ID, SourceRecordID: transactionRef, ReadBy: r.ReadBy,
			AuthorityClass: authorityClass(s), ResourceScope: s.ResourceKind + ":" + transactionRef, PrincipalScope: scope.PrincipalID, Sensitivity: s.Sensitivity,
			Src: layer.Source{Authority: s.Authority, SourceLimit: s.Limit}, DataSource: r.DataSource, Synthetic: r.Synthetic}
	}
	var items []layer.Item
	obs, caseIDs := r.caseList(ctx, scope, transactionRef, base(r.CaseSources.Case))
	item, err := r.Rec.Record(ctx, obs)
	if err != nil {
		return items, fmt.Errorf("%srecord: %w", prefix, err)
	}
	items = append(items, item)
	if len(caseIDs) == 0 {
		return items, nil
	}
	item, err = r.Rec.Record(ctx, r.complaintList(ctx, scope, caseIDs, base(r.CaseSources.Complaint)))
	if err != nil {
		return items, fmt.Errorf("%srecord: %w", prefix, err)
	}
	return append(items, item), nil
}

// pageSound is what every page must satisfy: its marker agrees with its cursor (complete has none, incomplete has one) and its provenance can back an item.
func (r *Reader) pageProvenance(rawHash string) adapters.Provenance {
	return adapters.Provenance{RawHash: rawHash, DataSource: r.DataSource, Synthetic: r.Synthetic}
}

func markerAgrees(complete bool, nextCursor string) bool { return complete == (nextCursor == "") }

func parseInstant(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s) // a zone is required: an instant, not a wall clock
	return t, err == nil
}

// sized refuses a payload over the configured limit: it is marshalled once, exactly as the Builder will seal it.
func (r *Reader) sized(payload map[string]any) bool {
	raw, err := json.Marshal(payload)
	return err == nil && len(raw) <= r.MaxPayloadBytes
}

// caseList reads and records-ready the case list. It returns the ids of the cases only when the list is usable and marked complete and has at least one case.
func (r *Reader) caseList(ctx context.Context, scope adapters.Scope, ref string, o layer.Observation) (layer.Observation, []string) {
	cctx, cancel := r.call(ctx)
	defer cancel()
	page, err := r.Cases.FindCases(cctx, adapters.CaseQuery{TenantID: scope.TenantID, PrincipalID: scope.PrincipalID, TransactionRef: ref})
	if err != nil {
		return r.failed(o, err), nil
	}
	prov := r.pageProvenance(page.RawHash)
	if !markerAgrees(page.Complete, page.NextCursor) {
		return r.malformed(o, prov), nil
	}
	recs := append([]adapters.CaseRecord(nil), page.Items...)
	sort.Slice(recs, func(i, j int) bool { return recs[i].CaseID < recs[j].CaseID })
	rows := make([]any, 0, len(recs))
	ids := make([]string, 0, len(recs))
	var latest time.Time
	for i, c := range recs {
		at, ok := parseInstant(c.CreatedAt)
		switch {
		case !ok, !recordRefRe.MatchString(c.CaseID), c.Status == "", c.TenantID != scope.TenantID, c.PrincipalID != scope.PrincipalID, c.TransactionRef != ref,
			i > 0 && recs[i-1].CaseID == c.CaseID:
			return r.malformed(o, prov), nil
		}
		if at.After(latest) {
			latest = at
		}
		ids = append(ids, c.CaseID)
		rows = append(rows, map[string]any{"case_id": c.CaseID, "reference": c.Reference, "status": c.Status, "owner_queue": c.OwnerQueue, "priority": c.Priority,
			"transaction_ref": c.TransactionRef, "reason_code": c.ReasonCode, "created_by_intent": c.CreatedByIntent, "epoch": c.Epoch, "created_at": c.CreatedAt})
	}
	payload := map[string]any{"items": rows, "complete": page.Complete}
	if !r.sized(payload) {
		return r.malformed(o, prov), nil
	}
	if latest.IsZero() {
		latest = r.Clock.Now()
	}
	o = r.answered(o, prov, latest, page.Complete, payload) // an empty list marked complete is "no case"; any other unfinished list is UNKNOWN
	if o.Read.Class != layer.ReadOK || !page.Complete {
		return o, nil
	}
	return o, ids
}

// complaintList reads the complaints of each case (one read per case, in case-id order) and merges them into one list that is complete only if every read was.
func (r *Reader) complaintList(ctx context.Context, scope adapters.Scope, caseIDs []string, o layer.Observation) layer.Observation {
	rows := []any{}
	complete := true
	var latest time.Time
	var hashes []string
	for _, caseID := range caseIDs {
		page, err := r.readComplaints(ctx, scope, caseID)
		if err != nil {
			return r.failed(o, err)
		}
		hashes = append(hashes, page.RawHash)
		prov := r.pageProvenance(page.RawHash)
		if !markerAgrees(page.Complete, page.NextCursor) {
			return r.malformed(o, prov)
		}
		recs := append([]adapters.ComplaintRecord(nil), page.Items...)
		sort.Slice(recs, func(i, j int) bool { return recs[i].ComplaintRef < recs[j].ComplaintRef })
		for i, c := range recs {
			started, ok1 := parseInstant(c.ClockStartedAt)
			logged, ok2 := parseInstant(c.LoggedAt)
			switch {
			case !ok1, !ok2, !recordRefRe.MatchString(c.ComplaintRef), c.CaseID != caseID, c.TenantID != scope.TenantID, i > 0 && recs[i-1].ComplaintRef == c.ComplaintRef:
				return r.malformed(o, prov)
			}
			latest = laterOf(latest, started, logged)
			rows = append(rows, map[string]any{"complaint_ref": c.ComplaintRef, "case_id": c.CaseID, "logged_at": c.LoggedAt, "clock_started_at": c.ClockStartedAt})
		}
		complete = complete && page.Complete
	}
	// One page's hash is the hash of the bytes received; several are combined into one digest of their hashes, in case-id order, so the item names every page it was made from.
	rawHash := hashes[0]
	if len(hashes) > 1 {
		sum := sha256.Sum256([]byte(strings.Join(hashes, "\n")))
		rawHash = "sha256:" + hex.EncodeToString(sum[:])
	}
	prov := r.pageProvenance(rawHash)
	payload := map[string]any{"items": rows, "complete": complete}
	if !r.sized(payload) {
		return r.malformed(o, prov)
	}
	if latest.IsZero() {
		latest = r.Clock.Now()
	}
	return r.answered(o, prov, latest, complete, payload)
}

func laterOf(a time.Time, others ...time.Time) time.Time {
	for _, t := range others {
		if t.After(a) {
			a = t
		}
	}
	return a
}

func (r *Reader) readComplaints(ctx context.Context, scope adapters.Scope, caseID string) (adapters.ComplaintPage, error) {
	cctx, cancel := r.call(ctx)
	defer cancel()
	return r.CaseRecords.FindComplaints(cctx, adapters.RecordQuery{TenantID: scope.TenantID, CaseID: caseID})
}
