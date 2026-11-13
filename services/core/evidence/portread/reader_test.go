package portread_test

// ENV-04 wiring slice W1 (REQ-CTX-001, REQ-CTX-002, REQ-ENV-001, REQ-AUD-001; INV-12, INV-19, INV-23, AGENT-25): the Evidence Layer's call site for the read
// ports. These tests use fake ports and a fake recorder and need no database; the status of each observation is the one layer.AssignStatus gives it. All data is
// SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/portread"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

var (
	epoch = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC) // SYNTHETIC
	scope = adapters.Scope{TenantID: "ten_aa", PrincipalID: "usr_ab12"}
	hash  = "sha256:" + strings.Repeat("ab", 32)
	prov  = adapters.Provenance{RawHash: hash, DataSource: "stand-in", Synthetic: true}
)

type ports struct {
	ledger  func(context.Context, adapters.Scope, string) (adapters.LedgerRead, error)
	sw      func(context.Context, adapters.Scope, string) (adapters.SwitchRead, error)
	dest    func(context.Context, adapters.Scope, string) (adapters.DestinationRead, error)
	calls   []string
	scopes  []adapters.Scope
	budgets []time.Time
}

func (p *ports) note(ctx context.Context, name string, s adapters.Scope) {
	p.calls = append(p.calls, name)
	p.scopes = append(p.scopes, s)
	if d, ok := ctx.Deadline(); ok {
		p.budgets = append(p.budgets, d)
	}
}
func (p *ports) LedgerEntries(ctx context.Context, s adapters.Scope, ref string) (adapters.LedgerRead, error) {
	p.note(ctx, "ledger", s)
	return p.ledger(ctx, s, ref)
}
func (p *ports) SwitchStatus(ctx context.Context, s adapters.Scope, ref string) (adapters.SwitchRead, error) {
	p.note(ctx, "switch", s)
	return p.sw(ctx, s, ref)
}
func (p *ports) DestinationStatus(ctx context.Context, s adapters.Scope, ref string) (adapters.DestinationRead, error) {
	p.note(ctx, "destination", s)
	return p.dest(ctx, s, ref)
}

type recorder struct {
	got  []layer.Observation
	fail int // fail the nth call (1-based); 0 never
}

func (r *recorder) Record(_ context.Context, o layer.Observation) (layer.Item, error) {
	r.got = append(r.got, o)
	if r.fail == len(r.got) {
		return layer.Item{}, errors.New("synthetic: the Record refused")
	}
	return layer.Item{EvidenceID: "evr_" + o.Source[len(o.Source)-4:]}, nil
}

func entry(id string, status adapters.PostingStatus) adapters.LedgerEntry {
	return adapters.LedgerEntry{EntryID: id, AccountRef: "acc_1", TransactionRef: "TX1", EntryType: adapters.EntryDebit, AmountMinor: 5000, Currency: "NGN",
		PostingStatus: status, PostedAt: "2026-11-11T08:00:00Z", Version: 1}
}

func healthy() *ports {
	return &ports{
		ledger: func(context.Context, adapters.Scope, string) (adapters.LedgerRead, error) {
			return adapters.LedgerRead{Entries: []adapters.LedgerEntry{entry("e1", adapters.PostingPosted)}, Provenance: prov}, nil
		},
		sw: func(context.Context, adapters.Scope, string) (adapters.SwitchRead, error) {
			return adapters.SwitchRead{Record: adapters.SwitchRecord{SwitchRef: "sw_1", TransactionRef: "TX1", RawCode: "00", RawCodeLabel: adapters.LabelSynthetic,
				SemanticStatus: adapters.SwitchSuccess, IsTerminal: true, DestinationAck: true, ReceivedAt: "2026-11-11T08:00:05Z", RawPayloadHash: hash}, Provenance: prov}, nil
		},
		dest: func(context.Context, adapters.Scope, string) (adapters.DestinationRead, error) {
			return adapters.DestinationRead{Status: adapters.DestinationStatus{TransactionRef: "TX1", CreditState: adapters.CreditCredited, SettlementState: adapters.SettlementSettled,
				EffectiveAt: "2026-11-11T08:00:10Z", ObservedAt: "2026-11-11T08:59:00Z"}, Provenance: prov}, nil
		},
	}
}

