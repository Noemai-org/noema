package main

// STM-01 final composition, the adapters that are on main (the Founder's order of 20 Nov; A330, A333, A341, A442 interim, relayed, not yet committed). The Trust adapter source builds
// the adapters that main code gives from the pack (Capabilities, Scope, Authority), and every other one it cannot build is named in one startup error: nothing is invented, no fake
// and no quiet nil (INV-03, INV-16, AGENT-16). The pack is the manifest-verified pack of noema#773, which is not on main: production has no pack source and says so by name. The
// confirmation route (noema#754) is mounted only with its component and logs refusals through the process logger. Every value is SYNTHETIC (the fintech pack under /packs is synthetic).
// REQ-TRU-002, REQ-TRU-004, REQ-TRU-005, REQ-TRU-009, REQ-SEC-003; INV-03, INV-12, INV-16, AGENT-13, AGENT-16.

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const syntheticEscalationGraph = "fin.graph.escalate"

func fixturePack(Env) (*trustpack.Pack, error) {
	return trustpack.LoadPack(os.DirFS(filepath.Join("..", "..", "..", "..", "packs")), "fintech_v1.json")
}

func mustPack(t *testing.T) *trustpack.Pack {
	t.Helper()
	p, err := fixturePack(Env{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func adaptersAt(t *testing.T, pack PackSource, e envMap) (TrustAdapters, error) {
	t.Helper()
	return trustAdaptersSource(pack)(Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, nil, clock.NewManual(time.Unix(1_800_000_000, 0)))
}

func unbuilt(t *testing.T, err error) []string {
	t.Helper()
	var u *UnbuiltAdaptersError
	if !errors.As(err, &u) {
		t.Fatalf("error %v (%T); want an UnbuiltAdaptersError", err, err)
	}
	return u.Names
}

// The adapters main code builds from a pack are real: Capabilities, Scope and Authority over the pack, with the escalation graph the setting names (A346). The rest are named, once, in
// one error, in the order of the TrustAdapters struct; none is built from a fake. REQ-TRU-004, REQ-TRU-005; INV-16.
func TestTheAdaptersFromThePackAreBuiltAndEveryOtherOneIsNamed(t *testing.T) {
	a, err := adaptersAt(t, fixturePack, envMap{EnvTrustEscalationGraph: syntheticEscalationGraph})
	names := unbuilt(t, err)
	if _, ok := a.Capabilities.(trustpack.Capabilities); !ok {
		t.Errorf("Capabilities is %T, want trustpack.Capabilities", a.Capabilities)
	}
	if _, ok := a.Scope.(trustpack.Scope); !ok {
		t.Errorf("Scope is %T, want trustpack.Scope", a.Scope)
	}
	if _, ok := a.Authority.(trustpack.Authority); !ok {
		t.Errorf("Authority is %T, want trustpack.Authority", a.Authority)
	}
	if a.Clock == nil {
		t.Error("no clock")
	}
	want := []string{"Intents", "Writes", "Candidates", "Ownership", "Evidence", "Reconciler", "Policies", "Clocks", "Confirmations", "Planner", "Recorder"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("not built %v; want %v", names, want)
	}
	if a.PackVersion == "" || a.PackVersion != mustPack(t).Ref() {
		t.Errorf("PackVersion %q, want the pack's own %q (A377 item 6)", a.PackVersion, mustPack(t).Ref())
	}
	if a.Intents != nil || a.Writes != nil || a.Candidates != nil || a.Ownership != nil || a.Evidence != nil || a.Reconciler != nil || a.Policies != nil || a.Clocks != nil ||
		a.Confirmations != nil || a.Planner != nil || a.Recorder != nil {
		t.Errorf("an adapter is set that main cannot build: %+v", a)
	}
}

// Without a pack nothing that reads the pack can be built: the error names the pack and every adapter, the three of the pack included. Production has no pack source until noema#773
// is on main (A333), so a process asked for Trust stops and says what it lacks. REQ-TRU-002; INV-03, AGENT-16.
func TestWithoutAPackEveryAdapterIsNamedAndProductionHasNoPackYet(t *testing.T) {
	_, err := adaptersAt(t, productionPack, envMap{EnvTrustEscalationGraph: syntheticEscalationGraph})
	names := unbuilt(t, err)
	want := []string{"Pack", "Intents", "Writes", "Capabilities", "Scope", "Authority", "Candidates", "Ownership", "Evidence", "Reconciler", "Policies", "Clocks", "Confirmations", "Planner", "Recorder", "PackVersion"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("not built %v; want %v", names, want)
	}
	a, perr := productionDeps().TrustAdapters(Env{Getenv: envMap{}.get}, nil, clock.NewManual(time.Unix(1_800_000_000, 0)))
	if len(unbuilt(t, perr)) != len(want) || a.Capabilities != nil || a.Intents != nil {
		t.Errorf("production adapters %+v, error %v; want every adapter named and none built", a, perr)
	}
	if !errors.Is(err, errPackNotConfigured) {
		t.Errorf("error %v does not carry the pack refusal as its cause", err)
	}
}

// The escalation graph is a setting with no default (A330, A346): missing is an error that names the setting, a graph the pack does not have is one too, and no adapter is returned.
func TestTheEscalationGraphSettingIsRequiredAndMustNameAGraphOfThePack(t *testing.T) {
	for name, c := range map[string]struct {
		env  envMap
		want string
	}{
		"missing": {envMap{}, EnvTrustEscalationGraph},
		"unknown": {envMap{EnvTrustEscalationGraph: "fin.graph.nope"}, EnvTrustEscalationGraph},
	} {
		a, err := adaptersAt(t, fixturePack, c.env)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v; want one naming %s", name, err, c.want)
		}
		var u *UnbuiltAdaptersError
		if errors.As(err, &u) {
			t.Errorf("%s: the error is the list of waiting adapters, not a setting error: %v", name, err)
		}
		if a.Capabilities != nil || a.Scope != nil || a.Authority != nil {
			t.Errorf("%s: adapters built with an error: %+v", name, a)
		}
	}
}

// A pack that fails to load is not a named gap: the pack's own error comes back as it is, never as a list of waiting adapters, and the process stops (INV-16; interim, relayed, not
// yet committed: EL of 20 Nov).
func TestAPackThatCannotBeLoadedIsAStartupErrorThatIsNotQuiet(t *testing.T) {
	bad := errors.New("cmd/core: synthetic pack refusal")
	_, err := adaptersAt(t, func(Env) (*trustpack.Pack, error) { return nil, bad }, envMap{EnvTrustEscalationGraph: syntheticEscalationGraph})
	var u *UnbuiltAdaptersError
	if !errors.Is(err, bad) || errors.As(err, &u) {
		t.Fatalf("error %v; want the pack's own error and not a list of waiting adapters", err)
	}
}

// The confirmation route is mounted only with its component, with the pattern of PERD 39.2. A341; REQ-TRU-009.
func TestTheConfirmationRouteIsMountedOnlyWithItsComponent(t *testing.T) {
	clk := clock.NewManual(apiEpoch)
	src := ids.New(clk, nil)
	if routes := apiRoutes(Components{}, src); len(routes) != 0 {
		t.Fatalf("routes %d with no component", len(routes))
	}
	routes := apiRoutes(Components{Confirmations: &confirmationRouteConfig{}}, src)
	if len(routes) != 1 || routes[0].Pattern != "POST /v1/confirmations/{confirmation_request_id}" {
		t.Fatalf("routes %+v; want the confirmation route", routes)
	}
}

// The mounted route answers through the whole edge chain and logs a refusal through the process logger the composition gives (noema#754's Logger field): one fixed line, no body.
func TestTheMountedConfirmationRouteLogsRefusalsThroughTheProcessLogger(t *testing.T) {
	var buf bytes.Buffer
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	rq, rc := confRequest("MAKER_CHECKER"), &confAnswers{}
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = confTenant
	c := Components{Confirmations: &confirmationRouteConfig{Requests: rq, Answers: rc, Clock: clk, Timeout: time.Second}, AccessLog: slog.New(slog.NewJSONHandler(&buf, nil))}
	h, err := buildAPI(c, Env{Getenv: e.get, ReadFile: os.ReadFile}, clk, ids.New(clk, nil))
	if err != nil || h == nil {
		t.Fatalf("buildAPI: %v, %v", h, err)
	}
	if rec := confPost(h, confToken(iss, session.RoleApprover, confRequester), confPath, confAnswer(confRequestID, "ACCEPT")); rec.Code != http.StatusForbidden {
		t.Fatalf("self-approval: %d %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil || m["msg"] != "confirmation answer refused: not eligible" || m["outcome"] != "not_eligible" {
		t.Fatalf("log %q, error %v; want the one fixed line", buf.String(), err)
	}
	if len(rc.got) != 0 {
		t.Error("a refused answer reached the store")
	}
}

// A confirmation route with no logger is a startup error, as the Record reader without one is: a route that logs nothing is not mounted quietly (REQ-AUD-003, AGENT-13).
func TestAConfirmationRouteWithoutALoggerIsAStartupError(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	e := sessionEnv(t, iss)
	_, err := buildAPI(Components{Confirmations: &confirmationRouteConfig{}}, Env{Getenv: e.get, ReadFile: os.ReadFile}, clk, ids.New(clk, nil))
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("error %v; want one naming the confirmation route", err)
	}
}

// A pack source that returns no pack and no error is still no pack: the adapters are not built from nothing (INV-16).
func TestAPackSourceThatReturnsNothingIsNoPack(t *testing.T) {
	a, err := adaptersAt(t, func(Env) (*trustpack.Pack, error) { return nil, nil }, envMap{EnvTrustEscalationGraph: syntheticEscalationGraph})
	if names := unbuilt(t, err); names[0] != "Pack" || a.Capabilities != nil {
		t.Fatalf("names %v, adapters %+v; want the pack named first and nothing built", names, a)
	}
}

// With several adapters missing the error names the first in the struct's order, and unsetAdapters names all of them in that order. AGENT-16.
func TestCheckTrustAdaptersNamesTheFirstMissingInStructOrder(t *testing.T) {
	err := checkTrustAdapters(TrustAdapters{Intents: fakeIntents{}, Clock: clock.NewManual(time.Unix(1_800_000_000, 0))})
	if err == nil || !strings.Contains(err.Error(), "the Writes adapter") {
		t.Fatalf("error %v; want one naming Writes", err)
	}
	if got := unsetAdapters(TrustAdapters{Clock: clock.NewManual(time.Unix(1_800_000_000, 0))}); got[0] != "Intents" || got[len(got)-1] != "PackVersion" || len(got) != 15 {
		t.Fatalf("unset %v; want 15 from Intents to PackVersion", got)
	}
}
