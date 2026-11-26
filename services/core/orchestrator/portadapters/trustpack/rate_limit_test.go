package trustpack_test

// TRU-11 (REQ-TRU-012; PERD 23.3 T11): the rate limit a capability's descriptor declares, read from the loaded pack and from nowhere else. The expectation is read from the pack
// file by a reader that does not use the package under test. SYNTHETIC.

import (
	"encoding/json"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

func TestTheRateLimitIsTheDescriptorsOwnForEveryAllowedCapabilityAndNoneForAnythingElse(t *testing.T) {
	var raw struct {
		Caps []struct {
			ID         string `json:"capability_id"`
			Prohibited bool   `json:"prohibited"`
			Limits     struct {
				Rate int `json:"rate_per_principal_per_hour"`
			} `json:"limits"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(readPackFile(t, packFileName), &raw); err != nil {
		t.Fatal(err)
	}
	p := loadReal(t)
	allowed := 0
	for _, c := range raw.Caps {
		got, declared := p.RatePerPrincipalPerHour(c.ID)
		switch {
		case c.Prohibited:
			if declared || got != 0 {
				t.Errorf("%s is prohibited and has no rate: got %d, %v", c.ID, got, declared)
			}
		case c.Limits.Rate > 0:
			allowed++
			if !declared || got != c.Limits.Rate {
				t.Errorf("%s: got %d, %v; want %d, true", c.ID, got, declared, c.Limits.Rate)
			}
		}
	}
	if allowed == 0 {
		t.Fatal("the pack declares no rate limit: the test would pass without testing anything")
	}
	for _, id := range []string{"", " ", "x.cap.unknown", raw.Caps[0].ID + "x", "FIN.CAP.LOOKUP_TRANSACTION"} {
		if got, declared := p.RatePerPrincipalPerHour(id); declared || got != 0 {
			t.Errorf("%q: got %d, %v; want no limit for an ID the registry does not know (exact match, no folding)", id, got, declared)
		}
	}
}

func TestAPackThatIsNotLoadedLimitsNothing(t *testing.T) {
	var p *trustpack.Pack
	if got, declared := p.RatePerPrincipalPerHour("fin.cap.create_escalation_case"); declared || got != 0 {
		t.Errorf("a nil pack: %d, %v", got, declared)
	}
	var zero trustpack.Pack
	if got, declared := zero.RatePerPrincipalPerHour("fin.cap.create_escalation_case"); declared || got != 0 {
		t.Errorf("a zero pack: %d, %v", got, declared)
	}
}
