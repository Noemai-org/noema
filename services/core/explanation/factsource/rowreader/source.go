package rowreader

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/explanation/factsource"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

var (
	// ErrBadRequest is returned for a call with no tenant or no principal, or a decision id that is not a reference (INV-12). The reader is never asked.
	ErrBadRequest = errors.New("rowreader: the request has no tenant, no principal or no decision")
	// ErrRead is returned when a seam or the reader failed in a way that is not one of the typed refusals. The cause is reachable with errors.Is and never printed.
	ErrRead = errors.New("rowreader: a source could not answer")
	// ErrLocale is returned when the locale seam fails or names a locale outside the five.
	ErrLocale = errors.New("rowreader: the locale could not be determined")
	// ErrNoVerdict is returned for a decision none of whose own grants has a verdict: there is no outcome to explain.
	ErrNoVerdict = errors.New("rowreader: the decision has no verdict")
)

// Config is the pack's and the deployment's data; nothing here has a default (AGENT-16). Facts is the currency table, the zone and the owner-queue names (factsource.Config). Label is the
// source label of the case facts. CaseCapability is the pack's capability whose receipt acknowledges a case. QueueParam names the step_params member that holds the owner queue
// of the case, as the pack's binding gives it; empty means no source for the queue. NoWriteOutcome is the pack's outcome that explains a decision with no write (A727); empty means a
// decision with no verdict is not explained.
type Config struct {
	Facts          factsource.Config
	Label          string
	CaseCapability string
	QueueParam     string
	NoWriteOutcome string
}

// RowsReader reads the rows of one decision for one tenant; *Reader is the real one.
type RowsReader interface {
	Read(ctx context.Context, tenantID, decisionID string) (Rows, error)
}

// Payments gives the payment facts of the resolved subject, scoped to the caller's tenant. It answers for the kinds amount, account and time; the composition fills it.
type Payments interface {
	PaymentFacts(ctx context.Context, tenantID string, ref contracts.CommonV1ResourceRef) ([]contracts.ExplanationFactsV2FactsItem, []contracts.ExplanationFactsV2UnknownsItem, error)
}

// Locales gives the locale of the conversation the explanation answers; the composition fills it. No locale is defaulted.
type Locales interface {
	Locale(ctx context.Context, s explanation.Subject, correlationID string) (contracts.ExplanationFactsV2Locale, error)
}

// Source implements explanation.FactsSource over a RowsReader.
type Source struct {
	cfg      Config
	reader   RowsReader
	payments Payments
	locales  Locales
	clk      clock.Clock
}

var _ explanation.FactsSource = (*Source)(nil)

// NewSource refuses a missing seam or clock, an empty label or capability, and a facts configuration with no zone or no currency table.
func NewSource(cfg Config, reader RowsReader, payments Payments, locales Locales, clk clock.Clock) (*Source, error) {
	if cfg.NoWriteOutcome != "" && !outcomeRe.MatchString(cfg.NoWriteOutcome) {
		return nil, ErrConfig
	}
	if reader == nil || payments == nil || locales == nil || clk == nil || cfg.Label == "" || cfg.CaseCapability == "" || cfg.Facts.Zone == nil || len(cfg.Facts.Exponents) == 0 {
		return nil, ErrConfig
	}
	return &Source{cfg: cfg, reader: reader, payments: payments, locales: locales, clk: clk}, nil
}

var (
	outcomeRe        = regexp.MustCompile(`\A[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+\z`)
	classificationRe = regexp.MustCompile(`\A[A-Z][A-Z0-9_]*\z`)
)

// typed are the refusals of the reader that pass through unchanged.
var typed = []error{ErrNotFound, ErrNoLogin, ErrNoDeadline, ErrStore, ErrNoClassification, ErrMismatch, ErrAmbiguous, ErrConfig}

func passThrough(err error) error {
	for _, t := range typed {
		if errors.Is(err, t) {
			return err
		}
	}
	return wrapped(ErrRead, err)
}

