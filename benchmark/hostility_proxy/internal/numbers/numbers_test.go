package numbers

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func mapper(t testing.TB, key string) *Mapper {
	t.Helper()
	m, err := New([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const key = "number-key-number-key-number-key-0001"

// REQ-ENV-005: the same Tushe number always maps to the same synthetic number, in every process and every run.
// This pins the mapping: a change of construction or key handling changes these values and fails the test.
func TestMappingIsStableAcrossRuns(t *testing.T) {
	m := mapper(t, key)
	want := map[string]string{}
	for _, real := range []string{"9999000000", "9999041234", "9999999999", "9999100200"} {
		s, ok := m.Synthetic(real)
		if !ok {
			t.Fatal(real)
		}
		want[real] = s
	}
	again := mapper(t, key)
	for real, s := range want {
		if got, _ := again.Synthetic(real); got != s {
			t.Errorf("%s: %s then %s", real, s, got)
		}
	}
	other := mapper(t, key+"x")
	differ := 0
	for real, s := range want {
		if got, _ := other.Synthetic(real); got != s {
			differ++
		}
	}
	if differ != len(want) {
		t.Error("another key must give another mapping")
	}
	pinned := map[string]string{}
	for real := range want {
		pinned[real], _ = m.Synthetic(real)
	}
	for _, s := range pinned {
		if len(s) != 10 {
			t.Errorf("synthetic number %q is not ten digits", s)
		}
	}
	golden, _ := m.Synthetic("9999000000")
	if golden != goldenFor0 {
		t.Errorf("9999000000 maps to %s; the pinned value is %s. Changing the construction changes every synthetic number a past run produced", golden, goldenFor0)
	}
}

// goldenFor0 is the synthetic number of 9999000000 under key. Computed once from this construction.
const goldenFor0 = "0999228089"

func TestSyntheticNeverLooksLikeATestNumberAndRoundTrips(t *testing.T) {
	m := mapper(t, key)
	rng := rand.New(rand.NewSource(1))
	seen := map[string]string{}
	for i := 0; i < 20000; i++ {
		real := fmt.Sprintf("9999%06d", rng.Intn(1000000))
		s, ok := m.Synthetic(real)
		if !ok {
			t.Fatalf("%s has no synthetic number", real)
		}
		if IsTestNumber(s) || strings.HasPrefix(s, "9999") {
			t.Fatalf("%s maps to %s, which still looks like a test number", real, s)
		}
		back, ok := m.Real(s)
		if !ok || back != real {
			t.Fatalf("%s -> %s -> %s (%v)", real, s, back, ok)
		}
		if prev, dup := seen[s]; dup && prev != real {
			t.Fatalf("two test numbers share %s: %s and %s", s, prev, real)
		}
		seen[s] = real
	}
}

func TestEncryptionIsABijection(t *testing.T) {
	m := mapper(t, key)
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 20000; i++ {
		n := uint64(rng.Int63n(space))
		if got := m.dec(m.enc(n)); got != n {
			t.Fatalf("dec(enc(%d)) = %d", n, got)
		}
	}
}

// A number the proxy never issued is not rewritten back: the inverse checks the walk forward.
func TestNumbersNeverIssuedStayAsTheyAre(t *testing.T) {
	m := mapper(t, key)
	rng := rand.New(rand.NewSource(3))
	changed := 0
	const n = 200000
	for i := 0; i < n; i++ {
		s := fmt.Sprintf("%010d", rng.Int63n(space))
		if IsTestNumber(s) {
			continue
		}
		if real, ok := m.Real(s); ok {
			changed++
			if back, _ := m.Synthetic(real); back != s {
				t.Fatalf("Real(%s)=%s but Synthetic gives %s", s, real, back)
			}
		}
	}
	// About 1 in 10,000 ten-digit numbers is a synthetic number: expect about 20 of 200,000, never thousands.
	if changed > 200 {
		t.Fatalf("%d of %d arbitrary numbers were treated as synthetic", changed, n)
	}
}

func TestRewriteTextOnlyTouchesTenDigitRuns(t *testing.T) {
	m := mapper(t, key)
	syn, _ := m.Synthetic("9999041234")
	in := `{"account":"9999041234","amount_minor":"999904123400","small":"99990412","ref":"TX9999041234","phone":"0803 999 9041234","list":["9999041234","9999041234"]}`
	out, st := m.RewriteText(in, ToSynthetic)
	if st.Rewritten != 4 || strings.Contains(out, "9999041234") && !strings.Contains(out, "999904123400") {
		t.Fatalf("%d rewritten: %s", st.Rewritten, out)
	}
	for _, keep := range []string{`"999904123400"`, `"99990412"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("%s must be untouched: %s", keep, out)
		}
	}
	if !strings.Contains(out, `"`+syn+`"`) || !strings.Contains(out, "TX"+syn) {
		t.Errorf("synthetic number missing: %s", out)
	}
	if ContainsTestNumber(out) {
		t.Fatalf("a test number survived: %s", out)
	}
	if !ContainsTestNumber(in) {
		t.Fatal("the scan must find a test number in the input")
	}
	back, st2 := m.RewriteText(out, ToReal)
	if back != in || st2.Rewritten != 4 {
		t.Fatalf("round trip differs (%d):\n%s\n%s", st2.Rewritten, in, back)
	}
}

func TestShortKeyRefused(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("a short key must be refused")
	}
}
