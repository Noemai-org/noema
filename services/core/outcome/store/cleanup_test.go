package store_test

import (
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"os"
	"regexp"
	"strings"
	"testing"
)

// AGENTS.md section 7 (a deadline on every call), EC-E16: a context detached from its caller's cancellation, as a rollback needs, always has a
// deadline of its own, so a stalled database cannot hold the connection after the answer is decided. Every WithoutCancel in this package's
// source is the argument of a WithTimeout.
func TestEveryDetachedContextInTheStoreHasItsOwnDeadline(t *testing.T) {
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
		if all, ok := len(bare.FindAll(src, -1)), len(bounded.FindAll(src, -1)); all != ok {
			t.Errorf("%s: %d detached contexts, %d of them with a deadline", f.Name(), all, ok)
		}
	}
}

// REQ-AGT-005, AGENTS.md section 7: the rollback bound is a safety parameter the deployment owner sets; a store without a positive one refuses
// to write, so a rollback can never run without a limit and no default is chosen here.
func TestAStoreWithoutARollbackTimeoutRefusesToWrite(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	f.st.RollbackTimeout = 0
	if res, err := f.v.Verify(ctx(t), f.req); err == nil {
		t.Fatalf("a store with no rollback timeout wrote a verdict: %+v", res)
	}
	if n := f.count(t, `SELECT count(*) FROM outcome_verdict`); n != 0 {
		t.Fatalf("%d verdicts stored by a store with no rollback timeout", n)
	}
}
