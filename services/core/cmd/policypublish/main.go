// Command policypublish publishes the policy version of a pack for one tenant (TRU-02; REQ-TRU-005, REQ-OPS-003; INV-09). See README.md.
//
//	policypublish --tenant ID --pack-dir DIR --pack-file NAME --dsn-file FILE --key-dir DIR --key-max-active N
//
// There is no --published-by: the publisher is the authenticated database role of the DSN's login, filled in by the database (A474, INV-12), and a flag of that name is refused as an unknown flag.
// The policy ID and the version are the pack document's own. The login in the DSN file is the tenant's Trust login (the only login the database lets publish); the key directory holds
// the signing keys of the identity core-trust, which sign the POLICY_PUBLISHED event policystore writes. Nothing secret is an argument: a DSN and a key are read from files.
//
// Output: one JSON log line on stdout (PERD 44.2 fields, never a DSN, a login or a driver's text) and, when the exit code is not 0, one fixed sentence on stderr.
// Exit codes: 0 published, or already published; 1 refused (the login is not the tenant's Trust login, a version that is not higher than the active one, a document or a request
// the loaders refuse); 2 the command could not run (a bad argument, a file or a database that cannot be used, a Record append that failed).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

const (
	exitDone    = 0
	exitRefused = 1
	exitCannot  = 2

	// runTimeout bounds the whole run: every call takes this context, and policystore refuses one without a deadline. An operating limit of this command, not a safety threshold.
	runTimeout = 2 * time.Minute

	maxDSNBytes = 8 << 10

	signerIdentity = "core-trust"
	usage          = "usage: policypublish --tenant ID --pack-dir DIR --pack-file NAME --dsn-file FILE --key-dir DIR --key-max-active N"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, clock.System{})) }

type settings struct {
	tenant, packDir, packFile, dsnFile, keyDir string
	keyMaxActive                               int
}

func parseArgs(args []string) (settings, bool) {
	var s settings
	fs := flag.NewFlagSet("policypublish", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&s.tenant, "tenant", "", "the tenant ID")
	fs.StringVar(&s.packDir, "pack-dir", "", "the pack directory")
	fs.StringVar(&s.packFile, "pack-file", "", "the pack file, inside the pack directory")
	fs.StringVar(&s.dsnFile, "dsn-file", "", "a file holding the DSN of the tenant's Trust login")
	fs.StringVar(&s.keyDir, "key-dir", "", "the directory of the core-trust signing keys")
	fs.IntVar(&s.keyMaxActive, "key-max-active", 0, "how many signing keys may be mounted at once (the rotation overlap; no default)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return s, false
	}
	ok := chain.ValidTenantID(s.tenant) && s.packDir != "" && s.packFile != "" && s.dsnFile != "" && s.keyDir != "" && s.keyMaxActive >= 1
	return s, ok
}

// logLine is the one line printed on stdout: the PERD 44.2 fields and the result, enums and identifiers only.
type logLine struct {
	Ts            string `json:"ts"`
	Level         string `json:"level"`
	Service       string `json:"service"`
	Power         string `json:"power"`
	Event         string `json:"event"`
	CorrelationID string `json:"correlation_id,omitempty"`
	TenantID      string `json:"tenant_id"`
	Msg           string `json:"msg"`
	Result        string `json:"result"`
	Reason        string `json:"reason,omitempty"`
	Policy        string `json:"policy,omitempty"`
	DocumentHash  string `json:"document_hash,omitempty"`
	SQLState      string `json:"sqlstate,omitempty"`
}

// sqlState is the five-character SQLSTATE of a database error, the one part of it that is a fixed code.
func sqlState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && len(pg.Code) == 5 {
		return pg.Code
	}
	var ce connectError
	if errors.As(err, &ce) {
		return ce.state
	}
	return ""
}

