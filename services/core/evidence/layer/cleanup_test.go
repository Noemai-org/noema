package layer_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
)

// AGENTS.md section 7 (a deadline on every call), EC-E16 (review of noema#172): a context detached from its caller's cancellation, as a rollback
// needs, is always given its own deadline. A detached context with no limit would let a stalled database hold the connection after the answer
// is decided. This guard reads the package's own source: every WithoutCancel is the argument of a WithTimeout.
func TestEveryDetachedContextInTheLayerHasItsOwnDeadline(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	bare := regexp.MustCompile(`WithoutCancel\(`)
	bounded := regexp.MustCompile(`context\.WithTimeout\(context\.WithoutCancel\(`)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := len(bare.FindAll(src, -1)), len(bounded.FindAll(src, -1)); got != ok {
			t.Errorf("%s: %d detached contexts, %d of them with a deadline", f.Name(), got, ok)
		}
	}
}

// The failure path keeps the original error and a failed rollback is reported with it, never swallowed; a failed append leaves no connection
// held by the builder.
func TestAFailedAppendReleasesTheConnectionAndKeepsTheOriginalError(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, true)
	_, err := b.Record(ctx(t), observation(tenantA, layer.ReadOK))
	if err == nil || !strings.Contains(err.Error(), "the Record refused the event") {
		t.Fatalf("err = %v, want the original append error", err)
	}
	if n := b.Pool.Stat().AcquiredConns(); n != 0 {
		t.Fatalf("%d connections are still held after a failed append", n)
	}
}
