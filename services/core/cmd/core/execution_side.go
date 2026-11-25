package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	executorport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/executor"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// execution_side.go: the Execution side per tenant (LANE-5 slice 2; A547, A550, A666, interim, relayed; status/LANE-5.md; REQ-EXE-001, REQ-EXE-006, REQ-EXE-008, REQ-SEC-004, REQ-SEC-005;
// INV-03, INV-07, INV-12, INV-17, INV-29, AGENT-08, AGENT-13, AGENT-16). buildExecutionSide takes slice 1's parts (the receipt ring and one execution pool for each tenant) and the seams other
// lanes own, and builds one Execution Controller for each tenant over that tenant's own execution login, and one Executor that sends a grant to the Controller of the session's tenant. The
// composition root (EL) calls it. A part that is missing is one fixed gap line for /readyz and builds no Executor: a Controller that cannot do everything dispatches nothing. Nothing
// here dials, retries or decides; the order of the protocol and every refusal are the Controller's, the Ledger's and the database's.

const (
	// EnvExecutionRollbackTimeout bounds the rollback of a failed Execution transaction after the caller's context has ended, in whole milliseconds (ledger.Ledger.RollbackTimeout, A24).
	// Required, no default (AGENT-16). Not in A554's list of four; named as an [OPEN] in the pull request.
	EnvExecutionRollbackTimeout = "NOEMA_EXECUTION_ROLLBACK_TIMEOUT_MS"
	// EnvExecutionExecuteTimeout bounds one Execute of one grant, in whole milliseconds (executorport.Port.Timeout). Required, no default (AGENT-16). Also an [OPEN].
	EnvExecutionExecuteTimeout = "NOEMA_EXECUTION_EXECUTE_TIMEOUT_MS"

	// executionWriter is the identity that signs the Execution side's Record events (the key announcement of migration 0004; the ledger's own writer name).
	executionWriter = "core-execution"
)

// ErrNoExecutionForTenant is what the Executor returns for a session with no tenant or no principal, or a tenant it holds no Controller for. Nothing was called.
var ErrNoExecutionForTenant = errors.New("cmd/core: the Execution side has no Controller for this session")

var millisRe = regexp.MustCompile(`^[1-9][0-9]*$`)

// executionIDs is what the Ledger, the Record appender and the Controller need of the ID generator (platform/ids.Generator has both).
type executionIDs interface {
	ID(prefix string) (string, error)
	ReceiptID() (string, error)
}

// executionSeams are the parts other lanes own. A nil seam is a named gap, never a stand-in. The dispatcher and the revalidator are the bridge's over the case adapter, which cmd/core may
// not import, so they are built where that is allowed and passed in.
type executionSeams struct {
	Clock       clock.Clock
	IDs         executionIDs
	Keys        controller.KeyVerifier // the grant key ring (E1): HMAC, the same keys Trust signs with
	Dispatcher  controller.Dispatcher
	Revalidator controller.Revalidator
	Intents     ledger.IntentSource
	Plans       ledger.PlanSource
	Verifier    orchestrator.Verifier // verifierport.Port, which needs slice 4's request reader
	Bindings    orchestrator.Bindings // the Trust adapter
	Log         *slog.Logger          // optional; must run through the redactor
}

// executionSide is what was built: a Controller for each tenant, the Executor over them, the two pass-through ports, and the gap lines of what is missing.
type executionSide struct {
	Controllers map[string]*controller.Controller
	Executor    orchestrator.Executor
	Verifier    orchestrator.Verifier
	Bindings    orchestrator.Bindings
	Gaps        []string
}

// millis reads a required setting of whole milliseconds. Absent is (0, false, nil). Present and not a positive whole number that fits a duration is an error that names the setting
// and never the value (AGENT-13).
func millis(env Env, name string) (time.Duration, bool, error) {
	raw := env.Getenv(name)
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if !millisRe.MatchString(raw) || err != nil || n > math.MaxInt64/int64(time.Millisecond) {
		return 0, false, fmt.Errorf("cmd/core: %s must be a positive whole number of milliseconds", name)
	}
	return time.Duration(n) * time.Millisecond, true, nil
}

