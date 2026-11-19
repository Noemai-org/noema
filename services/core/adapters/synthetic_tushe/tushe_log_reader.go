package synthetic_tushe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// TusheReader reads the payout transition log and the event feed of Tushe (delta v1.3 section 3; ruling A310). GET only. The credential comes from a file and is presented the way the
// caller says (no governing document says how an API key is presented, so there is no default). The reader never follows a redirect, never retries, and returns every failure as a
// *adapters.ReadError: a timeout or a 5xx is UNAVAILABLE, a body it cannot read is MALFORMED (UNKNOWN to the caller), and none of them is ever "no rows". SYNTHETIC (AGENT-25).
type TusheReader struct {
	base    string
	token   string
	present Presenter
	shapes  ListShapes
	http    *http.Client
}

// Presenter puts the credential on a request. The two common ways are provided; which one Tushe wants is the Founders' to tell (it is not in the specification or the deltas).
type Presenter func(req *http.Request, secret string)

// PresentBearer sends `Authorization: Bearer <secret>`.
func PresentBearer(req *http.Request, secret string) {
	req.Header.Set("Authorization", "Bearer "+secret)
}

// PresentAPIKeyHeader sends `X-API-Key: <secret>`.
func PresentAPIKeyHeader(req *http.Request, secret string) { req.Header.Set("X-API-Key", secret) }

// ListShape says where the array of a list answer is: "" means the body IS the array; otherwise the body is an object and the array is its member of that name. The delta does not state the
// envelope of either list, so the shape is a required parameter with no default: adopting the real one changes one value, not the reader.
type ListShape struct{ Member string }

// ListShapes holds the shape of each list the reader reads.
type ListShapes struct {
	Transitions ListShape
	Events      ListShape
}

var _ adapters.TransitionLog = (*TusheReader)(nil)
var _ adapters.EventFeed = (*TusheReader)(nil)

// NewTusheReader builds the reader. baseURL is http(s)://host[:port] with no path or query; tokenFile is the credential's file (its content is never logged, returned or sent anywhere but in
// the header the Presenter writes); a nil httpClient gets one that does not follow redirects (a redirect would carry the credential to another host).
func NewTusheReader(baseURL, tokenFile string, present Presenter, shapes ListShapes, httpClient *http.Client) (*TusheReader, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("tushe reader: base URL must be http(s)://host[:port] with no path or query")
	}
	if present == nil {
		return nil, errors.New("tushe reader: say how the credential is presented")
	}
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("tushe reader: cannot read the credential file %q", tokenFile)
	}
	secret := strings.TrimSpace(string(raw))
	if secret == "" || strings.ContainsAny(secret, "\r\n\t ") {
		return nil, fmt.Errorf("tushe reader: the credential file %q does not hold one printable value", tokenFile)
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	hc := *httpClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &TusheReader{base: strings.TrimRight(baseURL, "/"), token: secret, present: present, shapes: shapes, http: &hc}, nil
}

func (r *TusheReader) get(ctx context.Context, path string, query url.Values) (int, http.Header, []byte, *adapters.ReadError) {
	if _, ok := ctx.Deadline(); !ok {
		return 0, nil, nil, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:NO_DEADLINE"}
	}
	target := r.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, nil, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:BAD_REQUEST"}
	}
	r.present(req, r.token)
	req.Header.Set("Accept", "application/json")
	res, err := r.http.Do(req)
	if err != nil {
		return 0, nil, nil, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: transportFailure(ctx, err).RawCode}
	}
	defer func() { _ = res.Body.Close() }() // read in full below; a failed close afterwards is not actionable
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		// A body that cannot be read to its end is a transport failure (a stall until the deadline, a reset, a short body under its Content-Length), never schema corruption
		// (Codex P2 on noema#711): it is UNAVAILABLE with the same codes as a failure before the headers.
		return 0, nil, nil, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: transportFailure(ctx, err).RawCode}
	}
	if len(raw) > maxBody {
		return 0, nil, nil, &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(res.StatusCode) + ":TOO_LARGE_OR_CUT"}
	}
	return res.StatusCode, res.Header, raw, nil
}

func unreadable(status int, raw []byte) *adapters.ReadError {
	return &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(status), RawHash: hashOf(raw)}
}

// elements returns the array of a list answer, by the shape the caller gave.
func listElements(raw []byte, shape ListShape) ([]json.RawMessage, bool) {
	var list []json.RawMessage
	if shape.Member == "" {
		return list, json.Unmarshal(raw, &list) == nil && list != nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil, false
	}
	member, ok := obj[shape.Member]
	if !ok || json.Unmarshal(member, &list) != nil || list == nil {
		return nil, false
	}
	return list, true
}

// ---- the transition log

// wireTransition lists exactly the members delta v1.3 names. A member it does not name is refused: schema drift is UNKNOWN, never a guess.
type wireTransition struct {
	Seq        *int64  `json:"seq"`
	Lifecycle  string  `json:"lifecycle"`
	FromState  *string `json:"fromState"`
	ToState    string  `json:"toState"`
	Source     string  `json:"source"`
	SourceRef  string  `json:"sourceRef"`
	Inferred   bool    `json:"inferred"`
	Injected   bool    `json:"injected"`
	Reason     string  `json:"reason"`
	OccurredAt string  `json:"occurredAt"`
	ObservedAt string  `json:"observedAt"`
	Actor      string  `json:"actor"`
	Request    struct {
		ID             string `json:"id"`
		IdempotencyKey string `json:"idempotencyKey"`
	} `json:"request"`
}

