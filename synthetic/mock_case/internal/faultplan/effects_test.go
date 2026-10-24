package faultplan

import (
	"strings"
	"testing"
)

// The five effects added to mock-case: what a plan may and may not say about them (REQ-ENV-004: a plan that
// cannot be applied exactly is refused, never skipped).
func TestNewEffectsAcceptedAndRefused(t *testing.T) {
	ok := []string{
		fault("create_case", "{}", "NEAR_TIMEOUT_BEFORE", `{"delay_ms":2900}`),
		fault("route_case", "{}", "NEAR_TIMEOUT_AFTER", `{"delay_ms":3100}`),
		fault("create_case", "{}", "HTTP_200_SEMANTIC_PENDING", `{}`),
		fault("append_evidence", "{}", "IDEMPOTENCY_COLLISION", `{}`),
		fault("get_case", "{}", "INJECTION_PAYLOAD", `{"field":"reason_code"}`),
		fault("find_evidence", "{}", "INJECTION_PAYLOAD", `{"field":"payload"}`),
	}
	for _, f := range ok {
		if _, err := Parse(plan(f, "")); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	bad := map[string]struct{ fault, want string }{
		"near timeout needs a delay":        {fault("create_case", "{}", "NEAR_TIMEOUT_AFTER", `{}`), "delay_ms"},
		"near timeout takes only a delay":   {fault("create_case", "{}", "NEAR_TIMEOUT_BEFORE", `{"delay_ms":1,"x":1}`), "not a parameter"},
		"pending on a read":                 {fault("get_case", "{}", "HTTP_200_SEMANTIC_PENDING", `{}`), "keyed writes"},
		"pending on a route":                {fault("route_case", "{}", "HTTP_200_SEMANTIC_PENDING", `{}`), "keyed writes"},
		"pending takes no params":           {fault("create_case", "{}", "HTTP_200_SEMANTIC_PENDING", `{"creates_case":false}`), "not a parameter"},
		"collision on a read":               {fault("find_cases", "{}", "IDEMPOTENCY_COLLISION", `{}`), "keyed writes"},
		"injection needs a field":           {fault("get_case", "{}", "INJECTION_PAYLOAD", `{}`), "params.field"},
		"injection in a field the op lacks": {fault("get_case", "{}", "INJECTION_PAYLOAD", `{"field":"payload"}`), "can carry its text only"},
		"injection on a write":              {fault("create_case", "{}", "INJECTION_PAYLOAD", `{"field":"reason_code"}`), "can carry its text only"},
		"injection on a complaint":          {fault("get_complaint", "{}", "INJECTION_PAYLOAD", `{"field":"reason_code"}`), "can carry its text only"},
	}
	for name, c := range bad {
		_, err := Parse(plan(c.fault, ""))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want a refusal naming %q, got %v", name, c.want, err)
		}
	}
}

func TestNewEffectsAreNotLostResponses(t *testing.T) {
	for _, name := range []string{EffectNearTimeoutBefore, EffectNearTimeoutAfter, EffectHTTP200Pending, EffectIdempotencyCollision, EffectInjectionPayload} {
		if (&Effect{Name: name}).AfterCommit() {
			t.Errorf("%s: its answer is not lost or replaced after a commit", name)
		}
	}
}
