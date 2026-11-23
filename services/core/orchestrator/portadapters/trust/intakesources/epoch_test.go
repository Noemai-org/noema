package intakesources_test

// Proves A345 S1 (the logical resolution epoch is read from the stored admitted intent, 1 at creation; migration 0033), INV-19 (an epoch that cannot be read is an error, never a default),
// REQ-SEC-004 and INV-11 (the tenant's own login, and the query names the tenant), AGENT-13.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
)

func TestPolicyFactsGivesTheLogicalResolutionEpochOfTheStoredIntent(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_ep1", 1, node, admittedAt, resolvedSlots)
	f := facts(t, w)
	got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ep1", 1), vars("logical_resolution_epoch"))
	if err != nil || len(got.Variables) != 1 || got.Variables["logical_resolution_epoch"] != predicate.Int(1) {
		t.Errorf("got %+v, %v; want the epoch 1 of an intent at creation (A345 S1)", got, err)
	}
	got, err = f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ep1", 1), vars("logical_resolution_epoch", "subject_resource.status"))
	if err != nil || got.Variables["logical_resolution_epoch"] != predicate.Int(1) || got.Variables["subject_resource.status"] != predicate.Enum("RESOLVED") {
		t.Errorf("the epoch beside a slot's status: %+v, %v", got, err)
	}
}

func TestPolicyFactsTheEpochOfAnotherTenantsIntentIsNotFound(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantB, "int_ep2", 1, node, admittedAt, resolvedSlots)
	for name, logins := range map[string]intakesources.Logins{"tenant A's own login": w, "a login that sees every row": superuser{w}} {
		got, err := facts(t, logins).PolicyFacts(ctx(t), factQuery(tenantA, "int_ep2", 1), vars("logical_resolution_epoch"))
		if !errors.Is(err, intakesources.ErrNotFound) || len(got.Variables) != 0 {
			t.Errorf("%s: got %+v, %v; want ErrNotFound", name, got, err)
		}
	}
}

// epochRow answers the one read of the stored intent with a document and an epoch, as the database would, so that an epoch the table's check forbids can be played.
type epochRow struct {
	doc   string
	epoch int64
}

func (r epochRow) Scan(dest ...any) error {
	if len(dest) != 2 {
		return errors.New("the read must take the document and the epoch")
	}
	*(dest[0].(*string)) = r.doc
	*(dest[1].(*int64)) = r.epoch
	return nil
}

type epochLogin struct{ row epochRow }

func (e epochLogin) QueryRow(context.Context, string, ...any) pgx.Row { return e.row }

func TestPolicyFactsAnEpochBelowOneIsNotAnEpoch(t *testing.T) {
	doc := `{"intent_node":"` + node + `","slots":` + resolvedSlots + `}`
	for _, epoch := range []int64{0, -1} {
		f := facts(t, loginFunc(func(string) (intakesources.Querier, error) { return epochLogin{epochRow{doc, epoch}}, nil }))
		got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ep3", 1), vars("logical_resolution_epoch"))
		if !errors.Is(err, intakesources.ErrBadAnswer) || len(got.Variables) != 0 || strings.Contains(err.Error(), "-1") {
			t.Errorf("epoch %d: got %+v, %v; want ErrBadAnswer and no value", epoch, got, err)
		}
	}
	f := facts(t, loginFunc(func(string) (intakesources.Querier, error) { return epochLogin{epochRow{doc, 1}}, nil }))
	if got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ep3", 1), vars("logical_resolution_epoch")); err != nil || got.Variables["logical_resolution_epoch"] != predicate.Int(1) {
		t.Errorf("epoch 1 through the same fake: %+v, %v", got, err)
	}
}

func TestPolicyFactsTheEpochIsNotReadForAReadThatNeedsOnlyASlot(t *testing.T) {
	doc := `{"intent_node":"` + node + `","slots":` + resolvedSlots + `}`
	f := facts(t, loginFunc(func(string) (intakesources.Querier, error) { return epochLogin{epochRow{doc, 0}}, nil }))
	if got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ep3", 1), vars("subject_resource.status")); err != nil || got.Variables["subject_resource.status"] != predicate.Enum("RESOLVED") {
		t.Errorf("an unusable epoch must not spoil a fact that does not use it: %+v, %v", got, err)
	}
}

func TestPolicyFactsTheEpochIsTheStoredOneAndNotAConstant(t *testing.T) {
	doc := `{"intent_node":"` + node + `","slots":` + resolvedSlots + `}`
	f := facts(t, loginFunc(func(string) (intakesources.Querier, error) { return epochLogin{epochRow{doc, 7}}, nil }))
	if got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ep3", 1), vars("logical_resolution_epoch")); err != nil || got.Variables["logical_resolution_epoch"] != predicate.Int(7) {
		t.Errorf("got %+v, %v; want the stored epoch (the table's check pins it to 1 today, the reader does not)", got, err)
	}
}
