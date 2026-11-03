package verifier_test

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

func want(t *testing.T, res verifier.Result, v verifier.Verdict, r verifier.Reason) {
	t.Helper()
	if res.Verdict != v || res.Reason != r {
		t.Fatalf("verdict = %s/%s, want %s/%s", res.Verdict, res.Reason, v, r)
	}
}

// REQ-VER-001: Outcome reads the target and the predicate is true: ACHIEVED, with the reads it relied on and no
// recovery request.
func TestAchievedFromItsOwnReads(t *testing.T) {
	w := newWorld(t)
	res, wr := w.decided(t)
	want(t, res, verifier.Achieved, verifier.ReasonPredicateTrue)
	if wr.RecoveryRequestID != "" || wr.Watch != nil {
		t.Fatalf("ACHIEVED carries a recovery request %q or a watch", wr.RecoveryRequestID)
	}
	if wr.Verdict.PredicateResult == nil || !*wr.Verdict.PredicateResult || len(wr.Verdict.EvidenceReads) == 0 {
		t.Fatalf("ACHIEVED without a true predicate and evidence reads: %+v", wr.Verdict)
	}
	if w.read.reads() != 2 {
		t.Fatalf("reads = %d, want one by key and one by transaction", w.read.reads())
	}
}

// INV-04, AGENT-21: the receipt is a pointer, never proof. Whatever it claims, the verdict follows the target.
func TestTheReceiptIsNeverProof(t *testing.T) {
	for _, state := range []string{"ACKNOWLEDGED", "OUTCOME_UNKNOWN", "DISPATCHED", "REJECTED_BY_TARGET"} {
		t.Run(state+" with the effect present", func(t *testing.T) {
			w := newWorld(t, func(w *world) { w.store.receipt.State = state })
			res, _ := w.decided(t)
			want(t, res, verifier.Achieved, verifier.ReasonPredicateTrue)
		})
	}
	t.Run("OUTCOME_UNKNOWN with the effect absent", func(t *testing.T) {
		w := newWorld(t, func(w *world) { w.read.byKey, w.read.byTx = []step{complete()}, []step{complete()} })
		res, wr := w.decided(t)
		want(t, res, verifier.NotAchieved, verifier.ReasonVerifiedAbsent)
		if wr.RecoveryRequestID == "" {
			t.Fatal("NOT_ACHIEVED without a recovery request")
		}
	})
	t.Run("ACKNOWLEDGED with the effect absent is a postcondition the target did not keep", func(t *testing.T) {
		w := newWorld(t, func(w *world) {
			w.store.receipt.State = "ACKNOWLEDGED"
			w.read.byKey, w.read.byTx = []step{complete()}, []step{complete()}
		})
		res, _ := w.decided(t)
		want(t, res, verifier.NotAchieved, verifier.ReasonPostconditionUnchecked)
	})
}

// PERD 29.1, REQ-VER-001 acceptance: no verdict before dispatched_at + settle window, and no read either.
func TestNothingIsReadOrDecidedInsideTheSettleWindow(t *testing.T) {
	rng := rand.New(rand.NewSource(20261103))
	for i := 0; i < 200; i++ {
		offset := time.Duration(rng.Intn(60_000)) * time.Millisecond // 0 to 60 s after dispatch; the window is 30 s
		w := newWorld(t, at(offset))
		_, err := w.verify(t)
		inside := offset < 30*time.Second
		var open *verifier.ErrSettleWindowOpen
		switch {
		case inside && !errors.As(err, &open):
			t.Fatalf("offset %s: want ErrSettleWindowOpen, got %v", offset, err)
		case inside && (w.read.reads() != 0 || len(w.store.writes) != 0):
			t.Fatalf("offset %s: %d reads and %d writes inside the window", offset, w.read.reads(), len(w.store.writes))
		case !inside && err != nil:
			t.Fatalf("offset %s: %v", offset, err)
		case !inside && !w.store.writes[0].Verdict.VerifiedAt.Add(-30*time.Second).After(t0.Add(-time.Nanosecond)):
			t.Fatalf("offset %s: verified_at %s is inside the window", offset, w.store.writes[0].Verdict.VerifiedAt)
		}
	}
	w := newWorld(t, at(30*time.Second-time.Nanosecond))
	var open *verifier.ErrSettleWindowOpen
	if _, err := w.verify(t); !errors.As(err, &open) || !open.ReadyAt.Equal(t0.Add(30*time.Second)) {
		t.Fatalf("one nanosecond early: %v", err)
	}
}

