package limits_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/breaker"
	"github.com/Noemai-org/noema/services/core/execution/limits"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// The values are the Architect's ruling A3, kept as pack data (AGENT-16): REQ-CAP-003, REQ-RCV-003, INV-07.
const packFile = "../../../../packs/fintech/limits/execution_limits.json"

func doc(breaker, budget string) string {
	return `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","circuit_breaker":` + breaker + `,"dispatch_budget":` + budget + `}`
}

const goodBreaker = `{"min_samples":7,"cooldown_s":11,"half_open_probes":1}`
const goodBudget = `{"max_dispatches":3,"key":["tenant_id","correlation_id","capability","target_ref"]}`

func TestThePackFileCarriesTheRulingA3Values(t *testing.T) {
	f, err := os.Open(packFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := limits.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if l.Breaker.MinSamples != 20 || l.Breaker.Cooldown != 30*time.Second {
		t.Fatalf("breaker policy = %+v, want MinSamples 20 and Cooldown 30s", l.Breaker)
	}
	if l.DispatchBudget != 2 {
		t.Fatalf("dispatch budget = %d, want 2", l.DispatchBudget)
	}
}

// The numbers come from the file, not from the code: a different file gives different values.
func TestTheValuesComeFromTheDocumentAndNotFromTheCode(t *testing.T) {
	l, err := limits.Load(strings.NewReader(doc(goodBreaker, goodBudget)))
	if err != nil {
		t.Fatal(err)
	}
	if l.Breaker.MinSamples != 7 || l.Breaker.Cooldown != 11*time.Second || l.DispatchBudget != 3 {
		t.Fatalf("got %+v budget %d", l.Breaker, l.DispatchBudget)
	}
}

func TestADocumentThatIsNotExactlyTheContractIsRefused(t *testing.T) {
	cases := map[string]string{
		"empty":                  ``,
		"not json":               `{`,
		"unknown top field":      `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","circuit_breaker":` + goodBreaker + `,"dispatch_budget":` + goodBudget + `,"extra":1}`,
		"missing breaker":        `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","dispatch_budget":` + goodBudget + `}`,
		"missing budget":         `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","circuit_breaker":` + goodBreaker + `}`,
		"zero min_samples":       doc(`{"min_samples":0,"cooldown_s":11,"half_open_probes":1}`, goodBudget),
		"zero cooldown":          doc(`{"min_samples":7,"cooldown_s":0,"half_open_probes":1}`, goodBudget),
		"negative cooldown":      doc(`{"min_samples":7,"cooldown_s":-1,"half_open_probes":1}`, goodBudget),
		"two probes":             doc(`{"min_samples":7,"cooldown_s":11,"half_open_probes":2}`, goodBudget),
		"missing min_samples":    doc(`{"cooldown_s":11,"half_open_probes":1}`, goodBudget),
		"fractional cooldown":    doc(`{"min_samples":7,"cooldown_s":1.5,"half_open_probes":1}`, goodBudget),
		"zero budget":            doc(goodBreaker, `{"max_dispatches":0,"key":["tenant_id","correlation_id","capability","target_ref"]}`),
		"key reordered":          doc(goodBreaker, `{"max_dispatches":2,"key":["correlation_id","tenant_id","capability","target_ref"]}`),
		"key short":              doc(goodBreaker, `{"max_dispatches":2,"key":["tenant_id","correlation_id","capability"]}`),
		"key repeated":           doc(goodBreaker, `{"max_dispatches":2,"key":["tenant_id","tenant_id","capability","target_ref"]}`),
		"key unknown field":      doc(goodBreaker, `{"max_dispatches":2,"key":["tenant_id","correlation_id","capability","account"]}`),
		"overflowing cooldown":   doc(`{"min_samples":7,"cooldown_s":9007199254740991,"half_open_probes":1}`, goodBudget),
		"trailing close brace":   doc(goodBreaker, goodBudget) + `}`,
		"trailing close bracket": doc(goodBreaker, goodBudget) + `]`,
		"trailing text":          doc(goodBreaker, goodBudget) + ` x`,
		"a second document":      doc(goodBreaker, goodBudget) + doc(goodBreaker, goodBudget),
		"pack id with a symbol":  `{"schema_version":"1.0","pack_id":"!","label":"SYNTHETIC","circuit_breaker":` + goodBreaker + `,"dispatch_budget":` + goodBudget + `}`,
		"pack id in capitals":    `{"schema_version":"1.0","pack_id":"FIN","label":"SYNTHETIC","circuit_breaker":` + goodBreaker + `,"dispatch_budget":` + goodBudget + `}`,
		"pack id empty":          `{"schema_version":"1.0","pack_id":"","label":"SYNTHETIC","circuit_breaker":` + goodBreaker + `,"dispatch_budget":` + goodBudget + `}`,
		"pack id with a dot":     `{"schema_version":"1.0","pack_id":"fin.x","label":"SYNTHETIC","circuit_breaker":` + goodBreaker + `,"dispatch_budget":` + goodBudget + `}`,
		"wrong version":          `{"schema_version":"2.0","pack_id":"fin","label":"SYNTHETIC","circuit_breaker":` + goodBreaker + `,"dispatch_budget":` + goodBudget + `}`,
	}
	for name, in := range cases {
		if l, err := limits.Load(strings.NewReader(in)); err == nil {
			t.Errorf("%s: accepted: %+v", name, l)
		}
	}
}

// The loaded policy is accepted by the breaker as it is (CAP-01, REQ-CAP-003).
func TestTheLoadedPolicyBuildsABreaker(t *testing.T) {
	l, err := limits.Load(strings.NewReader(doc(goodBreaker, goodBudget)))
	if err != nil {
		t.Fatal(err)
	}
	params, err := breaker.ParamsOf(contracts.CapabilityDescriptorV1Limits{
		RatePerPrincipalPerHour: 5, TimeoutMs: 3000,
		CircuitBreaker: contracts.CapabilityDescriptorV1LimitsCircuitBreaker{ErrorRate: json.Number("0.5"), WindowS: 60},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := breaker.New(clock.NewManual(time.Unix(0, 0)), params, l.Breaker, nil); err != nil {
		t.Fatal(err)
	}
}
