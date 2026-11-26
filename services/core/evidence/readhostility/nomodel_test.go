package readhostility_test

import (
	"os/exec"
	"strings"
	"testing"
)

// INV-01, INV-02, AGENT-05: no model is in the read path. The packages the tests drive, with everything they import, hold no model, explanation, provider or
// voice package of this repository and no provider SDK. (tools/lint holds the same line for trust, execution, outcome and record; the Evidence Layer is not in
// that rule's list, so the read path states it for itself.)
func TestNoModelIsInTheReadPath(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "../portread", "../layer", "../../adapters/synthetic_tushe").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	deps := strings.Fields(string(out))
	if len(deps) < 20 {
		t.Fatalf("only %d packages listed: the scan is not seeing the dependencies", len(deps))
	}
	banned := map[string]bool{"explanation": true, "modelclient": true, "providers": true, "voice": true, "understanding": true}
	vendors := []string{"openai", "anthropic", "generativeai", "genai"}
	for _, p := range deps {
		for _, seg := range strings.Split(p, "/") {
			if banned[seg] {
				t.Errorf("the read path reaches %q", p)
			}
		}
		for _, v := range vendors {
			if strings.Contains(strings.ToLower(p), v) {
				t.Errorf("the read path reaches %q", p)
			}
		}
	}
}