// AGENT-16 (PERD 29.2): three retries after 1, 3 and 9 s of simulated time, then UNKNOWN with a recovery request.
func TestReadsAreRetriedOnTheBudgetThenUnknown(t *testing.T) {
	w := newWorld(t, func(w *world) { w.read.byKey = []step{down()} }) // the last step repeats: down for ever
	res, wr := w.decided(t)
	want(t, res, verifier.Unknown, verifier.ReasonReadsExhausted)
	if got := w.wait.waits; !reflect.DeepEqual(got, []time.Duration{time.Second, 3 * time.Second, 9 * time.Second}) {
		t.Fatalf("waits = %v", got)
	}
	if w.read.reads() != 4 {
		t.Fatalf("reads = %d, want the first and three retries", w.read.reads())
	}
	if wr.RecoveryRequestID == "" || len(wr.Verdict.EvidenceReads) != 0 {
		t.Fatalf("UNKNOWN: recovery %q, evidence reads %v", wr.RecoveryRequestID, wr.Verdict.EvidenceReads)
	}
	if len(wr.Reads) != 4 {
		t.Fatalf("VERIFICATION_READ records = %d, want one per attempt (4)", len(wr.Reads))
	}
}

func TestAReadThatRecoversWithinTheBudgetIsUsed(t *testing.T) {
	w := newWorld(t, func(w *world) { w.read.byKey = []step{down(), down(), complete(theCase())} })
	res, _ := w.decided(t)
	want(t, res, verifier.Achieved, verifier.ReasonPredicateTrue)
	if !reflect.DeepEqual(w.wait.waits, []time.Duration{time.Second, 3 * time.Second}) {
		t.Fatalf("waits = %v", w.wait.waits)
	}
}

// INV-19, REQ-VER-001: what is not a complete, consistent, verified read is UNKNOWN, never a negative and never a success.
func TestEvidenceThatCannotBeTrustedIsUnknown(t *testing.T) {
	other := func(f func(*verifier.Case)) verifier.Case { c := theCase(); f(&c); return c }
	cases := map[string]struct {
		key, tx []step
		reason  verifier.Reason
	}{
		"an incomplete page with no cursor":           {key: []step{{page: verifier.CasePage{Complete: false}}}, tx: []step{complete(theCase())}, reason: verifier.ReasonIncompleteRead},
		"a page that never completes":                 {key: []step{{page: verifier.CasePage{Complete: false, NextCursor: "c"}}}, tx: []step{complete(theCase())}, reason: verifier.ReasonIncompleteRead},
		"a malformed answer":                          {key: []step{{err: &verifier.ReadError{Kind: verifier.ReadMalformed}}}, tx: []step{complete(theCase())}, reason: verifier.ReasonMalformedRead},
		"a refused read":                              {key: []step{{err: &verifier.ReadError{Kind: verifier.ReadRefused}}}, tx: []step{complete(theCase())}, reason: verifier.ReasonReadRefused},
		"two cases for one key":                       {key: []step{complete(theCase(), other(func(c *verifier.Case) { c.CaseID = "case_2" }))}, tx: []step{complete(theCase())}, reason: verifier.ReasonInconsistent},
		"a case of another tenant":                    {key: []step{complete(other(func(c *verifier.Case) { c.TenantID = "ten_bb" }))}, tx: []step{complete(theCase())}, reason: verifier.ReasonInconsistent},
		"a case under another key":                    {key: []step{complete(other(func(c *verifier.Case) { c.IdempotencyKey = "idk_v1_other" }))}, tx: []step{complete(theCase())}, reason: verifier.ReasonInconsistent},
		"a case of another transaction":               {key: []step{complete(other(func(c *verifier.Case) { c.TransactionRef = "TX9" }))}, tx: []step{complete(theCase())}, reason: verifier.ReasonInconsistent},
		"a status the pack does not know":             {key: []step{complete(other(func(c *verifier.Case) { c.Status = "REOPENED" }))}, tx: []step{complete(other(func(c *verifier.Case) { c.Status = "REOPENED" }))}, reason: verifier.ReasonInconsistent},
		"the key's case missing from the transaction": {key: []step{complete(theCase())}, tx: []step{complete()}, reason: verifier.ReasonInconsistent},
		"no case for the key, yet one for the intent": {key: []step{complete()}, tx: []step{complete(other(func(c *verifier.Case) { c.IdempotencyKey = "idk_v1_other" }))}, reason: verifier.ReasonInconsistent},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, func(w *world) { w.read.byKey, w.read.byTx = c.key, c.tx })
			res, wr := w.decided(t)
			want(t, res, verifier.Unknown, c.reason)
			if wr.RecoveryRequestID == "" {
				t.Fatal("UNKNOWN without a recovery request")
			}
			if wr.Verdict.PredicateResult != nil && *wr.Verdict.PredicateResult {
				t.Fatal("UNKNOWN recorded a true predicate")
			}
		})
	}
}

