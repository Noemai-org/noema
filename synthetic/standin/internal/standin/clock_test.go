package standin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/standin/internal/standin"
)

// A run on the simulated clock is reproducible (REQ-ENV-004, INV-21): the stand-in's identity check must use the same clock as the
// client that mints the assertion, not the real one. The service starts its clock at STANDIN_CLOCK_START and moves it only when the
// benchmark says so (the shape of the hostility proxy's clock: POST /control/v1/clock/advance {"by": "PT10S"}). SYNTHETIC (AGENT-25).

var simStart = time.Date(2026, 10, 1, 9, 40, 0, 0, time.UTC)

type simRig struct {
	t       *testing.T
	data    *httptest.Server
	control *httptest.Server
}

func newSimRig(t *testing.T, start time.Time) *simRig {
	t.Helper()
	s, err := standin.New(standin.Options{BenchToken: benchToken, RuntimeToken: runtimeToken, PrincipalKey: principalKey, TenantID: tenant, ClockStart: start})
	if err != nil {
		t.Fatal(err)
	}
	r := &simRig{t: t, data: httptest.NewServer(s.DataHandler()), control: httptest.NewServer(s.ControlHandler())}
	t.Cleanup(r.data.Close)
	t.Cleanup(r.control.Close)
	golden := newRig(t).golden
	if put := do(t, http.MethodPut, r.control.URL+"/control/v1/standin", benchToken, "", golden); put.status != 200 {
		t.Fatalf("load: %d %s", put.status, put.body)
	}
	return r
}

func (r *simRig) readAs(principal string, exp int64) int {
	r.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, r.data.URL+"/standin/v1/principal", nil)
	req.Header.Set("Authorization", "Bearer "+runtimeToken)
	req.Header.Set("X-Principal-Assertion", mint(principalKey, "standin", principal, tenant, exp))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

func (r *simRig) advance(by string) reply {
	r.t.Helper()
	return do(r.t, http.MethodPost, r.control.URL+"/control/v1/clock/advance", benchToken, "", []byte(`{"by":"`+by+`"}`))
}

func TestAnAssertionOnTheSimulatedClockIsAcceptedAndThenExpiresWhenTheClockAdvances(t *testing.T) {
	r := newSimRig(t, simStart)
	exp := simStart.Unix() + 60
	if got := r.readAs("usr_a", exp); got != 200 {
		t.Fatalf("an assertion minted on the simulated clock was refused: %d (the service is using the real clock)", got)
	}
	if res := r.advance("PT2M"); res.status != 200 {
		t.Fatalf("advance: %d %s", res.status, res.body)
	}
	if got := r.readAs("usr_a", exp); got != 401 {
		t.Fatalf("after the clock passed the expiry the same assertion gave %d, want 401", got)
	}
	if got := r.readAs("usr_a", simStart.Unix()+120+60); got != 200 {
		t.Fatalf("a fresh assertion on the advanced clock gave %d, want 200", got)
	}
}

func TestAnAssertionFromTheRealClockIsRefusedOnTheSimulatedOne(t *testing.T) {
	r := newSimRig(t, simStart)
	if got := r.readAs("usr_a", time.Now().Unix()+60); got != 401 {
		t.Fatalf("an assertion from the real clock gave %d on a simulated 2026-10-01 clock, want 401 (too far in the future)", got)
	}
}

func TestTheClockIsReadAndNeverGoesBackwards(t *testing.T) {
	r := newSimRig(t, simStart)
	res := do(t, http.MethodGet, r.control.URL+"/control/v1/clock", benchToken, "", nil)
	if res.status != 200 || !strings.Contains(res.body, `"simulated":true`) || !strings.Contains(res.body, `"clock_source":"SYNTHETIC"`) || !strings.Contains(res.body, "2026-10-01T09:40:00Z") {
		t.Fatalf("clock: %d %s", res.status, res.body)
	}
	for _, bad := range []string{"-PT1S", "PT0S", "PT25H", "1h", "PT", "", "P1D"} {
		if res := r.advance(bad); res.status != 400 {
			t.Errorf("advance by %q gave %d, want 400", bad, res.status)
		}
	}
}

func TestOnlyTheBenchCredentialMovesTheClock(t *testing.T) {
	r := newSimRig(t, simStart)
	for _, tok := range []string{"", runtimeToken, principalKey} {
		res := do(t, http.MethodPost, r.control.URL+"/control/v1/clock/advance", tok, "", []byte(`{"by":"PT1M"}`))
		if res.status != 401 {
			t.Errorf("token %q: %d, want 401", tok, res.status)
		}
	}
}

func TestARealClockCannotBeAdvanced(t *testing.T) {
	s, err := standin.New(standin.Options{BenchToken: benchToken, RuntimeToken: runtimeToken, PrincipalKey: principalKey, TenantID: tenant})
	if err != nil {
		t.Fatal(err)
	}
	control := httptest.NewServer(s.ControlHandler())
	t.Cleanup(control.Close)
	if res := do(t, http.MethodGet, control.URL+"/control/v1/clock", benchToken, "", nil); res.status != 200 || !strings.Contains(res.body, `"simulated":false`) {
		t.Fatalf("clock: %d %s", res.status, res.body)
	}
	if res := do(t, http.MethodPost, control.URL+"/control/v1/clock/advance", benchToken, "", []byte(`{"by":"PT1M"}`)); res.status != 409 {
		t.Fatalf("advancing a real clock gave %d, want 409", res.status)
	}
}

func TestAClockStartAndAClockFunctionTogetherAreRefused(t *testing.T) {
	_, err := standin.New(standin.Options{BenchToken: benchToken, RuntimeToken: runtimeToken, PrincipalKey: principalKey, TenantID: tenant, ClockStart: simStart, Now: func() time.Time { return simStart }})
	if err == nil {
		t.Fatal("two clocks are ambiguous: want an error")
	}
}

func TestTheClockStartComesFromTheEnvironmentAndMustBeRFC3339(t *testing.T) {
	good, rf := env(map[string]string{"STANDIN_CLOCK_START": "2026-10-01T09:40:00Z"})
	c, err := standin.LoadConfig(good, rf)
	if err != nil || !c.Options.ClockStart.Equal(simStart) {
		t.Fatalf("clock start: %v %v", c, err)
	}
	for _, bad := range []string{"yesterday", "2026-10-01", "1790847600"} {
		getenv, readFile := env(map[string]string{"STANDIN_CLOCK_START": bad})
		if _, err := standin.LoadConfig(getenv, readFile); err == nil {
			t.Errorf("STANDIN_CLOCK_START=%q must be refused", bad)
		}
	}
	unset, rf2 := env(nil)
	if c, err := standin.LoadConfig(unset, rf2); err != nil || !c.Options.ClockStart.IsZero() {
		t.Fatalf("unset means the real clock: %v %v", c, err)
	}
}
