package ceilings_test

// A154 as corrected, REQ-AMB-004, REQ-SEC-003: the digit ceiling is the one number of masking-limits.v1, so a clarification label can never show more than a masked
// surface may. If the pack's masking limit and this ceiling ever differ, this test fails. The values are SYNTHETIC.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/ceilings"
	"github.com/Noemai-org/noema/services/core/platform/masking"
)

func TestTheDigitCeilingIsTheMaskingLimitOfThePack(t *testing.T) {
	f, err := os.Open("../../../../packs/fintech/limits/masking_limits.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := masking.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if l.MaxUnmaskedRun() != ceilings.MaxLabelDigitRun {
		t.Fatalf("masking-limits allows %d unmasked digits, the clarification ceiling is %d", l.MaxUnmaskedRun(), ceilings.MaxLabelDigitRun)
	}
}

// A561 (interim; PERD v1.23 section 67, "confirmation TTL 10 min"): the ceiling in code is the one number of trust-limits.v2's `maximum` for the confirmation TTL, ten minutes in
// milliseconds, so the schema and the loader can never differ.
func TestTheConfirmationCeilingIsTheSchemaMaximumAndTenMinutes(t *testing.T) {
	b, err := os.ReadFile("../../../../contracts/schemas/trust-limits.v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties map[string]struct {
			Maximum *int64 `json:"maximum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	m := s.Properties["confirmation_ttl_ceiling_ms"].Maximum
	if m == nil || *m != ceilings.MaxConfirmationTTLMillis || ceilings.MaxConfirmationTTLMillis != 10*60*1000 {
		t.Fatalf("schema maximum %v, ceiling %d; want both 600000", m, ceilings.MaxConfirmationTTLMillis)
	}
}

// A709 (interim, relayed): the receipt step_params bounds are the schema's, so the Controller can never sign a receipt the contract refuses. The values are SYNTHETIC.
func TestTheReceiptStepParamBoundsAreTheSchemasBounds(t *testing.T) {
	b, err := os.ReadFile("../../../../contracts/schemas/execution-receipt.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties struct {
			StepParams struct {
				MaxProperties        int `json:"maxProperties"`
				AdditionalProperties struct {
					MaxLength int `json:"maxLength"`
				} `json:"additionalProperties"`
			} `json:"step_params"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	sp := doc.Properties.StepParams
	if sp.MaxProperties != ceilings.MaxReceiptStepParams || sp.AdditionalProperties.MaxLength != ceilings.MaxReceiptStepParamLength {
		t.Fatalf("schema %d and %d, ceilings %d and %d", sp.MaxProperties, sp.AdditionalProperties.MaxLength, ceilings.MaxReceiptStepParams, ceilings.MaxReceiptStepParamLength)
	}
}
