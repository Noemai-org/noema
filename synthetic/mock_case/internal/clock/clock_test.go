package clock

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	good := map[string]time.Duration{
		"PT31S": 31 * time.Second, "PT5M": 5 * time.Minute, "PT1H": time.Hour, "P1D": 24 * time.Hour,
		"P1DT2H3M4S": 24*time.Hour + 2*time.Hour + 3*time.Minute + 4*time.Second, "PT0S": 0,
	}
	for in, want := range good {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "P", "PT", "P1DT", "31S", "PT-5S", "P1Y", "P1M", "P1W", "PT1.5S", "pt5s", "PT5S "} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) accepted a duration it must refuse", in)
		}
	}
}

func TestSimulatedMovesOnlyWhenTold(t *testing.T) {
	start := time.Date(2026, 10, 1, 9, 40, 0, 0, time.FixedZone("WAT", 3600))
	c := NewSimulated(start)
	if !c.Simulated() || !c.Now().Equal(start) || c.Now().Location() != time.UTC {
		t.Fatalf("clock must start at the start instant, in UTC: %v", c.Now())
	}
	if err := c.Advance(31 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Now(), start.Add(31*time.Second); !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if err := c.Advance(-time.Second); err == nil {
		t.Fatal("a simulated clock must refuse to run backwards")
	}
}

func TestSystemIsUTCAndNotSimulated(t *testing.T) {
	if (System{}).Simulated() || (System{}).Now().Location() != time.UTC {
		t.Fatal("system clock must be UTC and not simulated")
	}
}
