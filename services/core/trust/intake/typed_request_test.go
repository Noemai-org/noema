package intake

// TRU-12 (REQ-TRU-011; G1, A449): the Mode 2-compatible Trust interface stays clean: Trust takes a typed request, not a conversation. Two guards. (1) The Request type is a closed set of
// typed members; a new member (above all a text, a transcript, a message or a conversation) is a change to this list and so to the interface review note (status/TRU-12-interface-review.md),
// never a side edit. (2) No non-test source under services/core/trust imports a package of the conversation side: the orchestrator, the edge, the input gateway, the understanding layer,
// the evidence layer, execution, outcome or the explainer. All values are SYNTHETIC.

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestTheRequestIsATypedRequestAndNotAConversation(t *testing.T) {
	want := map[string]string{
		"Session": "intake.Session", "CorrelationID": "string", "IntentRef": "string", "EvidenceSnapshotID": "string", "EvidenceUnavailable": "bool", "TurnID": "string",
	}
	rt := reflect.TypeOf(Request{})
	got := map[string]string{}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		got[f.Name] = f.Type.String()
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the members of Request are %v, want exactly %v: a new member changes the Trust interface and needs the interface review note of TRU-12", got, want)
	}
	st := reflect.TypeOf(Session{})
	var names []string
	for i := 0; i < st.NumField(); i++ {
		names = append(names, st.Field(i).Name+" "+st.Field(i).Type.String())
	}
	if strings.Join(names, ",") != "TenantID string,PrincipalID string,Role string" {
		t.Errorf("the members of Session are %v, want TenantID, PrincipalID and Role only", names)
	}
}

// forbiddenImports are the packages of the conversation side and of the other powers: Trust reaches them through ports only (INV-01, SoT 51).
var forbiddenImports = []string{
	"/services/core/orchestrator", "/services/core/edge", "/services/core/inputgateway", "/services/core/understanding", "/services/core/evidence",
	"/services/core/execution", "/services/core/outcome", "/services/core/explanation",
}

func TestNoTrustSourceImportsTheConversationSideOrAnotherPower(t *testing.T) {
	root := filepath.Join("..") // services/core/trust
	var bad []string
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			for _, bannedPath := range forbiddenImports {
				if strings.Contains(p, bannedPath) {
					bad = append(bad, path+" imports "+p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Fatalf("the guard read only %d Trust sources: the walk is not reaching them", checked)
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("Trust imports the conversation side or another power: %v", bad)
	}
}
