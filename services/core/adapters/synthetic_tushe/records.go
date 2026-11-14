package synthetic_tushe

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// The evidence and complaint half of CaseClient (EXE-05, ruling A157; adapters.CaseRecords). Same rules as the case half: one request per write and
// never a retry (REQ-EXE-006), the target's answer as a transport class with its raw status (REQ-EXE-007), a 2xx body that is not exactly the
// documented record for THIS request is UNKNOWN (EVD-05), and no method can change, delete or close anything.

var _ adapters.CaseRecords = (*CaseClient)(nil)

// hashShape is the payload_hash of the target: "sha256:" and 64 lower-case hex characters.
var hashShape = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type wireEvidence struct {
	AttachmentID   string `json:"attachment_id"`
	CaseID         string `json:"case_id"`
	PayloadHash    string `json:"payload_hash"`
	IdempotencyKey string `json:"idempotency_key"`
	AttachedAt     string `json:"attached_at"`
	TenantID       string `json:"tenant_id"`
	Payload        string `json:"payload"`
}

type wireComplaint struct {
	ComplaintRef   string `json:"complaint_ref"`
	CaseID         string `json:"case_id"`
	IdempotencyKey string `json:"idempotency_key"`
	LoggedAt       string `json:"logged_at"`
	ClockStartedAt string `json:"clock_started_at"`
	TenantID       string `json:"tenant_id"`
}

type wireEvidencePage struct {
	Items      []wireEvidence `json:"items"`
	Complete   *bool          `json:"complete"`
	NextCursor string         `json:"next_cursor"`
}

type wireComplaintPage struct {
	Items      []wireComplaint `json:"items"`
	Complete   *bool           `json:"complete"`
	NextCursor string          `json:"next_cursor"`
}

func (w wireEvidence) record() (adapters.EvidenceRecord, bool) {
	ok := w.AttachmentID != "" && w.CaseID != "" && hashShape.MatchString(w.PayloadHash) && w.IdempotencyKey != "" && w.AttachedAt != "" && w.TenantID != ""
	return adapters.EvidenceRecord{AttachmentID: w.AttachmentID, CaseID: w.CaseID, PayloadHash: w.PayloadHash, IdempotencyKey: w.IdempotencyKey,
		AttachedAt: w.AttachedAt, TenantID: w.TenantID, Payload: w.Payload}, ok
}

func (w wireComplaint) record() (adapters.ComplaintRecord, bool) {
	ok := w.ComplaintRef != "" && w.CaseID != "" && w.IdempotencyKey != "" && w.LoggedAt != "" && w.ClockStartedAt != "" && w.TenantID != ""
	return adapters.ComplaintRecord{ComplaintRef: w.ComplaintRef, CaseID: w.CaseID, IdempotencyKey: w.IdempotencyKey, LoggedAt: w.LoggedAt,
		ClockStartedAt: w.ClockStartedAt, TenantID: w.TenantID}, ok
}

// AppendEvidence implements adapters.CaseRecords. It sends the SHA-256 of the exact payload bytes, so the target refuses a payload that changed on the
// way, and it accepts an acknowledgement only for this tenant, case, key and hash.
func (c *CaseClient) AppendEvidence(ctx context.Context, in adapters.AppendEvidenceInput) adapters.WriteResult {
	if in.IdempotencyKey == "" || in.TenantID == "" || in.CaseID == "" || in.Payload == "" || in.Timeout <= 0 {
		return defect("INVALID_INPUT")
	}
	// A payload too large to be echoed in the acknowledgement would make a write that landed look unknown, and one that is not valid UTF-8 is rewritten by the JSON encoder, so the
	// hash sent would not be the hash of the bytes stored: both are refused before anything is sent.
	if len(in.Payload) > maxEvidencePayload || !utf8.ValidString(in.Payload) {
		return defect("INVALID_INPUT")
	}
	hash := hashOf([]byte(in.Payload))
	body := map[string]any{"tenant_id": in.TenantID, "payload": in.Payload, "payload_hash": hash}
	status, hdr, raw, res := c.do(ctx, http.MethodPost, "/v1/cases/"+url.PathEscape(in.CaseID)+"/evidence", nil, map[string]string{"Idempotency-Key": in.IdempotencyKey}, body, in.Timeout)
	if res != nil {
		return *res
	}
	return classifyEvidence(status, hdr, raw, in, hash)
}