// INV-12: the transaction read is scoped to the principal the grant was issued to, taken from the grant and never from the request.
func TestTheTransactionReadIsScopedToTheGrantsPrincipal(t *testing.T) {
	w := newWorld(t)
	w.decided(t)
	q, ok := w.read.transactionQuery()
	if !ok || q.PrincipalID != princ || q.TransactionRef != subject || q.IdempotencyKey != "" || q.TenantID != tenant {
		t.Fatalf("transaction query = %+v", q)
	}
	w = newWorld(t, func(w *world) { w.store.receipt.PrincipalID = "" })
	if _, err := w.verify(t); err == nil || w.read.reads() != 0 || len(w.store.writes) != 0 {
		t.Fatalf("a receipt with no principal was verified: %v", err)
	}
}

func TestAMalformedOrRefusedReadIsNotRetried(t *testing.T) {
	for _, k := range []verifier.ReadKind{verifier.ReadMalformed, verifier.ReadRefused} {
		w := newWorld(t, func(w *world) { w.read.byKey = []step{{err: &verifier.ReadError{Kind: k}}} })
		w.decided(t)
		if len(w.wait.waits) != 0 || w.read.reads() != 1 {
			t.Fatalf("%s: waits %v, reads %d", k, w.wait.waits, w.read.reads())
		}
	}
}

func TestPagesAreFollowedUpToTheBudget(t *testing.T) {
	two := []step{{page: verifier.CasePage{Complete: false, NextCursor: "p2"}}, complete(theCase())}
	w := newWorld(t, func(w *world) { w.read.byTx = two })
	res, _ := w.decided(t)
	want(t, res, verifier.Achieved, verifier.ReasonPredicateTrue)
	if q := w.read.queries[len(w.read.queries)-1]; q.Cursor != "p2" {
		t.Fatalf("the second page was asked with cursor %q", q.Cursor)
	}
}

