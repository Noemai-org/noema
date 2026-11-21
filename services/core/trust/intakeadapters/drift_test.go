package intakeadapters

// A413 (REQ-TRU-005, REQ-TRU-007; INV-09, INV-21): the publication row keeps the hash of the policy document it was published with (migration 0035); the loader of the pack recomputes the
// hash of the document it holds for each version in force and fails closed, as ErrDrift, when the stored hash is missing (a row from before the hash existed) or is not that hash.
// All data is SYNTHETIC; the publication store is real PostgreSQL read as the tenant's own Trust login.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

func strp(s string) *string { return &s }

const otherHash = "sha256:00000000000000000000000000000000000000000000000000000000000000bb"

func evaluateErr(t *testing.T, w *world, at int64) (intake.PolicyEvaluation, error) {
	t.Helper()
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1 + 60}, &facts{master: resolvedMaster()})
	return p.Evaluate(ctx(t), query(lookup, at))
}

func TestAStoredHashThatIsTheHashOfThePacksDocumentIsEvaluated(t *testing.T) {
	w := newWorld(t)
	_, hash := packPolicyHash(t, realPack(t))
	w.publishHashed(t, tenantA, "3.1", t1, &hash)
	got, err := evaluateErr(t, w, late)
	if err != nil || !got.PermitMatched {
		t.Fatalf("the published document is the pack's: %+v, %v", got, err)
	}
}

func TestAStoredHashThatIsNotThePacksIsErrDriftAndNothingIsEvaluated(t *testing.T) {
	w := newWorld(t)
	w.publishHashed(t, tenantA, "3.1", t1, strp(otherHash))
	got, err := evaluateErr(t, w, late)
	if !errors.Is(err, ErrDrift) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
		t.Fatalf("a different document under the same version: %+v, %v; want ErrDrift and no evaluation", got, err)
	}
}

func TestANullStoredHashIsErrDriftAndNothingIsEvaluated(t *testing.T) {
	w := newWorld(t)
	w.publishHashed(t, tenantA, "3.1", t1, nil)
	got, err := evaluateErr(t, w, late)
	if !errors.Is(err, ErrDrift) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
		t.Fatalf("a row with no hash: %+v, %v; want ErrDrift and no evaluation", got, err)
	}
}

// The version in force at admission is read too (an AT_ADMISSION rule comes from it, D8): its hash must be the pack's as well as the newest version's.
func TestADriftInTheVersionInForceAtAdmissionIsErrDriftEvenWhenTheNewestIsRight(t *testing.T) {
	w := newWorld(t)
	older := variantPack(t, "3.0", nil)
	_, oldHash := packPolicyHash(t, older)
	_, newHash := packPolicyHash(t, realPack(t))
	w.publishHashed(t, tenantA, "3.0", t1, strp(otherHash))
	w.publishHashed(t, tenantA, "3.1", t2, &newHash)
	p := w.policies(t, book(t, realPack(t), older), &adm{at: t1 + 60}, &facts{master: resolvedMaster()})
	if got, err := p.Evaluate(ctx(t), query(lookup, late)); !errors.Is(err, ErrDrift) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
		t.Fatalf("3.0 was in force at admission with another document: %+v, %v; want ErrDrift", got, err)
	}
	// the same history with the right hash for 3.0 evaluates (positive control)
	w2 := newWorld(t)
	w2.publishHashed(t, tenantA, "3.0", t1, &oldHash)
	w2.publishHashed(t, tenantA, "3.1", t2, &newHash)
	p2 := w2.policies(t, book(t, realPack(t), older), &adm{at: t1 + 60}, &facts{master: resolvedMaster()})
	if _, err := p2.Evaluate(ctx(t), query(lookup, late)); err != nil {
		t.Fatalf("both documents are the pack's: %v", err)
	}
}

// A version nobody needs (published before the one in force at admission, with no document in the pack) is not read, as before: its hash is not checked.
func TestAnOlderVersionNobodyNeedsIsNotCheckedForDrift(t *testing.T) {
	w := newWorld(t)
	_, hash := packPolicyHash(t, realPack(t))
	w.publishHashed(t, tenantA, "2.9", t1, strp(otherHash)) // no document for 2.9 in the pack, and 3.1 was in force when the intent was admitted
	w.publishHashed(t, tenantA, "3.1", t2, &hash)
	p := w.policies(t, book(t, realPack(t)), &adm{at: t2 + 60}, &facts{master: resolvedMaster()})
	if got, err := p.Evaluate(ctx(t), query(lookup, late)); err != nil || !got.PermitMatched {
		t.Fatalf("a version nobody needs was checked for drift: %+v, %v", got, err)
	}
}
