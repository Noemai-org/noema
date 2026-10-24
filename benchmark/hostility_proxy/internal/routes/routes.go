// Package routes is the proxy's allowlist. Only a request that matches a route is forwarded, and only to the
// upstream the route names, so the proxy can never be used as an open relay. A route also names the logical
// service and the operation a fault plan addresses it by (PERD §37.1 target and operation), and which request
// attributes a fault's `match` may use. The route table is data: it follows the Tushe interface, which is frozen
// at Phase A exit (PERD REQ-ENV-001).
package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// EventFeed marks a route that returns a list of events and says where the list is.
type EventFeed struct {
	// Events is the top-level key holding the array; "" means the whole body is the array.
	Events string `json:"events"`
}

// Route is one entry of the table.
type Route struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"` // a net/http pattern path: /v1/status/{transaction_ref}
	Upstream  string            `json:"upstream"`
	Target    string            `json:"target"`
	Operation string            `json:"operation"`
	Attrs     map[string]string `json:"attrs,omitempty"` // attribute -> path:<var> | query:<name> | header:<Name>
	EventFeed *EventFeed        `json:"event_feed,omitempty"`
}

// Table is the parsed route table.
type Table struct {
	Upstreams map[string]struct{}
	routes    []Route
	mux       *http.ServeMux
	byKey     map[string]int
}

type file struct {
	SchemaVersion string  `json:"schema_version"`
	Synthetic     bool    `json:"synthetic"`
	Routes        []Route `json:"routes"`
}

var (
	nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	upRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	attrRE = regexp.MustCompile(`^(path:[a-z][a-z0-9_]*|query:[A-Za-z][A-Za-z0-9_.-]*|header:[A-Za-z][A-Za-z0-9-]*)$`)
	// The targets of PERD §34.1 that a request can address; mock-case is served by mock-case itself, not proxied.
	targets = map[string]bool{"mock-ledger": true, "mock-switch": true, "mock-destination": true, "mock-customer": true, "mock-policy": true}
)

// Parse reads a route table. It is strict: unknown fields, an unknown target, a repeated route or a repeated
// (target, operation) are errors.
func Parse(data []byte) (*Table, error) {
	var f file
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("routes: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("routes: trailing data")
	}
	if f.SchemaVersion != "1.0" {
		return nil, errors.New(`routes: schema_version must be "1.0"`)
	}
	if !f.Synthetic {
		return nil, errors.New("routes: synthetic must be true (AGENT-25): this table describes a test environment")
	}
	t := &Table{Upstreams: map[string]struct{}{}, mux: http.NewServeMux(), byKey: map[string]int{}}
	seenPattern := map[string]bool{}
	for i := range f.Routes {
		r := f.Routes[i]
		if err := t.add(r, seenPattern); err != nil {
			return nil, fmt.Errorf("routes[%d]: %w", i, err)
		}
	}
	if len(t.routes) == 0 {
		return nil, errors.New("routes: the table is empty")
	}
	return t, nil
}

func (t *Table) add(r Route, seen map[string]bool) error {
	switch r.Method {
	case "GET", "POST", "PUT", "DELETE":
	default:
		return fmt.Errorf("method %q is not served", r.Method)
	}
	if !strings.HasPrefix(r.Path, "/") || strings.ContainsAny(r.Path, " ?#") {
		return fmt.Errorf("path %q is not a path pattern", r.Path)
	}
	if !upRE.MatchString(r.Upstream) {
		return fmt.Errorf("upstream %q is not a name", r.Upstream)
	}
	if !targets[r.Target] {
		return fmt.Errorf("target %q is not a proxied logical service", r.Target)
	}
	if !nameRE.MatchString(r.Operation) {
		return fmt.Errorf("operation %q is not snake_case", r.Operation)
	}
	pattern := r.Method + " " + r.Path
	if seen[pattern] {
		return fmt.Errorf("%s is listed twice", pattern)
	}
	seen[pattern] = true
	key := r.Target + "/" + r.Operation
	if _, dup := t.byKey[key]; dup {
		return fmt.Errorf("%s is the target and operation of two routes", key)
	}
	for name, src := range r.Attrs {
		if !nameRE.MatchString(name) || name == "nth_call" {
			return fmt.Errorf("attribute name %q is not allowed", name)
		}
		if !attrRE.MatchString(src) {
			return fmt.Errorf("attribute %s: source %q is not path:<var>, query:<name> or header:<Name>", name, src)
		}
		if strings.HasPrefix(src, "path:") && !strings.Contains(r.Path, "{"+strings.TrimPrefix(src, "path:")+"}") {
			return fmt.Errorf("attribute %s: the path has no {%s}", name, strings.TrimPrefix(src, "path:"))
		}
	}
	if r.EventFeed != nil && r.Method != "GET" {
		return errors.New("an event feed is read with GET")
	}
	idx := len(t.routes)
	var conflict error
	func() {
		defer func() { // ServeMux panics on a pattern that conflicts with an earlier one
			if p := recover(); p != nil {
				conflict = fmt.Errorf("pattern %q conflicts with an earlier route: %v", pattern, p)
			}
		}()
		t.mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) { w.(*matched).idx = idx })
	}()
	if conflict != nil {
		return conflict
	}
	t.Upstreams[r.Upstream] = struct{}{}
	t.routes = append(t.routes, r)
	t.byKey[key] = idx
	return nil
}

// matched records which route the mux chose.
type matched struct{ idx int }

func (*matched) Header() http.Header         { return http.Header{} }
func (*matched) Write(b []byte) (int, error) { return len(b), nil }
func (*matched) WriteHeader(int)             {}

// Match finds the route for a request, or nil. It also fills in the request's path variables, which Extract reads.
func (t *Table) Match(r *http.Request) *Route {
	if r.Method == http.MethodHead {
		return nil // the mux would answer HEAD as the GET it matches
	}
	m := &matched{idx: -1}
	t.mux.ServeHTTP(m, r)
	if m.idx < 0 {
		return nil
	}
	return &t.routes[m.idx]
}

// Lookup finds the route a fault addresses.
func (t *Table) Lookup(target, operation string) *Route {
	if i, ok := t.byKey[target+"/"+operation]; ok {
		return &t.routes[i]
	}
	return nil
}

// UpstreamNames lists the upstreams the table uses, sorted.
func (t *Table) UpstreamNames() []string {
	out := make([]string, 0, len(t.Upstreams))
	for n := range t.Upstreams {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Extract returns the route's attributes for a request, with path variables read from the matched pattern.
func (r *Route) Extract(req *http.Request) map[string]string {
	out := map[string]string{}
	for name, src := range r.Attrs {
		kind, key, _ := strings.Cut(src, ":")
		switch kind {
		case "path":
			out[name] = req.PathValue(key)
		case "query":
			out[name] = req.URL.Query().Get(key)
		case "header":
			out[name] = req.Header.Get(key)
		}
	}
	return out
}