// LogComplaint implements adapters.CaseRecords.
func (c *CaseClient) LogComplaint(ctx context.Context, in adapters.LogComplaintInput) adapters.WriteResult {
	if in.IdempotencyKey == "" || in.TenantID == "" || in.CaseID == "" || in.Timeout <= 0 {
		return defect("INVALID_INPUT")
	}
	status, hdr, raw, res := c.do(ctx, http.MethodPost, "/v1/cases/"+url.PathEscape(in.CaseID)+"/complaints", nil, map[string]string{"Idempotency-Key": in.IdempotencyKey}, map[string]any{"tenant_id": in.TenantID}, in.Timeout)
	if res != nil {
		return *res
	}
	return classifyComplaint(status, hdr, raw, in)
}

// classifyKeyed maps what an evidence append and a complaint log answer alike except for the 2xx body, which ack reads.
func classifyKeyed(status int, h http.Header, raw []byte, ack func() (adapters.WriteResult, bool)) adapters.WriteResult {
	if r, ok := classifyCommon(status, h, raw); ok {
		return r
	}
	code, _ := errCode(raw)
	switch status {
	case http.StatusCreated, http.StatusOK:
		if r, ok := ack(); ok {
			rc := strconv.Itoa(status)
			if status == http.StatusOK && h.Get("Idempotent-Replay") == "true" {
				rc = "200:REPLAY"
			}
			r.RawCode = rc
			return r
		}
		return unrecognised(status)
	case http.StatusConflict:
		// The same key with a different payload (EC-E06): refused, never acknowledged.
		return adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: rawCode(status, code), KeyCollision: code == "IDEMPOTENCY_CONFLICT"}
	}
	return unrecognised(status)
}

func classifyEvidence(status int, h http.Header, raw []byte, in adapters.AppendEvidenceInput, hash string) adapters.WriteResult {
	return classifyKeyed(status, h, raw, func() (adapters.WriteResult, bool) {
		var w wireEvidence
		if strictJSON(h, raw, &w) != nil {
			return adapters.WriteResult{}, false
		}
		rec, ok := w.record()
		if !ok || rec.TenantID != in.TenantID || rec.CaseID != in.CaseID || rec.IdempotencyKey != in.IdempotencyKey || rec.PayloadHash != hash || hashOf([]byte(rec.Payload)) != hash {
			return adapters.WriteResult{}, false // not the record of this request: the write may have happened, so it is unknown
		}
		return adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: rec.AttachmentID, Evidence: &rec}, true
	})
}

func classifyComplaint(status int, h http.Header, raw []byte, in adapters.LogComplaintInput) adapters.WriteResult {
	return classifyKeyed(status, h, raw, func() (adapters.WriteResult, bool) {
		var w wireComplaint
		if strictJSON(h, raw, &w) != nil {
			return adapters.WriteResult{}, false
		}
		rec, ok := w.record()
		if !ok || rec.TenantID != in.TenantID || rec.CaseID != in.CaseID || rec.IdempotencyKey != in.IdempotencyKey {
			return adapters.WriteResult{}, false
		}
		return adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: rec.ComplaintRef, Complaint: &rec}, true
	})
}

func malformed(status int, raw []byte) *adapters.ReadError {
	return &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(status), RawHash: hashOf(raw)}
}

