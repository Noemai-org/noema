package synthetic_tushe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// ENV-06: "Each FaultPlan effect is observed by the adapter as specified" (REQ-ENV-005 as amended: UNAVAILABLE or a schema rejection,
// never a fact or a success). The exchanges are recorded, not written: testdata/standin_recordings.json is the real stand-in service
// (synthetic/standin, TestStandinRecordings) and testdata/standin_faulted_recordings.json is the real hostility proxy in front of those
// responses with one fault from a real fault plan (benchmark/hostility_proxy, TestStandinFaultedExchanges). Each has a drift guard in the
// module that makes it. SYNTHETIC (AGENT-25). Nothing here says anything about Tushe.

type recBase struct {
	Scenario  string `json:"scenario"`
	Principal string `json:"principal"`
	Request   struct{ Path, Query string }
	Response  struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Body        string `json:"body"`
	} `json:"response"`
}

type recFaulted struct {
	Scenario string `json:"scenario"`
	Base     string `json:"base"`
	Effect   string `json:"effect"`
	Request  struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	} `json:"request"`
	Response struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		RetryAfter  string `json:"retry_after"`
		Body        string `json:"body"`
	} `json:"response"`
}

func loadStandin[T any](t *testing.T, file string) []T {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Exchanges []T `json:"exchanges"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Exchanges
}

// outcome is what the adapter made of one read: the failure (empty when it returned an answer), the raw code and the provenance.
type outcome struct {
	failure     adapters.ReadFailure
	rawCode     string
	retryAfter  time.Duration
	answered    bool
	unavailable bool
	detail      string
	prov        adapters.Provenance
}

func readOne(t *testing.T, base string, principal, path, query string, status int, ctype, retryAfter, body string) outcome {
	t.Helper()
	f := newFakeStandin(t)
	recPath := path
	f.on(recPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("%s: the client sent %s", base, r.Method)
		}
		if query != "" && r.URL.RawQuery != query {
			t.Errorf("%s: query %q, recorded %q", base, r.URL.RawQuery, query)
		}
		w.Header().Set("Content-Type", ctype)
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	c := newStandinClient(t, f)
	ctx := sdl(t, 5*time.Second)
	sc := adapters.Scope{TenantID: "ten_aa", PrincipalID: principal}
	ref := ""
	if i := strings.Index(path, "/transactions/"); i >= 0 {
		ref = strings.Split(path[i+len("/transactions/"):], "/")[0]
	}
	var err error
	var o outcome
	switch {
	case strings.HasPrefix(base, "ledger"), strings.HasPrefix(base, "another"), strings.HasPrefix(base, "not_loaded"):
		var r adapters.LedgerRead
		r, err = c.LedgerEntries(ctx, sc, ref)
		o.answered, o.unavailable, o.prov = err == nil, r.Unavailable, r.Provenance
		o.detail = string(rune('0' + len(r.Entries)))
	case strings.HasPrefix(base, "switch"):
		var r adapters.SwitchRead
		r, err = c.SwitchStatus(ctx, sc, ref)
		o.answered, o.unavailable, o.prov = err == nil, r.Unavailable, r.Provenance
		o.detail = string(r.Record.SemanticStatus)
	case strings.HasPrefix(base, "destination"):
		var r adapters.DestinationRead
		r, err = c.DestinationStatus(ctx, sc, ref)
		o.answered, o.unavailable, o.prov = err == nil, r.Unavailable, r.Provenance
		o.detail = string(r.Status.CreditState) + "/" + string(r.Status.SettlementState)
	case strings.HasPrefix(base, "candidates"):
		var r adapters.CandidatePage
		r, err = c.FindCandidates(ctx, sc, adapters.TransactionQuery{AccountRef: "acc_" + principal, Limit: 10})
		o.answered, o.prov = err == nil, r.Provenance
		o.detail = string(rune('0'+len(r.Items))) + map[bool]string{true: "/complete", false: "/incomplete"}[r.Complete]
	default:
		t.Fatalf("no reader for %s", base)
	}
	if err != nil {
		re := sFail(t, err)
		o.failure, o.rawCode, o.retryAfter = re.Failure, re.RawCode, re.RetryAfter
	}
	return o
}

func TestTheAdapterReadsWhatTheRealStandinSends(t *testing.T) {
	want := map[string]struct {
		failure     adapters.ReadFailure
		rawCode     string
		unavailable bool
		detail      string
	}{
		"not_loaded":                     {adapters.ReadUnavailable, "503:STANDIN_NOT_LOADED", false, ""},
		"ledger_posted":                  {"", "", false, "1"},
		"ledger_unavailable":             {"", "", true, "0"},
		"switch_accepted":                {"", "", false, "ACCEPTED"},
		"switch_timed_out":               {"", "", false, "TIMED_OUT"},
		"destination_pending":            {"", "", false, "PENDING/PENDING"},
		"destination_credited":           {"", "", false, "CREDITED/SETTLED"},
		"candidates_of_usr_a":            {"", "", false, "3/complete"},
		"another_principals_transaction": {adapters.ReadNotFound, "404:NOT_FOUND", false, ""},
	}
	exs := loadStandin[recBase](t, "testdata/standin_recordings.json")
	if len(exs) != len(want) {
		t.Fatalf("%d recordings, expected %d: re-record (synthetic/standin, NOEMA_RECORD_STANDIN=1) and update this table", len(exs), len(want))
	}
	for _, ex := range exs {
		w, ok := want[ex.Scenario]
		if !ok {
			t.Fatalf("no expectation for the recorded scenario %q", ex.Scenario)
		}
		t.Run(ex.Scenario, func(t *testing.T) {
			o := readOne(t, ex.Scenario, ex.Principal, ex.Request.Path, ex.Request.Query, ex.Response.Status, ex.Response.ContentType, "", ex.Response.Body)
			if o.failure != w.failure || o.rawCode != w.rawCode {
				t.Fatalf("failure %q/%q, want %q/%q", o.failure, o.rawCode, w.failure, w.rawCode)
			}
			if w.failure == "" {
				sum := sha256.Sum256([]byte(ex.Response.Body))
				if !o.answered || o.unavailable != w.unavailable || o.detail != w.detail || o.prov.RawHash != "sha256:"+hex.EncodeToString(sum[:]) || o.prov.DataSource != "stand-in" || !o.prov.Synthetic {
					t.Fatalf("answer %+v, want unavailable=%v detail=%q and the hash of the wire bytes", o, w.unavailable, w.detail)
				}
			}
		})
	}
}

func TestEveryFaultFromTheRealProxyIsUnavailableOrASchemaRejectionNeverAFact(t *testing.T) {
	exs := loadStandin[recFaulted](t, "testdata/standin_faulted_recordings.json")
	base := map[string]recBase{}
	for _, b := range loadStandin[recBase](t, "testdata/standin_recordings.json") {
		base[b.Scenario] = b
	}
	if len(exs) != 32 {
		t.Fatalf("4 reads x 8 faults = 32 exchanges, found %d: re-record", len(exs))
	}
	seen := map[string]int{}
	for _, ex := range exs {
		b, ok := base[ex.Base]
		if !ok {
			t.Fatalf("%s: no base recording %q", ex.Scenario, ex.Base)
		}
		family := strings.TrimPrefix(ex.Scenario, ex.Base+"__")
		seen[family]++
		t.Run(ex.Scenario, func(t *testing.T) {
			o := readOne(t, ex.Base, b.Principal, ex.Request.Path, ex.Request.Query, ex.Response.Status, ex.Response.ContentType, ex.Response.RetryAfter, ex.Response.Body)
			if o.answered {
				t.Fatalf("a %s exchange was read as an answer: %+v", family, o)
			}
			switch family {
			case "http_503":
				if o.failure != adapters.ReadUnavailable || !strings.HasPrefix(o.rawCode, "503") {
					t.Fatalf("got %s/%s, want UNAVAILABLE 503", o.failure, o.rawCode)
				}
			case "http_429":
				if o.failure != adapters.ReadUnavailable || !strings.HasPrefix(o.rawCode, "429") || o.retryAfter != 7*time.Second {
					t.Fatalf("got %s/%s retry %v, want UNAVAILABLE 429 with Retry-After 7s", o.failure, o.rawCode, o.retryAfter)
				}
			default:
				if o.failure != adapters.ReadMalformed {
					t.Fatalf("got %s/%s, want MALFORMED (a schema rejection, which is UNKNOWN)", o.failure, o.rawCode)
				}
			}
		})
	}
	for _, family := range []string{"truncated_body", "invalid_json", "wrong_content_type", "empty_body_success", "extra_field", "missing_required_field", "http_503", "http_429"} {
		if seen[family] != 4 {
			t.Errorf("family %s has %d exchanges, want 4 (one per read)", family, seen[family])
		}
	}
}