// failure maps an error to the exit code, the reason enum and the fixed sentence. The sentence never carries the error's text.
func failure(err error) (code int, reason, sentence string) {
	switch {
	case errors.Is(err, errNotTrustLogin):
		return exitRefused, "login_not_for_tenant", "the login is not the Trust login of this tenant"
	case errors.Is(err, policystore.ErrNotNewer):
		return exitRefused, "not_newer", "the version is not higher than the active one"
	case errors.Is(err, errDocumentChanged):
		return exitRefused, "document_changed", "a different policy document is already published under this version; publish a new version"
	case errors.Is(err, errDocumentUnknown):
		return exitRefused, "document_unknown", "the published version has no document hash because it was published before hashes were kept; publish a new version"
	case errors.Is(err, policystore.ErrRequest):
		return exitRefused, "invalid_request", "the tenant, policy ID, version or document hash is outside its pattern"
	case errors.Is(err, errPackInvalid):
		return exitRefused, "invalid_policy", "the pack's policy document is refused"
	case errors.Is(err, errPackRead):
		return exitCannot, "pack_unreadable", "the pack file cannot be read"
	}
	return exitCannot, "", "the publication could not be completed"
}

func run(args []string, out, errw io.Writer, clk clock.Clock) int {
	s, ok := parseArgs(args)
	if !ok {
		fmt.Fprintln(errw, "policypublish: "+usage)
		return exitCannot
	}
	line := logLine{Service: "policypublish", Power: "trust", Event: "policy_publish", TenantID: s.tenant}
	finish := func(code int, err error) int {
		line.Ts = clk.Now().Format(time.RFC3339)
		if err == nil {
			line.Level = "info"
			line.Msg = "the policy version is in force for the tenant"
			if line.Result == resultAlready {
				line.Msg = "the policy version was already published; nothing was written"
			}
		} else {
			var sentence string
			code, line.Reason, sentence = failure(err)
			line.Level, line.Result, line.Msg, line.SQLState = "error", "refused", sentence, sqlState(err)
			if code == exitCannot {
				line.Result = "error"
			}
			fmt.Fprintln(errw, "policypublish: "+sentence+suffix(line.SQLState))
		}
		b, merr := json.Marshal(line)
		if merr != nil {
			fmt.Fprintln(errw, "policypublish: the result could not be written")
			return exitCannot
		}
		if _, werr := fmt.Fprintln(out, string(b)); werr != nil && code == exitDone {
			fmt.Fprintln(errw, "policypublish: the result could not be written")
			return exitCannot
		}
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	doc, docHash, err := loadPolicy(s.packDir, s.packFile)
	if err != nil {
		return finish(exitCannot, err)
	}
	line.Policy, line.DocumentHash = doc.PolicyID()+"@"+doc.Version(), docHash
	corr, err := ids.New(clk, nil).ID("corr")
	if err != nil {
		return finish(exitCannot, errors.New("no correlation ID"))
	}
	line.CorrelationID = corr
	conn, err := connect(ctx, s.dsnFile)
	if err != nil {
		return finish(exitCannot, err)
	}
	defer conn.Close(ctx)
	ring, err := signing.LoadRing(signerIdentity, s.keyDir, signing.Options{MaxActive: s.keyMaxActive}) // its errors never repeat a key's contents; none is printed
	if err != nil {
		return finish(exitCannot, errors.New("the signing keys cannot be loaded"))
	}
	store := &policystore.Store{Appender: &chain.Appender{Clock: clk, IDs: ids.New(clk, nil), Signers: map[string]chain.Signer{signerIdentity: ring}}}
	res, err := publish(ctx, conn, store, policystore.Request{TenantID: s.tenant, CorrelationID: corr, PolicyID: doc.PolicyID(), Version: doc.Version(), DocumentHash: docHash})
	line.Result = res
	return finish(exitDone, err)
}

func suffix(state string) string {
	if state == "" {
		return ""
	}
	return " (SQLSTATE " + state + ")"
}

// connect reads the DSN from its file (a regular file, not a link, not empty) and opens one connection. No error carries the DSN or any part of it.
func connect(ctx context.Context, path string) (*pgx.Conn, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDSNBytes {
		return nil, errors.New("the DSN file cannot be used")
	}
	raw, err := os.ReadFile(path)
	dsn := strings.TrimSpace(string(raw))
	if err != nil || dsn == "" {
		return nil, errors.New("the DSN file cannot be used")
	}
	cfg, err := pgx.ParseConfig(dsn) // the parse error is dropped on purpose: it can quote the DSN
	if err != nil {
		return nil, errors.New("the DSN does not parse")
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, connectError{state: sqlState(err)}
	}
	return conn, nil
}

// connectError is a connection that could not be opened. It keeps the SQLSTATE of the database's refusal, when there is one, and nothing else: a driver's text can quote the DSN.
type connectError struct{ state string }

func (connectError) Error() string { return "policypublish: the database cannot be reached" }
