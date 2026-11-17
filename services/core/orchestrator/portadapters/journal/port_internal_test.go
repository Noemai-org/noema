package journalport

// C1's read of noema#576 (P2): the journal port had no test file. These are the branches that need no database (a port that cannot be used, a context with no deadline, a
// begin that fails, and the reading of the idempotency key). The two branches that need a real chain (a Record that does not verify, and a GRANT_SPENT that names no key) are
// tests of the Record's rows and run with the pgsodium database in CI: they are the next commit, not claimed here. REQ-AUD-003, REQ-EXE-006, INV-07. SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

type failingBegin struct{ called int }

func (f *failingBegin) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	f.called++
	return nil, errors.New("synthetic begin failure")
}

var sess = orchestrator.Session{TenantID: "ten_aa", PrincipalID: "prn_1"}

func withDeadline() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	_ = cancel
	return ctx
}

func TestAPortThatCannotBeUsedRefusesBeforeTouchingTheDatabase(t *testing.T) {
	db := &failingBegin{}
	for name, c := range map[string]struct {
		p          Port
		s          orchestrator.Session
		inv, grant string
	}{
		"no database":      {Port{}, sess, "inv_AA11", "grt_AA11"},
		"no tenant":        {Port{DB: db}, orchestrator.Session{PrincipalID: "prn_1"}, "inv_AA11", "grt_AA11"},
		"no principal":     {Port{DB: db}, orchestrator.Session{TenantID: "ten_aa"}, "inv_AA11", "grt_AA11"},
		"no investigation": {Port{DB: db}, sess, "", "grt_AA11"},
		"no grant":         {Port{DB: db}, sess, "inv_AA11", ""},
	} {
		if _, err := c.p.Progress(withDeadline(), c.s, c.inv, c.grant); !errors.Is(err, ErrNotUsable) {
			t.Errorf("%s: %v, want ErrNotUsable", name, err)
		}
	}
	if db.called != 0 {
		t.Errorf("the database was touched %d times by a refused call", db.called)
	}
}

func TestAContextWithNoDeadlineIsRefusedBeforeTheDatabase(t *testing.T) {
	db := &failingBegin{}
	if _, err := (Port{DB: db}).Progress(context.Background(), sess, "inv_AA11", "grt_AA11"); !errors.Is(err, ErrNotUsable) || db.called != 0 {
		t.Fatalf("err %v, begins %d; want ErrNotUsable and no begin", err, db.called)
	}
}

// A database that cannot begin is an error, never an empty Progress (the Record is the truth; silence is not "nothing happened", INV-07).
func TestADatabaseThatCannotBeginIsAnErrorNeverAnEmptyProgress(t *testing.T) {
	db := &failingBegin{}
	p, err := (Port{DB: db}).Progress(withDeadline(), sess, "inv_AA11", "grt_AA11")
	if err == nil || errors.Is(err, ErrNotUsable) || db.called != 1 || p != (orchestrator.Progress{}) {
		t.Fatalf("progress %+v, err %v, begins %d; want an error from the begin and a zero Progress", p, err, db.called)
	}
}

func str(s string) *string { return &s }

// The key of a GRANT_SPENT payload: absent, not JSON, a non-string and a missing member all read as "" (the port then refuses the grant: it will not count dispatches by a key it
// cannot read); a key reads as itself.
func TestTheIdempotencyKeyOfAPayload(t *testing.T) {
	for name, c := range map[string]struct {
		payload *string
		want    string
	}{
		"no payload":       {nil, ""},
		"not json":         {str("{not json"), ""},
		"empty object":     {str(`{}`), ""},
		"a number":         {str(`{"idempotency_key":5}`), ""},
		"another member":   {str(`{"key":"idk_x"}`), ""},
		"the key":          {str(`{"idempotency_key":"idk_v1_k1"}`), "idk_v1_k1"},
		"the key and more": {str(`{"a":1,"idempotency_key":"idk_v1_k2","b":"x"}`), "idk_v1_k2"},
	} {
		if got := idempotencyKey(c.payload); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}
