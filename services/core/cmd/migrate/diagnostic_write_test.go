package main

// PLT-09 (REQ-TST-010; INV-14): handling the diagnostic write's error explicitly changes no behaviour. A stderr (and a result stream) that fails every write gives the same exit code as one that works,
// on the path that needs no database. All data is SYNTHETIC.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write refused") }

func TestAFailingDiagnosticWriterChangesNoExitCode(t *testing.T) {
	cases := map[string][]string{
		"a usage refusal":       nil,
		"a refused secret file": {"--host", "h", "--port", "1", "--database", "d", "--user", "u", "--password-file", "/nonexistent/secret", "--sslmode", "disable", "--migrations-dir", "/nonexistent/migrations"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var out, errw bytes.Buffer
			want := run(args, &out, &errw, clock.NewManual(epoch))
			if want == 0 {
				t.Fatal("the path under test must not succeed")
			}
			if got := run(args, failingWriter{}, failingWriter{}, clock.NewManual(epoch)); got != want {
				t.Fatalf("a failing writer changed the exit code: got %d, want %d", got, want)
			}
		})
	}
}