// The predicate is evaluated over the transaction's cases: a duplicate, a closed case and a missing binding are never ACHIEVED.
func TestThePredicateDecidesWhenTheKeyIsPresent(t *testing.T) {
	dup := theCase()
	dup.CaseID, dup.IdempotencyKey = "case_2", "idk_v1_other"
	closed := theCase()
	closed.Status = "CLOSED"
	t.Run("two cases for the subject and intent", func(t *testing.T) {
		w := newWorld(t, func(w *world) { w.read.byTx = []step{complete(theCase(), dup)} })
		res, wr := w.decided(t)
		want(t, res, verifier.NotAchieved, verifier.ReasonPredicateFalse)
		if wr.Verdict.PredicateResult == nil || *wr.Verdict.PredicateResult {
			t.Fatal("NOT_ACHIEVED without a false predicate")
		}
	})
	t.Run("a closed case", func(t *testing.T) {
		w := newWorld(t, func(w *world) { w.read.byKey, w.read.byTx = []step{complete(closed)}, []step{complete(closed)} })
		res, _ := w.decided(t)
		want(t, res, verifier.NotAchieved, verifier.ReasonPredicateFalse)
	})
	// P1 (Codex): the predicate is read over every case of the transaction, but only the case under our key is our effect. A
	// key case that fails the postcondition must not become ACHIEVED because another case satisfies it (INV-04, AGENT-21).
	t.Run("the key's own case fails while another case satisfies the predicate", func(t *testing.T) {
		ours := theCase()
		ours.Status, ours.CreatedByIntent = "CLOSED", "int_other"
		other := theCase()
		other.CaseID, other.IdempotencyKey = "case_2", "idk_v1_other"
		w := newWorld(t, func(w *world) { w.read.byKey, w.read.byTx = []step{complete(ours)}, []step{complete(ours, other)} })
		res, _ := w.decided(t)
		if res.Verdict == verifier.Achieved {
			t.Fatal("ACHIEVED although the case under our key does not satisfy the predicate")
		}
		want(t, res, verifier.Unknown, verifier.ReasonInconsistent)
	})
	t.Run("an unbound variable evaluates to UNKNOWN, not false", func(t *testing.T) {
		w := newWorld(t, func(w *world) { delete(w.req.Bindings, "intent_id") })
		res, _ := w.decided(t)
		want(t, res, verifier.Unknown, verifier.ReasonPredicateUnknown)
	})
}

func TestACapabilityThatIsNotQueryableByKeyIsUnknownWithoutReads(t *testing.T) {
	w := newWorld(t, func(w *world) { w.req.QueryableByKey = false })
	res, _ := w.decided(t)
	want(t, res, verifier.Unknown, verifier.ReasonNotQueryableByKey)
	if w.read.reads() != 0 {
		t.Fatalf("reads = %d", w.read.reads())
	}
}