func sources() portread.Sources {
	mk := func(id, fact string) portread.Source {
		return portread.Source{ID: id, Fact: fact, Authority: layer.AuthAuthoritative, Limit: 300 * time.Second, Sensitivity: "S3_financial_pii", ResourceKind: "fin.transaction"}
	}
	return portread.Sources{Ledger: mk("fin.src.core_ledger", "debit_posted_or_reversed"), Switch: mk("fin.src.switch", "switch_processing_result"),
		Destination: mk("fin.src.destination", "beneficiary_credited")}
}

func reader(p *ports, rec *recorder) *portread.Reader {
	return &portread.Reader{Ledger: p, Switch: p, Destination: p, Sources: sources(), Rec: rec, Clock: clock.NewManual(epoch), ReadBy: "TRUST", ReadTimeout: 5 * time.Second,
		DataSource: "stand-in", Synthetic: true}
}

func run(t *testing.T, r *portread.Reader) ([]layer.Item, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return r.ReadTransaction(ctx, scope, "corr_synthetic", "TX1")
}

func status(o layer.Observation) contracts.EnumsV1FactStatus {
	return layer.AssignStatus(o.Read, o.Src, epoch)
}

func TestEveryAnswerIsRecordedWithTheSessionsScopeAndThePacksDescriptor(t *testing.T) {
	p, rec := healthy(), &recorder{}
	items, err := run(t, reader(p, rec))
	if err != nil || len(items) != 3 || len(rec.got) != 3 {
		t.Fatalf("items %d, observations %d, err %v; want 3, 3, nil", len(items), len(rec.got), err)
	}
	wantClass := []string{"AUTHORITATIVE_FOR:debit_posted_or_reversed", "AUTHORITATIVE_FOR:switch_processing_result", "AUTHORITATIVE_FOR:beneficiary_credited"}
	for i, o := range rec.got {
		if o.TenantID != "ten_aa" || o.PrincipalScope != "usr_ab12" || o.CorrelationID != "corr_synthetic" || o.SourceRecordID != "TX1" || o.ReadBy != "TRUST" ||
			o.ResourceScope != "fin.transaction:TX1" || o.Sensitivity != "S3_financial_pii" || o.AuthorityClass != wantClass[i] || o.Src.SourceLimit != 300*time.Second {
			t.Errorf("observation %d: %+v", i, o)
		}
		if o.RawHash != hash || o.DataSource != "stand-in" || !o.Synthetic || o.NoBytes || o.Payload == nil {
			t.Errorf("observation %d provenance: raw %q source %q synthetic %v nobytes %v payload %v", i, o.RawHash, o.DataSource, o.Synthetic, o.NoBytes, o.Payload != nil)
		}
		if got := status(o); got != contracts.EnumsV1FactStatusKNOWN {
			t.Errorf("observation %d (%s): status %s, want KNOWN", i, o.Source, got)
		}
	}
	for i, s := range p.scopes {
		if s != scope {
			t.Errorf("port %s was called with scope %+v, want the session's %+v unchanged (INV-12)", p.calls[i], s, scope)
		}
	}
}

