package registry_test

// REQ-CAP-002 acceptance: "a property test SHALL assert that each prohibited ID exists and is never callable". INV-05, INV-18.

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// For every ID drawn (registered, prohibited, or made up): a handle exists exactly when the ID is registered and not
// prohibited; a prohibited ID never yields a handle, whatever case, spacing or prefix is tried on it.
func TestProhibitedIDsAreNeverCallable(t *testing.T) {
	r := loadFintech(t)
	prohibited := r.Prohibited()
	if len(prohibited) == 0 {
		t.Fatal("no prohibited IDs to test")
	}
	rapid.Check(t, func(rt *rapid.T) {
		id := rapid.OneOf(
			rapid.SampledFrom(prohibited),
			rapid.SampledFrom(allIDs(t, r)),
			rapid.StringN(0, 60, 80),
		).Draw(rt, "id")
		// A deliberate disguise of the ID: case, padding, a path prefix.
		switch rapid.IntRange(0, 3).Draw(rt, "disguise") {
		case 1:
			id = strings.ToUpper(id)
		case 2:
			id = " " + id + "\n"
		case 3:
			id = "../" + id
		}
		c, err := r.Callable(id)
		switch {
		case r.IsProhibited(id):
			if err == nil || c.ID() != "" {
				rt.Fatalf("prohibited %q gave a handle (%q, %v)", id, c.ID(), err)
			}
		case r.Registered(id):
			if err != nil || c.ID() != id {
				rt.Fatalf("allowed %q gave (%q, %v)", id, c.ID(), err)
			}
		default:
			if err == nil {
				rt.Fatalf("unregistered %q gave a handle", id)
			}
		}
		for _, p := range prohibited {
			if id == p && err == nil {
				rt.Fatalf("exactly the prohibited ID %q was callable", p)
			}
		}
	})
}

func allIDs(t testing.TB, r interface{ IDs() []string }) []string {
	t.Helper()
	ids := r.IDs()
	if len(ids) == 0 {
		t.Fatal("no IDs")
	}
	return ids
}
