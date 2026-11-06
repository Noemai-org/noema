package proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/jsonx"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/plan"
)

func itoa(n int) string { return strconv.Itoa(n) }

// faultTree applies the faults that change the JSON document: drift, skew, injected text and event-feed faults.
// detail receives a small, deterministic account of what happened, for the trace.
func (s *Server) faultTree(c *call, tree any, detail map[string]any) any {
	e := c.eff
	if c.rt.EventFeed != nil {
		tree = s.eventFeed(c, tree, detail)
	}
	if c.rt.Page != nil {
		tree = s.paginate(c, tree, detail)
	}
	if e == nil {
		return tree
	}
	switch e.Name {
	case plan.SchemaDrift:
		var field string
		tree, field = jsonx.Drift(tree, e.Change, e.Field)
		detail["field"], detail["applied"] = field, field != ""
	case plan.ClockSkew:
		var n int
		tree, n = jsonx.Skew(tree, e.Skew)
		detail["shifted"] = n
	case plan.InjectionPayload:
		var field string
		tree, field = jsonx.Inject(tree, e.Field, int(e.Pick%1024))
		detail["field"], detail["applied"] = field, field != ""
	}
	return tree
}

// eventFeed applies the event-delivery faults to the events of this response, then adds any withheld events that
// are now due, at the end: a late event arrives after the events that followed it.
func (s *Server) eventFeed(c *call, tree any, detail map[string]any) any {
	key := c.rt.Target + "/" + c.rt.Operation
	events, ok := jsonx.EventArray(tree, c.rt.EventFeed.Events)
	if !ok {
		if c.eff != nil {
			detail["applied"] = false
		}
		return tree
	}
	if e := c.eff; e != nil {
		applied, known := len(events) > 0, true
		switch e.Name {
		case plan.DropWebhook:
			if applied {
				events = events[1:]
				detail["dropped_index"], detail["remaining"] = 0, len(events) // the diagnostic that shows the drop (AT-05)
			}
		case plan.DuplicateWebhook:
			if applied {
				events = append([]any{events[0]}, events...)
			}
		case plan.ReorderEvents:
			applied = len(events) >= 2
			if applied {
				i := int(e.Pick % uint64(len(events)-1))
				events = append([]any(nil), events...)
				events[i], events[i+1] = events[i+1], events[i]
				detail["swapped"] = i
			}
		case plan.LateEvent:
			if applied {
				s.held.hold(key, c.now.Add(e.Delay), events)
				detail["held"] = len(events)
				events = []any{}
			}
		default:
			known = false // an effect for the document as a whole, applied by faultTree
		}
		if known {
			detail["applied"] = applied
		}
	}
	if due := s.held.due(key, c.now); len(due) > 0 {
		events = append(append([]any(nil), events...), due...)
		detail["released"] = len(due)
	}
	if events == nil {
		events = []any{}
	}
	return jsonx.SetEventArray(tree, c.rt.EventFeed.Events, events)
}

// bodyEffect reports whether an effect rewrites the delivered body or its framing (rather than the JSON tree), so a
// body that is not JSON does not make it "not applied".
func bodyEffect(name string) bool {
	switch name {
	case plan.Malformed, plan.InvalidJSON, plan.WrongContentType, plan.EmptyBodySuccess:
		return true
	}
	return false
}

// corruptBody applies the corrupted-payload effects (REQ-ENV-005, DEC-2026-10-31 D15). Headers are cloned before a
// change: the response shares its header map with the stale-read cache.
func corruptBody(e *plan.Effect, resp *upstreamResponse, body []byte, detail map[string]any) (*upstreamResponse, []byte) {
	switch e.Name {
	case plan.Malformed:
		body = body[:len(body)/2]
		detail["bytes"] = len(body)
	case plan.InvalidJSON:
		body = breakJSON(body)
		detail["bytes"], detail["applied"] = len(body), true
	case plan.WrongContentType:
		h := resp.header.Clone()
		h.Set("Content-Type", e.ContentType)
		resp = &upstreamResponse{status: resp.status, header: h, body: resp.body}
		detail["content_type"], detail["applied"] = e.ContentType, true
	case plan.EmptyBodySuccess:
		status := resp.status
		if status < 200 || status > 299 {
			status = http.StatusOK
		}
		resp = &upstreamResponse{status: status, header: resp.header.Clone(), body: nil}
		body = nil
		detail["status"], detail["applied"] = status, true
	}
	return resp, body
}

// breakJSON makes a document that is whole but is not JSON: the closing bracket is preceded by a comma, the most
// ordinary hand-edit slip. It is longer than the original, so it cannot be mistaken for a truncation.
func breakJSON(body []byte) []byte {
	t := bytes.TrimRight(body, " \t\r\n")
	if n := len(t); n > 0 && (t[n-1] == '}' || t[n-1] == ']') {
		return append(append(append([]byte(nil), t[:n-1]...), ','), t[n-1])
	}
	return append(append([]byte(nil), t...), '}')
}

// pageReq is a continuation the proxy issued itself: the offset into the list and the size of every page.
type pageReq struct{ offset, size int }

const (
	cursorPrefix    = "pgx1"
	maxCursorOffset = 1_000_000 // a structural bound on a forged cursor, not a product threshold
	maxCursorSize   = 10_000
)

func formatCursor(offset, size int) string {
	return fmt.Sprintf("%s.%d.%d", cursorPrefix, offset, size)
}

// parseCursor reads a cursor the proxy issued. Anything else is refused: a cursor is never forwarded.
func parseCursor(raw string) (pageReq, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != cursorPrefix {
		return pageReq{}, false
	}
	offset, err1 := strconv.Atoi(parts[1])
	size, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || offset < 0 || offset > maxCursorOffset || size < 1 || size > maxCursorSize {
		return pageReq{}, false
	}
	return pageReq{offset, size}, true
}

// paginate splits the list of a paged route. A call under a PAGINATE fault starts at the top; a call carrying one of
// the proxy's cursors continues wherever the fault is, so a caller that follows the cursors sees every item once,
// in order. The proxy assumes the upstream answered the whole list in one response.
func (s *Server) paginate(c *call, tree any, detail map[string]any) any {
	pg := c.rt.Page
	var at pageReq
	switch {
	case c.page != nil:
		at = *c.page
	case c.eff != nil && c.eff.Name == plan.Paginate:
		at = pageReq{0, c.eff.PageSize}
	default:
		return tree
	}
	doc, isObject := tree.(map[string]any)
	items, ok := jsonx.EventArray(tree, pg.Items)
	if !isObject || !ok {
		if c.eff != nil {
			detail["applied"] = false
		}
		return tree
	}
	offset := min(at.offset, len(items))
	end := min(offset+at.size, len(items))
	complete := end >= len(items)
	doc[pg.Items] = append([]any{}, items[offset:end]...)
	doc[pg.Complete] = complete
	if complete {
		delete(doc, pg.NextCursor)
	} else {
		doc[pg.NextCursor] = formatCursor(end, at.size)
	}
	if c.eff != nil {
		detail["applied"], detail["offset"], detail["items"], detail["total"] = true, offset, end-offset, len(items)
	}
	return doc
}
