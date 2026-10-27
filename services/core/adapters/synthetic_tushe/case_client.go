package synthetic_tushe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// CaseClient is the core's client of mock-case, the synthetic case system (PERD 34.4; REQ-ENV-001,
// REQ-EXE-007, REQ-ENV-008). It implements adapters.Case. mock-case is a Noema-run component, not hosted
// Tushe; PERD 64.2 keeps its client in this package with the other clients of the synthetic enterprise.
//
// It talks to the case listener only, with the runtime credential. It has no code for the truth or control
// listeners and never holds the bench credential (REQ-ENV-002, INV-22): a test reads this package's source
// and fails if either path appears.
type CaseClient struct {
	base  string
	token string
	http  *http.Client
}

var _ adapters.Case = (*CaseClient)(nil)

const maxBody = 1 << 20

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,39}$`)

// NewCaseClient builds the client. tokenFile is the path of the file-mounted runtime credential (local
// secrets are file-mounted, OD-P-22); its content is never logged, returned in an error or sent anywhere
// but in the Authorization header of a request to baseURL. A nil httpClient gets a client that does not
// follow redirects (a redirect would carry the credential to another host).
func NewCaseClient(baseURL, tokenFile string, httpClient *http.Client) (*CaseClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("case client: base URL must be http(s)://host[:port] with no query")
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return nil, errors.New("case client: base URL must not carry a path")
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("case client: cannot read the credential file %q", tokenFile)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return nil, fmt.Errorf("case client: the credential file %q is empty", tokenFile)
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	hc := *httpClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &CaseClient{base: strings.TrimRight(baseURL, "/"), token: token, http: &hc}, nil
}

// wire shapes (synthetic/mock_case/internal/api/openapi.json). Unknown fields are refused: schema drift
// is UNKNOWN, never a guess (EVD-05).
type wireCase struct {
	CaseID          string `json:"case_id"`
	Reference       string `json:"reference"`
	Status          string `json:"status"`
	OwnerQueue      string `json:"owner_queue"`
	Priority        string `json:"priority"`
	IdempotencyKey  string `json:"idempotency_key"`
	VersionToken    string `json:"version_token"`
	CreatedAt       string `json:"created_at"`
	TenantID        string `json:"tenant_id"`
	PrincipalID     string `json:"principal_id"`
	TransactionRef  string `json:"transaction_ref"`
	ReasonCode      string `json:"reason_code"`
	CreatedByIntent string `json:"created_by_intent"`
	Epoch           int64  `json:"epoch"`
}

type wirePage struct {
	Items      []wireCase `json:"items"`
	Complete   *bool      `json:"complete"`
	NextCursor string     `json:"next_cursor"`
}

type wireError struct {
	Error struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
		Details   []struct {
			Field string `json:"field"`
			Issue string `json:"issue"`
		} `json:"details"`
	} `json:"error"`
}

func strict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

func (w wireCase) record() (adapters.CaseRecord, bool) {
	ok := w.CaseID != "" && w.Reference != "" && w.VersionToken != "" && (w.Status == "OPEN" || w.Status == "ROUTED")
	return adapters.CaseRecord{CaseID: w.CaseID, Reference: w.Reference, Status: w.Status, OwnerQueue: w.OwnerQueue,
		Priority: w.Priority, IdempotencyKey: w.IdempotencyKey, VersionToken: w.VersionToken, CreatedAt: w.CreatedAt,
		TenantID: w.TenantID, PrincipalID: w.PrincipalID, TransactionRef: w.TransactionRef, ReasonCode: w.ReasonCode,
		CreatedByIntent: w.CreatedByIntent, Epoch: w.Epoch}, ok
}

// CreateCase implements adapters.Case. It sends exactly one request and never retries (REQ-EXE-006).
func (c *CaseClient) CreateCase(ctx context.Context, in adapters.CreateCaseInput) adapters.WriteResult {
	if in.IdempotencyKey == "" || in.TenantID == "" || in.PrincipalID == "" || in.TransactionRef == "" ||
		in.ReasonCode == "" || in.CreatedByIntent == "" || in.Epoch < 0 || in.Timeout <= 0 {
		return defect("INVALID_INPUT")
	}
	body := map[string]any{"tenant_id": in.TenantID, "principal_id": in.PrincipalID, "transaction_ref": in.TransactionRef,
		"reason_code": in.ReasonCode, "created_by_intent": in.CreatedByIntent, "epoch": in.Epoch}
	if in.OwnerQueue != "" {
		body["owner_queue"] = in.OwnerQueue
	}
	if in.Priority != "" {
		body["priority"] = in.Priority
	}
	status, hdr, raw, res := c.do(ctx, http.MethodPost, "/v1/cases", nil, map[string]string{"Idempotency-Key": in.IdempotencyKey, "If-None-Match": "*"}, body, in.Timeout)
	if res != nil {
		return *res
	}
	return classifyCreate(status, hdr, raw)
}

// RouteCase implements adapters.Case.
func (c *CaseClient) RouteCase(ctx context.Context, in adapters.RouteCaseInput) adapters.WriteResult {
	if in.TenantID == "" || in.CaseID == "" || in.OwnerQueue == "" || in.ExpectedVersionToken == "" || in.Timeout <= 0 {
		return defect("INVALID_INPUT")
	}
	body := map[string]any{"tenant_id": in.TenantID, "owner_queue": in.OwnerQueue, "expected_version_token": in.ExpectedVersionToken}
	status, hdr, raw, res := c.do(ctx, http.MethodPost, "/v1/cases/"+url.PathEscape(in.CaseID)+"/route", nil, nil, body, in.Timeout)
	if res != nil {
		return *res
	}
	return classifyRoute(status, hdr, raw)
}

// GetCase implements adapters.Case. The caller's context must carry a deadline.
func (c *CaseClient) GetCase(ctx context.Context, tenantID, caseID string) (adapters.CaseRecord, error) {
	if tenantID == "" || caseID == "" {
		return adapters.CaseRecord{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	q := url.Values{"tenant_id": {tenantID}}
	status, hdr, raw, rerr := c.read(ctx, "/v1/cases/"+url.PathEscape(caseID), q)
	if rerr != nil {
		return adapters.CaseRecord{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.CaseRecord{}, e
	}
	var w wireCase
	if err := strict(raw, &w); err != nil {
		return adapters.CaseRecord{}, &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(status)}
	}
	rec, ok := w.record()
	if !ok {
		return adapters.CaseRecord{}, &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(status)}
	}
	return rec, nil
}

// FindCases implements adapters.Case. An empty page with Complete=true means "no such case"; an incomplete
// page never means absence. The caller's context must carry a deadline.
func (c *CaseClient) FindCases(ctx context.Context, q adapters.CaseQuery) (adapters.CasePage, error) {
	byKey := q.IdempotencyKey != ""
	byPrincipal := q.PrincipalID != ""
	if q.TenantID == "" || byKey == byPrincipal || (byKey && q.TransactionRef != "") || q.Limit < 0 {
		return adapters.CasePage{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	v := url.Values{"tenant_id": {q.TenantID}}
	if byKey {
		v.Set("idempotency_key", q.IdempotencyKey)
	} else {
		v.Set("principal_id", q.PrincipalID)
		if q.TransactionRef != "" {
			v.Set("transaction_ref", q.TransactionRef)
		}
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	status, hdr, raw, rerr := c.read(ctx, "/v1/cases", v)
	if rerr != nil {
		return adapters.CasePage{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.CasePage{}, e
	}
	var w wirePage
	if err := strict(raw, &w); err != nil || w.Complete == nil || (!*w.Complete && w.NextCursor == "") || (*w.Complete && w.NextCursor != "") {
		return adapters.CasePage{}, &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(status)}
	}
	page := adapters.CasePage{Complete: *w.Complete, NextCursor: w.NextCursor}
	for _, it := range w.Items {
		rec, ok := it.record()
		if !ok {
			return adapters.CasePage{}, &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(status)}
		}
		page.Items = append(page.Items, rec)
	}
	return page, nil
}

// do sends one request. A non-nil last return value is the final WriteResult for a transport failure.
func (c *CaseClient) do(ctx context.Context, method, path string, query url.Values, hdr map[string]string, body any, timeout time.Duration) (int, http.Header, []byte, *adapters.WriteResult) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	status, h, raw, err := c.send(ctx, method, path, query, hdr, body)
	if err != nil {
		r := transportFailure(ctx, err)
		return 0, nil, nil, &r
	}
	return status, h, raw, nil
}

func (c *CaseClient) read(ctx context.Context, path string, query url.Values) (int, http.Header, []byte, *adapters.ReadError) {
	if _, ok := ctx.Deadline(); !ok {
		return 0, nil, nil, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:NO_DEADLINE"}
	}
	status, h, raw, err := c.send(ctx, http.MethodGet, path, query, nil, nil)
	if err != nil {
		return 0, nil, nil, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: transportFailure(ctx, err).RawCode}
	}
	return status, h, raw, nil
}

func (c *CaseClient) send(ctx context.Context, method, path string, query url.Values, hdr map[string]string, body any) (int, http.Header, []byte, error) {
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return 0, nil, nil, err
	}
	return res.StatusCode, res.Header, raw, nil
}

func defect(why string) adapters.WriteResult {
	return adapters.WriteResult{Class: adapters.ClassDefect, RawCode: "ADAPTER:" + why}
}

// transportFailure classifies an error with no usable response. Every one of them means "the write may or
// may not have happened": Execution maps all of them to OUTCOME_UNKNOWN, never to a failure (INV-07).
func transportFailure(ctx context.Context, err error) adapters.WriteResult {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return adapters.WriteResult{Class: adapters.ClassTimeout, RawCode: "TIMEOUT"}
	case errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.ErrUnexpectedEOF):
		return adapters.WriteResult{Class: adapters.ClassConnectionReset, RawCode: "CONNECTION_RESET"}
	default:
		return adapters.WriteResult{Class: adapters.ClassNoResponse, RawCode: "NO_RESPONSE"}
	}
}

func errCode(raw []byte) (string, *wireError) {
	var e wireError
	if json.Unmarshal(raw, &e) != nil || !codePattern.MatchString(e.Error.Code) {
		return "", nil
	}
	return e.Error.Code, &e
}

func rawCode(status int, code string) string {
	if code == "" {
		return strconv.Itoa(status)
	}
	return strconv.Itoa(status) + ":" + code
}

func retryAfter(h http.Header) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && n >= 0 && n < 3600 {
		return time.Duration(n) * time.Second
	}
	return 0
}

// classifyCommon maps the statuses that mean the same for every write.
func classifyCommon(status int, h http.Header, raw []byte) (adapters.WriteResult, bool) {
	code, _ := errCode(raw)
	switch {
	case status == http.StatusTooManyRequests:
		return adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: rawCode(status, code), RetryAfter: retryAfter(h)}, true
	case status >= 500 && status <= 599:
		return adapters.WriteResult{Class: adapters.ClassServer5xx, RawCode: rawCode(status, code)}, true
	case status >= 400 && status <= 499 && status != http.StatusConflict:
		return adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: rawCode(status, code)}, true
	}
	return adapters.WriteResult{}, false
}

// unrecognised is a 2xx or 3xx the adapter cannot read: the write may have happened, so it is unknown.
func unrecognised(status int) adapters.WriteResult {
	return adapters.WriteResult{Class: adapters.ClassNoResponse, RawCode: strconv.Itoa(status) + ":UNRECOGNISED"}
}

func classifyCreate(status int, h http.Header, raw []byte) adapters.WriteResult {
	if status != http.StatusPreconditionFailed {
		if r, ok := classifyCommon(status, h, raw); ok {
			return r
		}
	}
	code, we := errCode(raw)
	switch status {
	case http.StatusPreconditionFailed:
		// The create was conditional on there being no case for the subject and epoch, and the target holds one that
		// another key made. The state the write expected ("absent") no longer holds: the same class as a stale version
		// (E10). The case that exists is named, never adopted as this write's result.
		if code != "PRECONDITION_FAILED" {
			return adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: rawCode(status, code)}
		}
		r := adapters.WriteResult{Class: adapters.ClassVersionConflict, RawCode: rawCode(status, code)}
		if we != nil {
			for _, d := range we.Error.Details {
				switch d.Field {
				case "existing_version_token":
					r.CurrentVersionToken = d.Issue
				case "existing_case_id":
					r.ExistingCaseID = d.Issue
				}
			}
		}
		return r
	case http.StatusCreated, http.StatusOK:
		var w wireCase
		if err := strict(raw, &w); err == nil {
			if rec, ok := w.record(); ok {
				rc := "201"
				if status == http.StatusOK {
					rc = "200"
					if h.Get("Idempotent-Replay") == "true" {
						rc = "200:REPLAY"
					}
				}
				return adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: rec.Reference, RawCode: rc, Case: &rec}
			}
		}
		// HTTP 200 with a semantic PENDING and nothing created: preserved as acknowledged with no reference;
		// Outcome decides what it means (REQ-EXE-007, EC-E03).
		var p struct {
			Status string `json:"status"`
			Detail string `json:"detail"`
		}
		if status == http.StatusOK && json.Unmarshal(raw, &p) == nil && p.Status == "PENDING" {
			return adapters.WriteResult{Class: adapters.ClassAck2xx, RawCode: "200:PENDING"}
		}
		return unrecognised(status)
	case http.StatusConflict:
		if code == "IDEMPOTENCY_CONFLICT" {
			// The same key with a different payload (EC-E06): refused, never acknowledged.
			return adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: rawCode(status, code), KeyCollision: true}
		}
		return adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: rawCode(status, code)}
	}
	return unrecognised(status)
}

func classifyRoute(status int, h http.Header, raw []byte) adapters.WriteResult {
	if r, ok := classifyCommon(status, h, raw); ok {
		return r
	}
	code, we := errCode(raw)
	switch status {
	case http.StatusOK:
		var w wireCase
		if err := strict(raw, &w); err == nil {
			if rec, ok := w.record(); ok {
				return adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: rec.Reference, RawCode: "200", Case: &rec}
			}
		}
		return unrecognised(status)
	case http.StatusConflict:
		if code == "VERSION_MISMATCH" {
			r := adapters.WriteResult{Class: adapters.ClassVersionConflict, RawCode: rawCode(status, code)}
			if we != nil {
				for _, d := range we.Error.Details {
					if d.Field == "current_version_token" {
						r.CurrentVersionToken = d.Issue
					}
				}
			}
			return r
		}
		return adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: rawCode(status, code)}
	}
	return unrecognised(status)
}

// readStatusError maps a non-200 read to its failure. A nil result means 200.
func readStatusError(status int, h http.Header, raw []byte) *adapters.ReadError {
	code, _ := errCode(raw)
	rc := rawCode(status, code)
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusNotFound:
		return &adapters.ReadError{Failure: adapters.ReadNotFound, RawCode: rc}
	case status == http.StatusBadRequest:
		return &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: rc}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &adapters.ReadError{Failure: adapters.ReadUnauthorized, RawCode: rc}
	case status == http.StatusTooManyRequests:
		return &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: rc, RetryAfter: retryAfter(h)}
	case status >= 500 && status <= 599:
		return &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: rc}
	}
	return &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: rc}
}