func TestNothingIsKnownFromAnAnswerThatCanStillChange(t *testing.T) {
	cases := map[string]func(*ports){
		"a pending ledger posting": func(p *ports) {
			p.ledger = func(context.Context, adapters.Scope, string) (adapters.LedgerRead, error) {
				return adapters.LedgerRead{Entries: []adapters.LedgerEntry{entry("e1", adapters.PostingPending)}, Provenance: prov}, nil
			}
		},
		"an empty ledger": func(p *ports) {
			p.ledger = func(context.Context, adapters.Scope, string) (adapters.LedgerRead, error) {
				return adapters.LedgerRead{Provenance: prov}, nil
			}
		},
		"a switch success that is not terminal": func(p *ports) {
			inner := p.sw
			p.sw = func(c context.Context, s adapters.Scope, r string) (adapters.SwitchRead, error) {
				got, err := inner(c, s, r)
				got.Record.IsTerminal = false
				return got, err
			}
		},
		"a switch status that is unknown": func(p *ports) {
			inner := p.sw
			p.sw = func(c context.Context, s adapters.Scope, r string) (adapters.SwitchRead, error) {
				got, err := inner(c, s, r)
				got.Record.SemanticStatus = adapters.SwitchUnknown
				return got, err
			}
		},
		"a credit that is pending": func(p *ports) {
			inner := p.dest
			p.dest = func(c context.Context, s adapters.Scope, r string) (adapters.DestinationRead, error) {
				got, err := inner(c, s, r)
				got.Status.CreditState = adapters.CreditPending
				return got, err
			}
		},
		"a reversal that is pending (A31)": func(p *ports) {
			inner := p.dest
			p.dest = func(c context.Context, s adapters.Scope, r string) (adapters.DestinationRead, error) {
				got, err := inner(c, s, r)
				got.Status.SettlementState = adapters.SettlementReversalPending
				return got, err
			}
		},
	}
	idx := map[string]int{"a pending ledger posting": 0, "an empty ledger": 0, "a switch success that is not terminal": 1, "a switch status that is unknown": 1,
		"a credit that is pending": 2, "a reversal that is pending (A31)": 2}
	for name, mutate := range cases {
		p, rec := healthy(), &recorder{}
		mutate(p)
		if _, err := run(t, reader(p, rec)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := status(rec.got[idx[name]]); got != contracts.EnumsV1FactStatusUNKNOWN {
			t.Errorf("%s: status %s, want UNKNOWN", name, got)
		}
	}
}

func TestACorroboratingSourceNeverConfersKnown(t *testing.T) {
	p, rec := healthy(), &recorder{}
	r := reader(p, rec)
	r.Sources.Switch.Authority = layer.AuthCorroborating
	r.Sources.Switch.Fact = "beneficiary_credited"
	if _, err := run(t, r); err != nil {
		t.Fatal(err)
	}
	if rec.got[1].AuthorityClass != "CORROBORATING_FOR:beneficiary_credited" || status(rec.got[1]) != contracts.EnumsV1FactStatusUNKNOWN {
		t.Errorf("corroborating switch: class %q status %s", rec.got[1].AuthorityClass, status(rec.got[1]))
	}
}

func TestAPortThatFailsIsAnItemNotAnErrorAndTheOtherPortsAreStillRead(t *testing.T) {
	type failure struct {
		err      error
		want     contracts.EnumsV1FactStatus
		noBytes  bool
		wantHash string
	}
	for name, f := range map[string]failure{
		"a timeout":               {&adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "TIMEOUT"}, contracts.EnumsV1FactStatusUNAVAILABLE, true, ""},
		"a 503 with a body":       {&adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503:UNAVAILABLE", RawHash: hash}, contracts.EnumsV1FactStatusUNAVAILABLE, false, hash},
		"a refused credential":    {&adapters.ReadError{Failure: adapters.ReadUnauthorized, RawCode: "403:FORBIDDEN", RawHash: hash}, contracts.EnumsV1FactStatusUNAUTHORIZED, false, hash},
		"a record not found":      {&adapters.ReadError{Failure: adapters.ReadNotFound, RawCode: "404:NOT_FOUND", RawHash: hash}, contracts.EnumsV1FactStatusUNKNOWN, false, hash},
		"a malformed answer":      {&adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: "FIN:schema", RawHash: hash}, contracts.EnumsV1FactStatusUNKNOWN, false, hash},
		"an error nobody foresaw": {errors.New("synthetic: surprise"), contracts.EnumsV1FactStatusUNKNOWN, true, ""},
	} {
		p, rec := healthy(), &recorder{}
		p.sw = func(context.Context, adapters.Scope, string) (adapters.SwitchRead, error) {
			return adapters.SwitchRead{}, f.err
		}
		items, err := run(t, reader(p, rec))
		if err != nil || len(items) != 3 {
			t.Fatalf("%s: items %d err %v; want 3 items and no error", name, len(items), err)
		}
		o := rec.got[1]
		if got := status(o); got != f.want || o.NoBytes != f.noBytes || o.RawHash != f.wantHash || o.Payload != nil {
			t.Errorf("%s: status %s nobytes %v raw %q payload %v; want %s %v %q nil", name, got, o.NoBytes, o.RawHash, o.Payload != nil, f.want, f.noBytes, f.wantHash)
		}
		if o.DataSource != "stand-in" || !o.Synthetic {
			t.Errorf("%s: a failed read must still carry the reader's synthetic label (AGENT-25), got %q %v", name, o.DataSource, o.Synthetic)
		}
		if status(rec.got[0]) != contracts.EnumsV1FactStatusKNOWN || status(rec.got[2]) != contracts.EnumsV1FactStatusKNOWN {
			t.Errorf("%s: the ports that answered were not recorded as KNOWN", name)
		}
	}
}