// Facts reads the rows of the decision and builds its facts. Any refusal is an error and no fact: the Explainer then records nothing.
func (s *Source) Facts(ctx context.Context, sub explanation.Subject, decisionID string) (contracts.ExplanationFactsV2, error) {
	if s == nil {
		return contracts.ExplanationFactsV2{}, ErrConfig
	}
	if sub.TenantID == "" || sub.PrincipalID == "" || !decisionRe.MatchString(decisionID) {
		return contracts.ExplanationFactsV2{}, ErrBadRequest
	}
	rows, err := s.reader.Read(ctx, sub.TenantID, decisionID)
	if err != nil {
		return contracts.ExplanationFactsV2{}, passThrough(err)
	}
	if rows.DecisionID != decisionID || rows.CorrelationID == "" {
		return contracts.ExplanationFactsV2{}, ErrMismatch
	}
	if !classificationRe.MatchString(rows.Classification) {
		return contracts.ExplanationFactsV2{}, ErrNoClassification
	}
	outcome, err := s.outcomeOf(rows)
	if err != nil {
		return contracts.ExplanationFactsV2{}, err
	}
	row, err := s.caseRow(rows)
	if err != nil {
		return contracts.ExplanationFactsV2{}, err
	}
	asOf := s.clk.Now()
	facts, unknowns, err := s.payment(ctx, sub.TenantID, rows.SubjectRef)
	if err != nil {
		return contracts.ExplanationFactsV2{}, err
	}
	loc, err := s.locales.Locale(ctx, sub, rows.CorrelationID)
	if err != nil || !validLocale(loc) {
		return contracts.ExplanationFactsV2{}, wrapped(ErrLocale, err)
	}
	cf, cu := factsource.Case(s.cfg.Facts, row, asOf, s.cfg.Label)
	return contracts.ExplanationFactsV2{
		SchemaVersion: "2.0", CorrelationId: contracts.CommonV1CorrelationId(rows.CorrelationID), Locale: loc,
		Facts: append(facts, cf...), Unknowns: append(unknowns, cu...),
		Actions: []contracts.ExplanationFactsV2ActionsItem{}, Advice: []string{}, NextSteps: []contracts.ExplanationFactsV2NextStepsItem{},
		OutcomeId: contracts.CommonV1PackId(outcome), Classification: rows.Classification,
	}, nil
}

func validLocale(l contracts.ExplanationFactsV2Locale) bool {
	switch l {
	case contracts.ExplanationFactsV2LocaleEnNG, contracts.ExplanationFactsV2LocalePcm, contracts.ExplanationFactsV2LocaleYo, contracts.ExplanationFactsV2LocaleHa, contracts.ExplanationFactsV2LocaleIg:
		return true
	}
	return false
}

// outcomeOf is the outcome to explain, chosen the same way every time (A727): the verdict row of the decision's own grants wins when there is one; with none, the Decision Record alone
// explains only a no-write outcome; a write with no verdict is ErrNoVerdict.
func (s *Source) outcomeOf(rows Rows) (string, error) {
	outcome, err := verdictOutcome(rows)
	if !errors.Is(err, ErrNoVerdict) {
		return outcome, err
	}
	return s.noWriteOutcome(rows)
}

// noWriteOutcome is the configured no-write outcome for an ALLOW record that has no grant of its own. Any other record outcome has no outcome to explain here (ErrNoVerdict), a record
// whose outcome is not one of the seven is ErrMismatch, and a decision with a grant of its own is a write, never explained from the record alone.
func (s *Source) noWriteOutcome(rows Rows) (string, error) {
	if s.cfg.NoWriteOutcome == "" {
		return "", ErrNoVerdict
	}
	switch contracts.EnumsV1DecisionOutcome(rows.DecisionOutcome) {
	case contracts.EnumsV1DecisionOutcomeALLOW:
	case contracts.EnumsV1DecisionOutcomeCLARIFY, contracts.EnumsV1DecisionOutcomeCONFIRM, contracts.EnumsV1DecisionOutcomeWAIT, contracts.EnumsV1DecisionOutcomeESCALATE,
		contracts.EnumsV1DecisionOutcomeREJECT, contracts.EnumsV1DecisionOutcomeUNKNOWN:
		return "", ErrNoVerdict
	default:
		return "", ErrMismatch
	}
	for _, e := range rows.Executions {
		if e.OwnGrant {
			return "", ErrNoVerdict
		}
	}
	return s.cfg.NoWriteOutcome, nil
}