func (w wireTransition) row() (adapters.PayoutTransition, bool) {
	occurred, err1 := time.Parse(time.RFC3339Nano, w.OccurredAt)
	if w.Seq == nil || w.ToState == "" || err1 != nil {
		return adapters.PayoutTransition{}, false
	}
	var observed time.Time
	if w.ObservedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, w.ObservedAt)
		if err != nil {
			return adapters.PayoutTransition{}, false
		}
		observed = t
	}
	from := ""
	if w.FromState != nil {
		from = *w.FromState
	}
	return adapters.PayoutTransition{Seq: *w.Seq, Lifecycle: w.Lifecycle, FromState: from, ToState: w.ToState, Source: w.Source, SourceRef: w.SourceRef, Inferred: w.Inferred,
		Injected: w.Injected, Reason: w.Reason, OccurredAt: occurred, ObservedAt: observed, Actor: w.Actor, RequestID: w.Request.ID, IdempotencyKey: w.Request.IdempotencyKey}, true
}

// PayoutTransitions reads GET /v1/accounts/{accountId}/payouts/{payoutId}/transitions, which the delta says returns the log oldest first. The order is checked, not trusted: a sequence that
// does not strictly rise is MALFORMED. An empty log is a real answer only because the read succeeded; the caller still decides what it means.
func (r *TusheReader) PayoutTransitions(ctx context.Context, accountID, payoutID string) (adapters.TransitionPage, error) {
	if accountID == "" || payoutID == "" {
		return adapters.TransitionPage{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	status, hdr, raw, rerr := r.get(ctx, "/v1/accounts/"+url.PathEscape(accountID)+"/payouts/"+url.PathEscape(payoutID)+"/transitions", nil)
	if rerr != nil {
		return adapters.TransitionPage{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.TransitionPage{}, e
	}
	if !isJSONResponse(hdr) {
		return adapters.TransitionPage{}, unreadable(status, raw)
	}
	list, ok := listElements(raw, r.shapes.Transitions)
	if !ok {
		return adapters.TransitionPage{}, unreadable(status, raw)
	}
	page := adapters.TransitionPage{RawHash: hashOf(raw)}
	for _, el := range list {
		var w wireTransition
		if strict(el, &w) != nil {
			return adapters.TransitionPage{}, unreadable(status, raw)
		}
		row, ok := w.row()
		if !ok || (len(page.Items) > 0 && row.Seq <= page.Items[len(page.Items)-1].Seq) {
			return adapters.TransitionPage{}, unreadable(status, raw)
		}
		page.Items = append(page.Items, row)
	}
	return page, nil
}

// ---- the event feed

func feedEvent(el json.RawMessage) (adapters.FeedEvent, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(el, &m) != nil {
		return adapters.FeedEvent{}, false
	}
	var ev adapters.FeedEvent
	var seq int64                                                                                                                                                                               // an int64 member: a number held in a string ("16") is refused, as json.Number would have accepted it
	if json.Unmarshal(m["id"], &ev.ID) != nil || json.Unmarshal(m["traceId"], &ev.TraceID) != nil || json.Unmarshal(m["sequence"], &seq) != nil || ev.ID == "" || ev.TraceID == "" || seq < 0 { // a null or empty trace id decodes to "" and cannot join the trace (Codex P2 on noema#711)
		return adapters.FeedEvent{}, false
	}
	ev.Sequence = seq
	ev.Raw = append(json.RawMessage(nil), el...)
	return ev, true
}

// EventsAfter reads GET /v1/events?after={sequence}&limit={n}: the delta's cursor is the sequence, never a timestamp.
func (r *TusheReader) EventsAfter(ctx context.Context, after int64, limit int) (adapters.FeedPage, error) {
	if after < 0 || limit < 1 {
		return adapters.FeedPage{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	status, hdr, raw, rerr := r.get(ctx, "/v1/events", url.Values{"after": {strconv.FormatInt(after, 10)}, "limit": {strconv.Itoa(limit)}})
	if rerr != nil {
		return adapters.FeedPage{}, rerr
	}
	if e := readStatusError(status, hdr, raw); e != nil {
		return adapters.FeedPage{}, e
	}
	if !isJSONResponse(hdr) {
		return adapters.FeedPage{}, unreadable(status, raw)
	}
	list, ok := listElements(raw, r.shapes.Events)
	if !ok {
		return adapters.FeedPage{}, unreadable(status, raw)
	}
	page := adapters.FeedPage{RawHash: hashOf(raw)}
	last := after
	for _, el := range list {
		ev, ok := feedEvent(el)
		if !ok || ev.Sequence <= last {
			return adapters.FeedPage{}, unreadable(status, raw) // a sequence that does not rise past the cursor: a repeat or a rewind is never read as data
		}
		last = ev.Sequence
		page.Items = append(page.Items, ev)
	}
	return page, nil
}

// ReadFeed follows the feed forward until a page is empty.
func (r *TusheReader) ReadFeed(ctx context.Context, after int64, limit, maxPages int) ([]adapters.FeedEvent, error) {
	if maxPages < 1 {
		return nil, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	var out []adapters.FeedEvent
	cursor := after
	for page := 0; page < maxPages; page++ {
		p, err := r.EventsAfter(ctx, cursor, limit)
		if err != nil {
			return nil, err
		}
		if len(p.Items) == 0 {
			return out, nil
		}
		out = append(out, p.Items...)
		cursor = p.Items[len(p.Items)-1].Sequence
	}
	return nil, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:FEED_PAGE_LIMIT"} // not caught up within the limit: never a truncated result
}