func TestAnAnswerThatSaysUnavailableIsUnavailableAndNeverAnEmptyAnswer(t *testing.T) {
	p, rec := healthy(), &recorder{}
	p.ledger = func(context.Context, adapters.Scope, string) (adapters.LedgerRead, error) {
		return adapters.LedgerRead{Unavailable: true, Provenance: prov}, nil
	}
	p.dest = func(context.Context, adapters.Scope, string) (adapters.DestinationRead, error) {
		return adapters.DestinationRead{Unavailable: true}, nil // no usable provenance: no hash and an explicit no-bytes
	}
	if _, err := run(t, reader(p, rec)); err != nil {
		t.Fatal(err)
	}
	if o := rec.got[0]; status(o) != contracts.EnumsV1FactStatusUNAVAILABLE || o.RawHash != hash || o.NoBytes || o.Payload != nil {
		t.Errorf("ledger: %s raw %q nobytes %v", status(o), o.RawHash, o.NoBytes)
	}
	if o := rec.got[2]; status(o) != contracts.EnumsV1FactStatusUNAVAILABLE || o.RawHash != "" || !o.NoBytes {
		t.Errorf("destination: %s raw %q nobytes %v", status(o), o.RawHash, o.NoBytes)
	}
}

func TestAnAnswerTheReaderCannotUseIsUnknownAndKeepsNoPayload(t *testing.T) {
	cases := map[string]func(*ports){
		"an unlabelled provenance": func(p *ports) {
			inner := p.ledger
			p.ledger = func(c context.Context, s adapters.Scope, r string) (adapters.LedgerRead, error) {
				got, err := inner(c, s, r)
				got.Provenance = adapters.Provenance{RawHash: hash}
				return got, err
			}
		},
		"a switch record for another transaction": func(p *ports) {
			inner := p.sw
			p.sw = func(c context.Context, s adapters.Scope, r string) (adapters.SwitchRead, error) {
				got, err := inner(c, s, r)
				got.Record.TransactionRef = "TX2"
				return got, err
			}
		},
		"a destination time that does not parse": func(p *ports) {
			inner := p.dest
			p.dest = func(c context.Context, s adapters.Scope, r string) (adapters.DestinationRead, error) {
				got, err := inner(c, s, r)
				got.Status.EffectiveAt = "yesterday"
				return got, err
			}
		},
	}
	idx := map[string]int{"an unlabelled provenance": 0, "a switch record for another transaction": 1, "a destination time that does not parse": 2}
	for name, mutate := range cases {
		p, rec := healthy(), &recorder{}
		mutate(p)
		if _, err := run(t, reader(p, rec)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if o := rec.got[idx[name]]; status(o) != contracts.EnumsV1FactStatusUNKNOWN || o.Payload != nil || o.Read.Class != layer.ReadMalformed {
			t.Errorf("%s: status %s class %s payload %v; want UNKNOWN MALFORMED nil", name, status(o), o.Read.Class, o.Payload != nil)
		}
	}
}

func TestARequestOrConfigurationTheReaderRefusesReadsNothing(t *testing.T) {
	ctxNoDeadline := context.Background()
	bad := map[string]func(*portread.Reader){
		"no read timeout":                 func(r *portread.Reader) { r.ReadTimeout = 0 },
		"a read_by outside the list":      func(r *portread.Reader) { r.ReadBy = "EVERYONE" },
		"no recorder":                     func(r *portread.Reader) { r.Rec = nil },
		"a source with no freshness":      func(r *portread.Reader) { r.Sources.Switch.Limit = 0 },
		"an authoritative source no fact": func(r *portread.Reader) { r.Sources.Ledger.Fact = "" },
		"a resource kind with a path":     func(r *portread.Reader) { r.Sources.Ledger.ResourceKind = "fin/transaction" },
		"a sensitivity off the shape":     func(r *portread.Reader) { r.Sources.Destination.Sensitivity = "secret" },
	}
	for name, mutate := range bad {
		p, rec := healthy(), &recorder{}
		r := reader(p, rec)
		mutate(r)
		if _, err := run(t, r); err == nil || len(p.calls) != 0 {
			t.Errorf("%s: err %v, ports called %v; want an error and no read", name, err, p.calls)
		}
	}
	reqs := map[string]struct {
		ctx      context.Context
		scope    adapters.Scope
		corr     string
		ref      string
		wantCall bool
	}{
		"no deadline":           {ctxNoDeadline, scope, "corr_s", "TX1", false},
		"an empty tenant":       {nil, adapters.Scope{PrincipalID: "usr_ab12"}, "corr_s", "TX1", false},
		"an empty principal":    {nil, adapters.Scope{TenantID: "ten_aa"}, "corr_s", "TX1", false},
		"a principal off shape": {nil, adapters.Scope{TenantID: "ten_aa", PrincipalID: "alice"}, "corr_s", "TX1", false},
		"a reference with a /":  {nil, scope, "corr_s", "TX/1", false},
		"an empty reference":    {nil, scope, "corr_s", "", false},
		"a huge reference":      {nil, scope, "corr_s", strings.Repeat("A", 65), false},
		"no correlation id":     {nil, scope, "", "TX1", false},
	}
	for name, q := range reqs {
		p, rec := healthy(), &recorder{}
		ctx := q.ctx
		if ctx == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
		}
		if _, err := reader(p, rec).ReadTransaction(ctx, q.scope, q.corr, q.ref); err == nil || len(p.calls) != 0 || len(rec.got) != 0 {
			t.Errorf("%s: err %v, ports called %v, recorded %d; want an error and no read", name, err, p.calls, len(rec.got))
		}
	}
}