// REQ-VER-004, INV-28: a pending condition that holds gives PENDING_EXTERNAL with a watch that has a deadline; one that
// does not gives the plain verdict; a watch that cannot be kept is refused.
func TestPendingExternalHasAWatchWithADeadline(t *testing.T) {
	pend := func(w *world) {
		closed := theCase()
		closed.Status = "CLOSED"
		w.read.byKey, w.read.byTx = []step{complete(closed)}, []step{complete(closed)}
		now := w.clk.Now()
		w.req.Definition.Pending = &verifier.Pending{Predicate: "exists(case where case.status == 'CLOSED')", Triggers: []string{"fin.evt.settlement.updated"},
			NextCheckAt: now.Add(5 * time.Minute), DeadlineAt: now.Add(72 * time.Hour)}
	}
	w := newWorld(t, pend)
	res, wr := w.decided(t)
	want(t, res, verifier.PendingExternal, verifier.ReasonPendingCondition)
	if wr.Watch == nil || wr.Watch.WatchID == "" || !wr.Watch.DeadlineAt.After(wr.Watch.NextCheckAt) || wr.RecoveryRequestID == "" {
		t.Fatalf("PENDING_EXTERNAL without a usable watch or recovery request: %+v", wr)
	}
	// P2 (Codex): the pending condition is about the case under our key, as the success predicate is. Another case that
	// matches it must not hold a watch open for this receipt.
	t.Run("another case matches the pending condition but the key's case does not", func(t *testing.T) {
		ours := theCase()
		ours.CreatedByIntent = "int_other"
		other := theCase()
		other.CaseID, other.IdempotencyKey, other.Status, other.CreatedByIntent = "case_2", "idk_v1_other", "CLOSED", "int_other"
		w := newWorld(t, pend, func(w *world) { w.read.byKey, w.read.byTx = []step{complete(ours)}, []step{complete(ours, other)} })
		res, wr := w.decided(t)
		want(t, res, verifier.NotAchieved, verifier.ReasonPredicateFalse)
		if wr.Watch != nil {
			t.Fatal("a watch was stored for an effect that is not this receipt's")
		}
	})
	for name, mut := range map[string]func(*verifier.Pending){
		"a deadline already past":      func(p *verifier.Pending) { p.DeadlineAt = t0 },
		"a first check after deadline": func(p *verifier.Pending) { p.NextCheckAt = p.DeadlineAt.Add(time.Second) },
		"a first check in the past":    func(p *verifier.Pending) { p.NextCheckAt = t0 },
		"no deadline":                  func(p *verifier.Pending) { p.DeadlineAt = time.Time{} },
		// outcome-verdict.v1: watch.triggers are unique PackIds. A value the contract refuses must not be signed.
		"a duplicate trigger":         func(p *verifier.Pending) { p.Triggers = []string{"fin.evt.a", "fin.evt.a"} },
		"a trigger that is no PackId": func(p *verifier.Pending) { p.Triggers = []string{"Not A Pack Id"} },
		"an empty trigger":            func(p *verifier.Pending) { p.Triggers = []string{""} },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, pend, func(w *world) { mut(w.req.Definition.Pending) })
			res, wr := w.decided(t)
			want(t, res, verifier.Unknown, verifier.ReasonWatchUnusable)
			if wr.Watch != nil {
				t.Fatal("a watch without a usable deadline was stored")
			}
		})
	}
}

// SoT 13.1 slot: an invalid receipt signature stops the verification before any read.
func TestAnInvalidReceiptSignatureStopsBeforeAnyRead(t *testing.T) {
	w := newWorld(t, func(w *world) { w.auth = authenticity{result: verifier.AuthInvalid} })
	res, wr := w.decided(t)
	want(t, res, verifier.Unknown, verifier.ReasonReceiptUnverifiable)
	if !wr.SignatureInvalid || w.read.reads() != 0 {
		t.Fatalf("signature invalid %v, reads %d", wr.SignatureInvalid, w.read.reads())
	}
	w = newWorld(t)
	if _, wr := w.decided(t); wr.Verdict.ReceiptSignature != verifier.AuthNotChecked {
		t.Fatalf("an unchecked receipt is recorded as %q", wr.Verdict.ReceiptSignature)
	}
}

// P2 (Codex): a signature check that cannot run is not an invalid signature. It stays fail closed (UNKNOWN, no read), but no
// SECURITY_EVENT SIGNATURE_INVALID is raised and the receipt is recorded as not checked.
func TestACheckThatCannotRunIsNotAnInvalidSignature(t *testing.T) {
	w := newWorld(t, func(w *world) { w.auth = authenticity{err: errors.New("verification key unavailable")} })
	res, wr := w.decided(t)
	want(t, res, verifier.Unknown, verifier.ReasonReceiptUnverifiable)
	if wr.SignatureInvalid {
		t.Fatal("SIGNATURE_INVALID was raised for a check that never ran")
	}
	if wr.Verdict.ReceiptSignature != verifier.AuthNotChecked || w.read.reads() != 0 {
		t.Fatalf("receipt signature %q, reads %d", wr.Verdict.ReceiptSignature, w.read.reads())
	}
}

// P2 (Codex): only the statuses the verifier knows are acted on. Anything else is fail closed, as an error is.
func TestAnUnrecognizedAuthenticityResultIsNotAPass(t *testing.T) {
	w := newWorld(t, func(w *world) { w.auth = authenticity{result: verifier.Authenticity("MAYBE")} })
	res, wr := w.decided(t)
	want(t, res, verifier.Unknown, verifier.ReasonReceiptUnverifiable)
	if wr.SignatureInvalid || w.read.reads() != 0 {
		t.Fatalf("signature invalid %v, reads %d", wr.SignatureInvalid, w.read.reads())
	}
}

