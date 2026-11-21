package inputgateway_test

// A2 (Architect, interim, relayed, not yet committed): public routes emit errors.v3. The Input Gateway's error envelope moves from errors.v2 to errors.v3 (already merged; no
// contract bump). The wire bytes do not change (the two envelopes have the same members), so what proves the move is the source: the handler names no errors.v2 type and names the
// v3 ones, and every code it uses is a code of errors.v3. REQ-API-001, AGENT-15. A source read cannot be satisfied by a wrapper that still builds v2.

import (
	"os"
	"strings"
	"testing"
)

func TestTheGatewayEmitsErrorsV3AndNamesNoErrorsV2Type(t *testing.T) {
	src, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if strings.Contains(text, "ErrorsV2") {
		t.Error("handler.go still names an errors.v2 type: a public route emits errors.v3")
	}
	for _, code := range []string{"INPUTINVALID", "UNAUTHENTICATED", "REQUESTIDREUSED", "SESSIONINVALID", "DEPENDENCYUNAVAILABLE", "INTERNALSAFETYBLOCK"} {
		if !strings.Contains(text, "ErrorsV3ErrorCode"+code) {
			t.Errorf("handler.go does not name ErrorsV3ErrorCode%s", code)
		}
	}
}
