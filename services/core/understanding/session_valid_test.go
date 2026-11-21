package understanding_test

// The session rule the Interpreter uses before it calls the model service is the gate's own: Session.Valid() is false exactly when Gate.Admit refuses the same session as
// SESSION_INVALID. A second copy of the rule could drift from it. REQ-INP-002, INV-12. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
)

func TestTheSessionRuleOfTheCallersIsTheGatesOwn(t *testing.T) {
	good := understanding.Session{PrincipalID: "usr_88213", TenantID: "ten_demo", AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "noema-poc-idp"}
	sessions := map[string]understanding.Session{"good": good}
	for name, mutate := range map[string]func(s *understanding.Session){
		"empty":                func(s *understanding.Session) { *s = understanding.Session{} },
		"principal type space": func(s *understanding.Session) { s.PrincipalType = "End User" },
		"tenant no prefix":     func(s *understanding.Session) { s.TenantID = "demo" },
		"authn other kind":     func(s *understanding.Session) { s.AuthnRef = "tok_7a1b" },
		"asserted_by capitals": func(s *understanding.Session) { s.AssertedBy = "Noema IdP" },
		"principal id short":   func(s *understanding.Session) { s.PrincipalID = "usr_" },
	} {
		s := good
		mutate(&s)
		sessions[name] = s
	}
	g, err := understanding.NewGate(64<<10, testPack{})
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range sessions {
		_, err := g.Admit([]byte(`{}`), s, "corr_c1")
		var rej *understanding.Rejection
		refusedAsSession := errors.As(err, &rej) && len(rej.Reasons) == 1 && rej.Reasons[0] == understanding.ReasonIdentityNotInOrder
		if s.Valid() == refusedAsSession {
			t.Errorf("%s: Valid() = %v but Admit refused it as SESSION_INVALID = %v; the two must be opposites", name, s.Valid(), refusedAsSession)
		}
	}
}