// GetEvidence implements adapters.CaseRecords. The caller's context must carry a deadline.
func (c *CaseClient) GetEvidence(ctx context.Context, tenantID, attachmentID string) (adapters.EvidenceRecord, error) {
	if tenantID == "" || attachmentID == "" {
		return adapters.EvidenceRecord{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	status, hdr, raw, rerr := c.read(ctx, "/v1/evidence/"+url.PathEscape(attachmentID), url.Values{"tenant_id": {tenantID}})
	if rerr != nil {
		return adapters.EvidenceRecord{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.EvidenceRecord{}, e
	}
	var w wireEvidence
	if strictJSON(hdr, raw, &w) != nil {
		return adapters.EvidenceRecord{}, malformed(status, raw)
	}
	rec, ok := w.record()
	if !ok || rec.TenantID != tenantID || rec.AttachmentID != attachmentID || hashOf([]byte(rec.Payload)) != rec.PayloadHash {
		return adapters.EvidenceRecord{}, malformed(status, raw)
	}
	rec.RawHash = hashOf(raw)
	return rec, nil
}

// GetComplaint implements adapters.CaseRecords. The caller's context must carry a deadline.
func (c *CaseClient) GetComplaint(ctx context.Context, tenantID, complaintRef string) (adapters.ComplaintRecord, error) {
	if tenantID == "" || complaintRef == "" {
		return adapters.ComplaintRecord{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	status, hdr, raw, rerr := c.read(ctx, "/v1/complaints/"+url.PathEscape(complaintRef), url.Values{"tenant_id": {tenantID}})
	if rerr != nil {
		return adapters.ComplaintRecord{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.ComplaintRecord{}, e
	}
	var w wireComplaint
	if strictJSON(hdr, raw, &w) != nil {
		return adapters.ComplaintRecord{}, malformed(status, raw)
	}
	rec, ok := w.record()
	if !ok || rec.TenantID != tenantID || rec.ComplaintRef != complaintRef {
		return adapters.ComplaintRecord{}, malformed(status, raw)
	}
	rec.RawHash = hashOf(raw)
	return rec, nil
}

// recordValues validates a RecordQuery: a tenant and exactly one of an idempotency key or a case.
func recordValues(q adapters.RecordQuery) (url.Values, bool) {
	if q.TenantID == "" || (q.IdempotencyKey != "") == (q.CaseID != "") || q.Limit < 0 {
		return nil, false
	}
	v := url.Values{"tenant_id": {q.TenantID}}
	if q.IdempotencyKey != "" {
		v.Set("idempotency_key", q.IdempotencyKey)
	} else {
		v.Set("case_id", q.CaseID)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	return v, true
}

// inQuery is true when a record answers the query: the case or the key it asked for (a query names exactly one of them), never another's.
func inQuery(q adapters.RecordQuery, caseID, key string) bool {
	if q.CaseID != "" {
		return caseID == q.CaseID
	}
	return key == q.IdempotencyKey
}

// pageOK checks the completeness marker: complete has no cursor, incomplete has one.
func pageOK(complete *bool, next string) bool {
	return complete != nil && (*complete == (next == ""))
}

// FindEvidence implements adapters.CaseRecords. An empty page with Complete=true means "none"; an incomplete page never means absence.
func (c *CaseClient) FindEvidence(ctx context.Context, q adapters.RecordQuery) (adapters.EvidencePage, error) {
	v, ok := recordValues(q)
	if !ok {
		return adapters.EvidencePage{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	status, hdr, raw, rerr := c.read(ctx, "/v1/evidence", v)
	if rerr != nil {
		return adapters.EvidencePage{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.EvidencePage{}, e
	}
	var w wireEvidencePage
	if strictJSON(hdr, raw, &w) != nil || !pageOK(w.Complete, w.NextCursor) {
		return adapters.EvidencePage{}, malformed(status, raw)
	}
	page := adapters.EvidencePage{Complete: *w.Complete, NextCursor: w.NextCursor, RawHash: hashOf(raw)}
	if q.Limit > 0 && len(w.Items) > q.Limit {
		return adapters.EvidencePage{}, malformed(status, raw)
	}
	for _, it := range w.Items {
		rec, ok := it.record()
		if !ok || rec.TenantID != q.TenantID || !inQuery(q, rec.CaseID, rec.IdempotencyKey) || hashOf([]byte(rec.Payload)) != rec.PayloadHash {
			return adapters.EvidencePage{}, malformed(status, raw)
		}
		page.Items = append(page.Items, rec)
	}
	return page, nil
}

// FindComplaints implements adapters.CaseRecords.
func (c *CaseClient) FindComplaints(ctx context.Context, q adapters.RecordQuery) (adapters.ComplaintPage, error) {
	v, ok := recordValues(q)
	if !ok {
		return adapters.ComplaintPage{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	status, hdr, raw, rerr := c.read(ctx, "/v1/complaints", v)
	if rerr != nil {
		return adapters.ComplaintPage{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.ComplaintPage{}, e
	}
	var w wireComplaintPage
	if strictJSON(hdr, raw, &w) != nil || !pageOK(w.Complete, w.NextCursor) {
		return adapters.ComplaintPage{}, malformed(status, raw)
	}
	page := adapters.ComplaintPage{Complete: *w.Complete, NextCursor: w.NextCursor, RawHash: hashOf(raw)}
	if q.Limit > 0 && len(w.Items) > q.Limit {
		return adapters.ComplaintPage{}, malformed(status, raw)
	}
	for _, it := range w.Items {
		rec, ok := it.record()
		if !ok || rec.TenantID != q.TenantID || !inQuery(q, rec.CaseID, rec.IdempotencyKey) {
			return adapters.ComplaintPage{}, malformed(status, raw)
		}
		page.Items = append(page.Items, rec)
	}
	return page, nil
}
