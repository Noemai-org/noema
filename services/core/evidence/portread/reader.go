package portread

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const prefix = "portread: "

var (
	recordRefRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	resourceKindRe = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$`)
	principalRe    = regexp.MustCompile(`^usr_[0-9A-Za-z]{2,32}$`)
	packIDRe       = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
	sensitivityRe  = regexp.MustCompile(`^S[0-9]_[a-z][a-z0-9_]*$`)
	readByAllowed  = map[string]bool{"TRUST": true, "OUTCOME": true, "RESOLUTION": true, "EXECUTION_REVALIDATION": true}
)

// Source is what the pack's evidence-source descriptor says about one source, handed in by the caller. Nothing here comes from adapter code (SoT 14).
type Source struct {
	ID           string          // the PackId of the evidence-source
	Fact         string          // the fact the source is authoritative or corroborating for; empty only for Authority NONE
	Authority    layer.Authority // from the descriptor
	Limit        time.Duration   // freshness_sla_seconds
	Sensitivity  string          // from the descriptor, for example S3_financial_pii
	ResourceKind string          // the pack's resource kind for the thing read, <vertical>.<kind>
}

// Sources are the three sources one transaction is read from.
type Sources struct{ Ledger, Switch, Destination Source }

// Recorder is the part of layer.Builder the reader uses.
type Recorder interface {
	Record(ctx context.Context, o layer.Observation) (layer.Item, error)
}

// Reader reads the ports and records evidence. Every field is required.
type Reader struct {
	Ledger      adapters.Ledger
	Switch      adapters.Switch
	Destination adapters.Destination
	Sources     Sources
	Rec         Recorder
	Clock       clock.Clock
	ReadBy      string        // TRUST, OUTCOME, RESOLUTION or EXECUTION_REVALIDATION
	ReadTimeout time.Duration // the deadline of each port call; from configuration, zero refuses every read (AGENT-16)
	// DataSource and Synthetic label a read that failed and so has no provenance (for example "stand-in", true). A successful read carries the adapter's
	// own provenance label instead.
	DataSource string
	Synthetic  bool
}

func (s Source) check() error {
	switch {
	case !packIDRe.MatchString(s.ID):
		return fmt.Errorf("%ssource id %q is not a pack id", prefix, s.ID)
	case !resourceKindRe.MatchString(s.ResourceKind):
		return fmt.Errorf("%ssource %s has resource kind %q, want <vertical>.<kind>", prefix, s.ID, s.ResourceKind)
	case !sensitivityRe.MatchString(s.Sensitivity):
		return fmt.Errorf("%ssource %s has sensitivity %q", prefix, s.ID, s.Sensitivity)
	case s.Limit <= 0:
		return fmt.Errorf("%ssource %s has no positive freshness window", prefix, s.ID)
	}
	switch s.Authority {
	case layer.AuthAuthoritative, layer.AuthCorroborating:
		if s.Fact == "" {
			return fmt.Errorf("%ssource %s is authoritative or corroborating for no fact", prefix, s.ID)
		}
	case layer.AuthNone:
	default:
		return fmt.Errorf("%ssource %s has unknown authority %q", prefix, s.ID, s.Authority)
	}
	return nil
}

func (r *Reader) configured() error {
	switch {
	case r.Ledger == nil || r.Switch == nil || r.Destination == nil || r.Rec == nil || r.Clock == nil:
		return errors.New(prefix + "the reader is not fully configured")
	case r.ReadTimeout <= 0:
		return errors.New(prefix + "the reader needs a positive read timeout from configuration")
	case !readByAllowed[r.ReadBy]:
		return fmt.Errorf("%sread_by %q is not allowed", prefix, r.ReadBy)
	}
	for _, s := range []Source{r.Sources.Ledger, r.Sources.Switch, r.Sources.Destination} {
		if err := s.check(); err != nil {
			return err
		}
	}
	return nil
}

// ReadTransaction reads the ledger, the switch and the destination answers for one transaction and records one evidence item for each. The scope is the
// authenticated session's tenant and principal and is passed to the ports unchanged. A port that cannot answer is not an error of this call: it becomes
// an item whose status says so (UNAVAILABLE, UNKNOWN or UNAUTHORIZED), and the other ports are still read. An error is returned only for a request or
// configuration the reader refuses before reading, and for a failure to record (the Record comes first: nothing further is read after it, EC-E16); the
// items recorded before that failure are returned with the error.
func (r *Reader) ReadTransaction(ctx context.Context, scope adapters.Scope, correlationID, transactionRef string) ([]layer.Item, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New(prefix + "the context needs a deadline")
	}
	if err := r.configured(); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !principalRe.MatchString(scope.PrincipalID) {
		return nil, fmt.Errorf("%sthe session's principal does not match the contract", prefix)
	}
	if !recordRefRe.MatchString(transactionRef) {
		return nil, fmt.Errorf("%sthe transaction reference does not match the contract", prefix)
	}
	if correlationID == "" {
		return nil, errors.New(prefix + "a correlation id is required")
	}
	base := func(s Source) layer.Observation {
		return layer.Observation{TenantID: scope.TenantID, CorrelationID: correlationID, Source: s.ID, SourceRecordID: transactionRef, ReadBy: r.ReadBy,
			AuthorityClass: authorityClass(s), ResourceScope: s.ResourceKind + ":" + transactionRef, PrincipalScope: scope.PrincipalID, Sensitivity: s.Sensitivity,
			Src: layer.Source{Authority: s.Authority, SourceLimit: s.Limit}, DataSource: r.DataSource, Synthetic: r.Synthetic}
	}
	steps := []func() layer.Observation{
		func() layer.Observation { return r.ledger(ctx, scope, transactionRef, base(r.Sources.Ledger)) },
		func() layer.Observation { return r.switchRead(ctx, scope, transactionRef, base(r.Sources.Switch)) },
		func() layer.Observation {
			return r.destination(ctx, scope, transactionRef, base(r.Sources.Destination))
		},
	}
	var items []layer.Item
	for _, step := range steps {
		item, err := r.Rec.Record(ctx, step())
		if err != nil {
			return items, fmt.Errorf("%srecord: %w", prefix, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func authorityClass(s Source) string {
	switch s.Authority {
	case layer.AuthAuthoritative:
		return "AUTHORITATIVE_FOR:" + s.Fact
	case layer.AuthCorroborating:
		return "CORROBORATING_FOR:" + s.Fact
	}
	return "NONE"
}

func (r *Reader) call(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.ReadTimeout)
}

// failed fills in an observation for a read that returned an error: the class, and either the hash of the bytes the adapter received or an explicit no-bytes.
func (r *Reader) failed(o layer.Observation, err error) layer.Observation {
	o.Read = layer.Read{Class: layer.ClassifyError(err), ReadAt: r.Clock.Now()}
	o.RawHash, o.NoBytes = layer.BodyOf(err)
	return o
}

// unusable fills in an observation for an answer that carries no usable payload: the source said it could not answer (a server-side failure), or the
// reader cannot use what arrived (MALFORMED). The adapter's hash is kept when its provenance is well formed; otherwise the read says no bytes were received.
func (r *Reader) unusable(o layer.Observation, p adapters.Provenance, class layer.ReadClass) layer.Observation {
	o.Read = layer.Read{Class: class, ReadAt: r.Clock.Now()}
	if p.Validate() == nil {
		o.RawHash, o.DataSource, o.Synthetic = p.RawHash, p.DataSource, p.Synthetic
	} else {
		o.NoBytes = true
	}
	return o
}

// answered fills in an observation for an answer that arrived with a payload: the provenance is the adapter's, and a provenance that cannot back an item
// makes the read MALFORMED with no payload.
func (r *Reader) answered(o layer.Observation, p adapters.Provenance, effective time.Time, determinate bool, payload map[string]any) layer.Observation {
	now := r.Clock.Now()
	if err := p.Validate(); err != nil {
		o.Read = layer.Read{Class: layer.ReadMalformed, ReadAt: now}
		o.NoBytes = true
		return o
	}
	o.RawHash, o.DataSource, o.Synthetic = p.RawHash, p.DataSource, p.Synthetic
	o.EffectiveAt = effective
	o.Payload = payload
	o.Read = layer.Read{Class: layer.ReadOK, Determinate: determinate, ReadAt: now}
	return o
}

func (r *Reader) ledger(ctx context.Context, scope adapters.Scope, ref string, o layer.Observation) layer.Observation {
	cctx, cancel := r.call(ctx)
	defer cancel()
	got, err := r.Ledger.LedgerEntries(cctx, scope, ref)
	switch {
	case err != nil:
		return r.failed(o, err)
	case got.IsUnavailable():
		return r.unavailable(o, got.Provenance)
	}
	entries := append([]adapters.LedgerEntry(nil), got.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].EntryID < entries[j].EntryID })
	rows := make([]any, 0, len(entries))
	determinate := len(entries) > 0 // an empty ledger is an absence, never a fact (INV-19)
	var latest time.Time
	for _, e := range entries {
		at, perr := time.Parse(time.RFC3339, e.PostedAt)
		if perr != nil || e.Validate() != nil {
			return r.malformed(o, got.Provenance)
		}
		if at.After(latest) {
			latest = at
		}
		if e.PostingStatus == adapters.PostingPending {
			determinate = false // a pending posting can still change
		}
		rows = append(rows, map[string]any{"entry_id": e.EntryID, "account_ref": e.AccountRef, "entry_type": string(e.EntryType), "amount_minor": e.AmountMinor,
			"currency": e.Currency, "posting_status": string(e.PostingStatus), "posted_at": e.PostedAt, "version": e.Version})
	}
	if latest.IsZero() {
		latest = r.Clock.Now()
	}
	return r.answered(o, got.Provenance, latest, determinate, map[string]any{"transaction_ref": ref, "entries": rows})
}

func (r *Reader) switchRead(ctx context.Context, scope adapters.Scope, ref string, o layer.Observation) layer.Observation {
	cctx, cancel := r.call(ctx)
	defer cancel()
	got, err := r.Switch.SwitchStatus(cctx, scope, ref)
	switch {
	case err != nil:
		return r.failed(o, err)
	case got.Unavailable:
		return r.unavailable(o, got.Provenance)
	}
	at, perr := time.Parse(time.RFC3339, got.Record.ReceivedAt)
	if perr != nil || got.Record.Validate() != nil || got.Record.TransactionRef != ref {
		return r.malformed(o, got.Provenance)
	}
	rec := got.Record
	determinate := rec.IsTerminal && rec.SemanticStatus != adapters.SwitchUnknown // a success that is not terminal is not a credited transfer
	return r.answered(o, got.Provenance, at, determinate, map[string]any{"transaction_ref": ref, "switch_ref": rec.SwitchRef, "raw_code": rec.RawCode,
		"raw_code_label": rec.RawCodeLabel, "semantic_status": string(rec.SemanticStatus), "is_terminal": rec.IsTerminal, "destination_ack": rec.DestinationAck,
		"received_at": rec.ReceivedAt, "raw_payload_hash": rec.RawPayloadHash})
}

func (r *Reader) destination(ctx context.Context, scope adapters.Scope, ref string, o layer.Observation) layer.Observation {
	cctx, cancel := r.call(ctx)
	defer cancel()
	got, err := r.Destination.DestinationStatus(cctx, scope, ref)
	switch {
	case err != nil:
		return r.failed(o, err)
	case got.Unavailable:
		return r.unavailable(o, got.Provenance)
	}
	at, perr := time.Parse(time.RFC3339, got.Status.EffectiveAt)
	if perr != nil || got.Status.Validate() != nil || got.Status.TransactionRef != ref {
		return r.malformed(o, got.Provenance)
	}
	s := got.Status
	creditFinal := s.CreditState != adapters.CreditUnknown && s.CreditState != adapters.CreditPending
	return r.answered(o, got.Provenance, at, creditFinal && s.SettlementState.IsTerminal(), map[string]any{"transaction_ref": ref, "credit_state": string(s.CreditState),
		"settlement_state": string(s.SettlementState), "effective_at": s.EffectiveAt, "observed_at": s.ObservedAt})
}

func (r *Reader) unavailable(o layer.Observation, p adapters.Provenance) layer.Observation {
	return r.unusable(o, p, layer.ReadServerError)
}

// malformed is an answer the port returned but this reader cannot use (a field that does not parse, a reference for another transaction): UNKNOWN.
func (r *Reader) malformed(o layer.Observation, p adapters.Provenance) layer.Observation {
	return r.unusable(o, p, layer.ReadMalformed)
}