// buildExecutionSide builds the side. With no parts it is the one gap "execution: not configured" and no error. A setting that is present and invalid is an error naming it; one that is
// absent, like a missing seam, is a gap line. Gaps come in a fixed order and carry no value. Only a complete set of parts builds the Controllers and the Executor.
func buildExecutionSide(env Env, parts *executionParts, s executionSeams) (*executionSide, error) {
	side := &executionSide{Verifier: s.Verifier, Bindings: s.Bindings}
	if parts == nil {
		side.Gaps = []string{"execution: not configured"}
		return side, nil
	}
	rollback, haveRollback, err := millis(env, EnvExecutionRollbackTimeout)
	if err != nil {
		return nil, err
	}
	execute, haveExecute, err := millis(env, EnvExecutionExecuteTimeout)
	if err != nil {
		return nil, err
	}
	attempts, haveAttempts := 0, env.Getenv(ledger.EnvSerializationAttempts) != ""
	if haveAttempts {
		if attempts, err = ledger.ParseSerializationAttempts(env.Getenv(ledger.EnvSerializationAttempts)); err != nil {
			return nil, fmt.Errorf("cmd/core: %s must be a whole number of at least 1", ledger.EnvSerializationAttempts) // the ledger's cause can quote the value
		}
	}
	for _, c := range []struct {
		missing bool
		gap     string
	}{
		{s.Clock == nil, "execution: clock missing"},
		{s.IDs == nil, "execution: ids missing"},
		{s.Keys == nil, "execution: grant keys missing"},
		{s.Dispatcher == nil, "execution: dispatcher missing"},
		{s.Revalidator == nil, "execution: revalidator missing"},
		{s.Intents == nil, "execution: intent source missing"},
		{s.Plans == nil, "execution: plan source missing"},
		{!haveRollback, "execution: " + EnvExecutionRollbackTimeout + " missing"},
		{!haveExecute, "execution: " + EnvExecutionExecuteTimeout + " missing"},
		{!haveAttempts, "execution: " + ledger.EnvSerializationAttempts + " missing"},
	} {
		if c.missing {
			side.Gaps = append(side.Gaps, c.gap)
		}
	}
	if len(side.Gaps) > 0 {
		return side, nil
	}
	rec := &chain.Appender{Clock: s.Clock, IDs: s.IDs, Signers: map[string]chain.Signer{executionWriter: parts.Receipts}}
	tenants := make([]string, 0, len(parts.ExecutionPools))
	for t := range parts.ExecutionPools {
		tenants = append(tenants, t)
	}
	sort.Strings(tenants)
	ctls := make(map[string]*controller.Controller, len(tenants))
	byTenant := make(map[string]orchestrator.Executor, len(tenants))
	for _, t := range tenants {
		pool := parts.ExecutionPools[t]
		l := &ledger.Ledger{Pool: pool, Clock: s.Clock, IDs: s.IDs, Rec: rec, RollbackTimeout: rollback, SerializationAttempts: attempts, Log: s.Log}
		ctl := &controller.Controller{Clock: s.Clock, Keys: s.Keys, Receipts: parts.Receipts, Grants: l, Bind: &ledger.StoreBinder{Pool: pool, Ledger: l, Intents: s.Intents, Plans: s.Plans},
			Reval: s.Revalidator, Ledger: l, Out: s.Dispatcher, IDs: s.IDs}
		ctls[t] = ctl
		byTenant[t] = executorport.Port{Controller: ctl, Timeout: execute}
	}
	side.Controllers, side.Executor = ctls, &tenantExecutor{byTenant: byTenant}
	return side, nil
}

// WritePorts gives the write path's ports: the Executor of this side, the Verifier and Bindings it was given, and the Journal EL passes in. If any is missing it returns the zero
// WritePorts, never a part-built one (NewWriteRunner refuses it anyway), and the gap lines: this side's own and one for each of the three that is not built.
func (e *executionSide) WritePorts(journal orchestrator.Journal) (orchestrator.WritePorts, []string) {
	gaps := append([]string(nil), e.Gaps...)
	if e.Verifier == nil {
		gaps = append(gaps, "execution: verifier not built")
	}
	if e.Bindings == nil {
		gaps = append(gaps, "execution: bindings not built")
	}
	if journal == nil {
		gaps = append(gaps, "execution: journal not built")
	}
	if len(gaps) > 0 || e.Executor == nil {
		return orchestrator.WritePorts{}, gaps
	}
	return orchestrator.WritePorts{Executor: e.Executor, Verifier: e.Verifier, Journal: journal, Bindings: e.Bindings}, nil
}

// tenantExecutor sends a grant to the Executor of the tenant of the session, and to no other (INV-12: the tenant is the verified session's, never an argument of the grant).
type tenantExecutor struct {
	byTenant map[string]orchestrator.Executor
}

var _ orchestrator.Executor = (*tenantExecutor)(nil)

// Execute implements orchestrator.Executor. A session with no tenant or no principal, or a tenant with no Controller, is refused before any call.
func (x *tenantExecutor) Execute(ctx context.Context, s orchestrator.Session, grantID string) (orchestrator.Receipt, error) {
	if x == nil || s.TenantID == "" || s.PrincipalID == "" {
		return orchestrator.Receipt{}, ErrNoExecutionForTenant
	}
	ex, ok := x.byTenant[s.TenantID]
	if !ok {
		return orchestrator.Receipt{}, ErrNoExecutionForTenant
	}
	return ex.Execute(ctx, s, grantID)
}
