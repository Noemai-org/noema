package admissionsport_test

// STM-01 (A161, A169; REQ-AUD-001, REQ-SEC-004; INV-12): the adapter from the Record-backed reader (record/admission) to the orchestrator's Admissions port. It asks the
// reader for the session's tenant and the turn's reference only, hands back what the Record holds and nothing else, and refuses an answer about another tenant. All data is
// SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	admissionsport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/admissions"
	"github.com/Noemai-org/noema/services/core/record/admission"
)

type fakeReader struct {
	asked [][2]string
	found admission.Found
	err   error
}

func (f *fakeReader) Lookup(_ context.Context, tenantID, eventID string) (admission.Found, error) {
	f.asked = append(f.asked, [2]string{tenantID, eventID})
	return f.found, f.err
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

var sess = orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_01"}

const ref = "recev_01ABC"

func foundFor(tenant string) admission.Found {
	return admission.Found{EventType: "INPUT_RECEIVED", TenantID: tenant, PrincipalID: "usr_01", CorrelationID: "corr_ABC123", RequestID: "0a1b2c3d-0000-4000-8000-000000000001"}
}

func TestItAsksTheReaderForTheSessionsTenantAndTheTurnsReferenceAndNothingElse(t *testing.T) {
	r := &fakeReader{found: foundFor("ten_aa")}
	got, err := admissionsport.Port{Reader: r}.Lookup(ctx(t), sess, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.asked) != 1 || r.asked[0] != [2]string{"ten_aa", ref} {
		t.Fatalf("the reader was asked %v, want exactly the session's tenant and the reference", r.asked)
	}
	want := orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: "ten_aa", PrincipalID: "usr_01", CorrelationID: "corr_ABC123", RequestID: "0a1b2c3d-0000-4000-8000-000000000001"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAReaderErrorIsTheAnswerAndIsNotRewrittenOrHidden(t *testing.T) {
	for _, e := range []error{admission.ErrNotFound, admission.ErrIntegrity, admission.ErrRequest, errors.New("boom")} {
		got, err := admissionsport.Port{Reader: &fakeReader{err: e}}.Lookup(ctx(t), sess, ref)
		if !errors.Is(err, e) || got != (orchestrator.Admission{}) {
			t.Errorf("reader error %v: got %+v, %v; want that error and an empty Admission", e, got, err)
		}
	}
}

// INV-12: what the reader answers about another tenant is never passed on, whatever the reader is.
func TestAnAnswerAboutAnotherTenantIsRefused(t *testing.T) {
	got, err := admissionsport.Port{Reader: &fakeReader{found: foundFor("ten_bb")}}.Lookup(ctx(t), sess, ref)
	if !errors.Is(err, admissionsport.ErrWrongTenant) || got != (orchestrator.Admission{}) {
		t.Fatalf("got %+v, %v; want ErrWrongTenant and an empty Admission", got, err)
	}
	if err != nil && (containsAny(err.Error(), "ten_bb", "usr_01", "corr_ABC123")) {
		t.Errorf("the error carries content of the answer: %v", err)
	}
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		for i := 0; i+len(p) <= len(s); i++ {
			if s[i:i+len(p)] == p {
				return true
			}
		}
	}
	return false
}

func TestAnUnusableCallIsRefusedBeforeTheReaderIsAsked(t *testing.T) {
	r := &fakeReader{found: foundFor("ten_aa")}
	for name, c := range map[string]struct {
		p admissionsport.Port
		s orchestrator.Session
		r string
	}{
		"no reader":     {admissionsport.Port{}, sess, ref},
		"no tenant":     {admissionsport.Port{Reader: r}, orchestrator.Session{PrincipalID: "usr_01"}, ref},
		"no reference":  {admissionsport.Port{Reader: r}, sess, ""},
		"empty session": {admissionsport.Port{Reader: r}, orchestrator.Session{}, ref},
	} {
		if _, err := c.p.Lookup(ctx(t), c.s, c.r); !errors.Is(err, admissionsport.ErrNotUsable) {
			t.Errorf("%s: got %v, want ErrNotUsable", name, err)
		}
	}
	if len(r.asked) != 0 {
		t.Errorf("the reader was asked %v for an unusable call", r.asked)
	}
}