// verdictOutcome is the one outcome the verdicts of the decision's own grants name. None is ErrNoVerdict and several are ErrAmbiguous: this source never chooses.
func verdictOutcome(rows Rows) (string, error) {
	outcome := ""
	for _, e := range rows.Executions {
		if !e.OwnGrant || e.Verdict == "" {
			continue
		}
		if !outcomeRe.MatchString(e.OutcomeID) {
			return "", ErrMismatch
		}
		if outcome != "" && outcome != e.OutcomeID {
			return "", ErrAmbiguous
		}
		outcome = e.OutcomeID
	}
	if outcome == "" {
		return "", ErrNoVerdict
	}
	return outcome, nil
}

// caseRow is what the case receipt of the investigation says. No receipt of the case capability is a row that is not known (a status answer has no case); two are ErrAmbiguous. The row is
// KNOWN only for an ACKNOWLEDGED receipt whose current verdict is ACHIEVED (INV-04, INV-07).
func (s *Source) caseRow(rows Rows) (factsource.CaseRow, error) {
	var found *Execution
	for i := range rows.Executions {
		if rows.Executions[i].CapabilityID != s.cfg.CaseCapability {
			continue
		}
		if found != nil {
			return factsource.CaseRow{}, ErrAmbiguous
		}
		found = &rows.Executions[i]
	}
	row := factsource.CaseRow{Status: contracts.EnumsV1FactStatusUNAVAILABLE}
	if found == nil || found.ReceiptID == "" || found.State != "ACKNOWLEDGED" || found.Verdict != string(contracts.EnumsV1VerdictACHIEVED) {
		return row, nil
	}
	row.Status = contracts.EnumsV1FactStatusKNOWN
	if found.ExternalAck != nil {
		row.ExternalAck = *found.ExternalAck
	}
	row.OwnerQueue = s.queue(found)
	return row, nil
}

// queue is the owner queue the receipt's signed step_params name under the configured member: a non-empty string, else none.
func (s *Source) queue(e *Execution) string {
	raw, ok := e.StepParams[s.cfg.QueueParam]
	if !ok || s.cfg.QueueParam == "" {
		return ""
	}
	var q string
	if json.Unmarshal(raw, &q) != nil {
		return ""
	}
	return q
}

// paymentKinds are the kinds this source answers for when the subject is not resolved: the amount, the account and the time (A711 took the recipient's name out of the candidate-sourced kinds).
var paymentKinds = []string{"payment.amount", "account", "payment.time"}

// payment asks the seam for the resolved subject. With none, the kinds are unknowns and the seam is not asked.
func (s *Source) payment(ctx context.Context, tenantID, subject string) ([]contracts.ExplanationFactsV2FactsItem, []contracts.ExplanationFactsV2UnknownsItem, error) {
	facts, unknowns := []contracts.ExplanationFactsV2FactsItem{}, []contracts.ExplanationFactsV2UnknownsItem{}
	if subject == "" {
		for _, k := range paymentKinds {
			unknowns = append(unknowns, contracts.ExplanationFactsV2UnknownsItem{FactId: "unk_" + strings.ReplaceAll(k, ".", "_"), Kind: k, Status: contracts.EnumsV1FactStatusUNAVAILABLE, WhyCode: "SUBJECT_NOT_RESOLVED"})
		}
		return facts, unknowns, nil
	}
	f, u, err := s.payments.PaymentFacts(ctx, tenantID, contracts.CommonV1ResourceRef(subject))
	if err != nil {
		return nil, nil, wrapped(ErrRead, err)
	}
	return append(facts, f...), append(unknowns, u...), nil
}