func TestEachPortCallGetsItsOwnDeadlineFromConfiguration(t *testing.T) {
	p, rec := healthy(), &recorder{}
	r := reader(p, rec)
	r.ReadTimeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	parent, _ := ctx.Deadline()
	if _, err := r.ReadTransaction(ctx, scope, "corr_synthetic", "TX1"); err != nil {
		t.Fatal(err)
	}
	if len(p.budgets) != 3 {
		t.Fatalf("%d calls with a deadline, want 3", len(p.budgets))
	}
	for i, d := range p.budgets {
		// the call's deadline is the configured two seconds from now, so it is minutes before the caller's own (no wall clock is read here)
		if parent.Sub(d) < 9*time.Minute {
			t.Errorf("call %d: deadline %v is not within the configured 2s of the call (parent deadline %v)", i, d, parent)
		}
	}
}

func TestWhenTheRecordRefusesNothingFurtherIsRead(t *testing.T) {
	p, rec := healthy(), &recorder{fail: 1}
	items, err := run(t, reader(p, rec))
	if err == nil || len(items) != 0 {
		t.Fatalf("err %v items %d; want the Record's error and no item", err, len(items))
	}
	if len(p.calls) != 1 || p.calls[0] != "ledger" {
		t.Errorf("ports called %v after a refused Record; want only the ledger (EC-E16)", p.calls)
	}
	p, rec = healthy(), &recorder{fail: 2}
	items, err = run(t, reader(p, rec))
	if err == nil || len(items) != 1 || len(p.calls) != 2 {
		t.Errorf("second record refused: err %v items %d calls %v; want the error, the first item, and no destination read", err, len(items), p.calls)
	}
}