// outcome-verdict.v1: the verdict is signed by the Signer over its canonical document, and what is stored matches.
func TestTheVerdictIsSignedOverItsDocument(t *testing.T) {
	w := newWorld(t)
	_, wr := w.decided(t)
	if len(w.sign.signed) != 1 || wr.Verdict.Sig.KeyID != "k_outcome_test" || wr.Verdict.Sig.Value == "" {
		t.Fatalf("signature: %+v after %d signings", wr.Verdict.Sig, len(w.sign.signed))
	}
	doc := wr.Verdict.Document
	for _, f := range []string{"schema_version", "tenant_id", "correlation_id", "verdict_id", "receipt_id", "outcome_definition", "evidence_reads", "predicate_result", "verdict", "verified_at", "sig"} {
		if _, ok := doc[f]; !ok {
			t.Fatalf("the verdict document lacks %q", f)
		}
	}
	if doc["verdict"] != "ACHIEVED" || doc["receipt_id"] != rcpt || doc["tenant_id"] != tenant || doc["correlation_id"] != corr {
		t.Fatalf("document = %v", doc)
	}
}

// Fail closed: a signer or store that fails leaves no verdict, and no result is returned.
func TestAFailedSignOrStoreReturnsAnErrorAndNoResult(t *testing.T) {
	w := newWorld(t, func(w *world) { w.sign.err = errors.New("no key") })
	if res, err := w.verify(t); err == nil || res.Verdict != "" || len(w.store.writes) != 0 {
		t.Fatalf("signer failure: %v %+v", err, res)
	}
	w = newWorld(t, func(w *world) { w.store.putErr = errors.New("append failed") })
	if res, err := w.verify(t); err == nil || res.Verdict != "" {
		t.Fatalf("store failure: %v %+v", err, res)
	}
	w = newWorld(t, func(w *world) { w.store.loadErr = errors.New("no receipt") })
	if _, err := w.verify(t); err == nil || w.read.reads() != 0 {
		t.Fatalf("receipt failure: %v, reads %d", err, w.read.reads())
	}
}

func TestADefinitionThatDoesNotCompileIsRefusedBeforeAnyRead(t *testing.T) {
	w := newWorld(t, func(w *world) { w.req.Definition.SuccessPredicate = "exists(case where case.nope == 1)" })
	if _, err := w.verify(t); !errors.Is(err, verifier.ErrDefinition) || w.read.reads() != 0 || len(w.store.writes) != 0 {
		t.Fatalf("err = %v, reads %d", err, w.read.reads())
	}
}

func TestNewRefusesAMissingPort(t *testing.T) {
	if _, err := verifier.New(verifier.Config{}); err == nil {
		t.Fatal("an empty config was accepted")
	}
	w := newWorld(t)
	_ = w
	for name, cut := range map[string]func(*verifier.Config){
		"clock": func(c *verifier.Config) { c.Clock = nil }, "waiter": func(c *verifier.Config) { c.Waiter = nil }, "ids": func(c *verifier.Config) { c.IDs = nil },
		"cases": func(c *verifier.Config) { c.Cases = nil }, "store": func(c *verifier.Config) { c.Store = nil }, "signer": func(c *verifier.Config) { c.Signer = nil },
		"authenticity": func(c *verifier.Config) { c.Authenticity = nil }, "budget pages": func(c *verifier.Config) { c.Budget.MaxPages = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.New(cfgWithout(t, cut)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestTheContextNeedsADeadline(t *testing.T) {
	w := newWorld(t)
	if _, err := w.v.Verify(t.Context(), w.req); err == nil || w.read.reads() != 0 {
		t.Fatalf("a context without a deadline was accepted: %v", err)
	}
}
