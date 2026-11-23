package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	interpreterport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/interpreter"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

// wire_understanding.go builds the Understanding half of the composition root (UND-04, wiring slice 1; rulings A320, A329, A330, A332, A333, A347, A356; REQ-INT-001, REQ-INT-002, REQ-INP-002,
// REQ-AUD-001, REQ-SEC-004, REQ-OPS-003; INV-02, INV-12, INV-14, INV-17, INV-29; AGENT-13, AGENT-16). It loads the pack through its manifest (platform/packload: a file changed after the manifest was made, a link, an
// unlisted file and an id other than the manifest's each stop the start), builds from the verified bytes the schema gate, the required slots of each intent node, the reconciliation rule set and the
// evidence mapping, and builds one Interpreter per tenant over that tenant's own orchestrator login (the pools of the Recorder: no second set), behind the port the orchestrator calls. The
// compile call goes to the model service over mutual TLS (modelclient.go). The core names no pack, id, folder or file of a vertical: the manifest does (INV-14). Every setting is required
// when this is built and has no default (AGENT-16); an unusable one is a fixed error that names it and never repeats its value.
//
// This slice does not mount a route, answer /readyz or read the investigation: the caller plugs UnderstandingParts.Interpreter into Ports.Interpreter (compose.go). The Trust intake's Reconciler
// and the Evidence read-and-seal take RuleSet, Mapping and SealTimeout from here when their wiring lands.

// The settings of this half. NOEMA_PACK_DIR, NOEMA_STORE_TIMEOUT_MS, NOEMA_COMPILE_TIMEOUT_MS and NOEMA_SEAL_TIMEOUT_MS are A320's, with the values of A356 in Compose; the other three
// follow A330 (`NOEMA_<AREA>_<WHAT>[_MS|_BYTES]`, no code default) and are listed in the pull request.
const (
	// EnvPackDir is the one mounted directory every pack path of the manifest is relative to.
	EnvPackDir = "NOEMA_PACK_DIR"
	// EnvPackManifest is the manifest's path, relative to EnvPackDir. It is a setting and not a constant because a fixed path would name a vertical in core (INV-14).
	EnvPackManifest = "NOEMA_PACK_MANIFEST"
	// EnvPackMaxBytes bounds the manifest and each file it lists (packload) and the evidence mapping the parser takes (mapping.Limits.MaxBytes), in bytes.
	EnvPackMaxBytes = "NOEMA_PACK_MAX_BYTES"
	// EnvPackMaxDepth bounds the nesting of the evidence mapping (mapping.Limits.MaxDepth).
	EnvPackMaxDepth = "NOEMA_PACK_MAX_DEPTH"
	// EnvStoreTimeoutMS bounds each save of an admitted intent and each append of a gate refusal or an override, in whole milliseconds.
	EnvStoreTimeoutMS = "NOEMA_STORE_TIMEOUT_MS"
	// EnvCompileTimeoutMS bounds the compile call to the model service, in whole milliseconds.
	EnvCompileTimeoutMS = "NOEMA_COMPILE_TIMEOUT_MS"
	// EnvSealTimeoutMS bounds the sealing of an evidence snapshot, in whole milliseconds. It is validated here and carried in UnderstandingParts for the Evidence read-and-seal wiring.
	EnvSealTimeoutMS = "NOEMA_SEAL_TIMEOUT_MS"
)

var (
	// ErrNoInterpreterForTenant marks a tenant that has no orchestrator login, and so no Interpreter: nothing is asked of the model service or any database, and no other tenant's Interpreter
	// is used (A320, INV-12). The text names no tenant (A347).
	ErrNoInterpreterForTenant = errors.New("cmd/core: no Interpreter is configured for this tenant")
	// ErrSessionUnavailable marks a call whose orchestrator Session is not whole (INV-12, A514). The text names no tenant, principal or role.
	ErrSessionUnavailable = errors.New("cmd/core: the verified session is not available to the Interpreter")
)

// wholeRe is a whole positive number: ASCII digits, no sign, no space, no leading zero.
var wholeRe = regexp.MustCompile(`\A[1-9][0-9]*\z`)

// maxMillis keeps a bound in milliseconds a representable duration. It is structural, not a policy number.
const maxMillis = int64(math.MaxInt64) / int64(time.Millisecond)

// wholeNumber reads a setting that must be a whole positive number. The error names the setting and never echoes the value.
func wholeNumber(env Env, name, unit string) (int64, error) {
	raw := env.Getenv(name)
	n, err := strconv.ParseInt(raw, 10, 64)
	if !wholeRe.MatchString(raw) || err != nil {
		return 0, fmt.Errorf("cmd/core: %s must be a whole positive number%s", name, unit)
	}
	return n, nil
}

// wholeMillis reads a bound in whole milliseconds.
func wholeMillis(env Env, name string) (time.Duration, error) {
	n, err := wholeNumber(env, name, " of milliseconds")
	if err != nil {
		return 0, err
	}
	if n > maxMillis {
		return 0, fmt.Errorf("cmd/core: %s must be a whole positive number of milliseconds", name)
	}
	return time.Duration(n) * time.Millisecond, nil
}

// wireError is a failure whose text is fixed: the cause stays reachable with errors.Is and errors.As and is never part of Error(), because a parser's message can quote the pack (A347).
type wireError struct {
	text  string
	cause error
}

func (e *wireError) Error() string { return e.text }
func (e *wireError) Unwrap() error { return e.cause }

func refused(part string, cause error) error {
	return &wireError{text: "cmd/core: the " + part + " cannot be built from the verified pack", cause: cause}
}

// UnderstandingParts is what buildUnderstanding makes. Interpreter is for Ports.Interpreter. RuleSet and Mapping are the verified reconciliation rule set and evidence mapping, for the
// Trust intake's Reconciler and the Evidence read-and-seal. PackRef is `<vertical>@<version>` as the verified pack says it. SealTimeout is NOEMA_SEAL_TIMEOUT_MS. It is all or nothing: on an
// error every field is zero.
type UnderstandingParts struct {
	Interpreter orchestrator.Interpreter
	RuleSet     *reconcile.RuleSet
	Mapping     *mapping.Mapping
	PackRef     string
	SealTimeout time.Duration
}

// buildUnderstanding builds the half from the settings, the clock and the orchestrator's Recorder, whose per-tenant stores it reuses (the same login, the same signing ring). Every part is
// required: a missing one is an error that names it. Nothing is dialled here.
func buildUnderstanding(env Env, clk clock.Clock, recorder *orchestratorRecorder, admissions orchestrator.Admissions) (UnderstandingParts, error) {
	switch {
	case recorder == nil || len(recorder.stores) == 0:
		return UnderstandingParts{}, errors.New("cmd/core: the Interpreter stores through the orchestrator's logins and needs the orchestrator's Recorder (" + EnvOrchestratorDSNDir + ")")
	case clk == nil:
		return UnderstandingParts{}, errors.New("cmd/core: the Interpreter needs a clock")
	case admissions == nil:
		return UnderstandingParts{}, errors.New("cmd/core: the Interpreter needs the Record's admissions to confirm each turn's recorded input")
	case env.Getenv(EnvPackDir) == "":
		return UnderstandingParts{}, errors.New("cmd/core: the pack needs " + EnvPackDir)
	case env.Getenv(EnvPackManifest) == "":
		return UnderstandingParts{}, errors.New("cmd/core: the pack needs " + EnvPackManifest)
	}
	maxBytes, err := wholeNumber(env, EnvPackMaxBytes, " of bytes")
	if err != nil {
		return UnderstandingParts{}, err
	}
	maxDepth, err := wholeNumber(env, EnvPackMaxDepth, "")
	if err != nil {
		return UnderstandingParts{}, err
	}
	if maxBytes > math.MaxInt32 || maxDepth > math.MaxInt32 {
		return UnderstandingParts{}, fmt.Errorf("cmd/core: %s and %s must fit a 32-bit integer", EnvPackMaxBytes, EnvPackMaxDepth)
	}
	storeTimeout, err := wholeMillis(env, EnvStoreTimeoutMS)
	if err != nil {
		return UnderstandingParts{}, err
	}
	compileTimeout, err := wholeMillis(env, EnvCompileTimeoutMS)
	if err != nil {
		return UnderstandingParts{}, err
	}
	sealTimeout, err := wholeMillis(env, EnvSealTimeoutMS)
	if err != nil {
		return UnderstandingParts{}, err
	}
	client, err := buildModelClient(env)
	if err != nil {
		return UnderstandingParts{}, err
	}
	if client == nil {
		return UnderstandingParts{}, errors.New("cmd/core: the Interpreter calls the model service and needs its settings (" + EnvModelURL + " and the other model settings)")
	}
	gateBytes, err := modelMaxBytes(env) // A333: the gate's bound is the model response bound
	if err != nil {
		return UnderstandingParts{}, err
	}
	if gateBytes > math.MaxInt32 {
		return UnderstandingParts{}, fmt.Errorf("cmd/core: %s must fit a 32-bit integer", EnvModelMaxResponseBytes)
	}
	pack, err := packload.Load(env.Getenv(EnvPackDir), env.Getenv(EnvPackManifest), maxBytes)
	if err != nil {
		return UnderstandingParts{}, fmt.Errorf("cmd/core: %s: the pack cannot be loaded: %w", EnvPackDir, err)
	}
	view, err := understanding.NewPackView(pack.Pack)
	if err != nil {
		return UnderstandingParts{}, refused("pack view", err)
	}
	gate, err := understanding.NewGate(int(gateBytes), view)
	if err != nil {
		return UnderstandingParts{}, refused("schema gate", err)
	}
	required, err := interpreterport.RequiredFromPack(pack.Pack)
	if err != nil {
		return UnderstandingParts{}, refused("required slots", err)
	}
	ruleSet, err := reconcile.ParseRuleSet(pack.ReconRules)
	if err != nil {
		return UnderstandingParts{}, refused("rule set", err)
	}
	sources, err := evidenceSources(pack.Pack)
	if err != nil {
		return UnderstandingParts{}, refused("evidence sources", err)
	}
	scope, err := mapping.ScopeFromDescriptors(pack.Pack)
	if err != nil {
		return UnderstandingParts{}, refused("evidence descriptors", err)
	}
	evidenceMapping, err := mapping.Parse(pack.EvidenceMapping, ruleSet, sources, pack.EvidenceMappingID, mapping.Limits{MaxBytes: int(maxBytes), MaxDepth: int(maxDepth)}, mapping.WithScope(scope))
	if err != nil {
		return UnderstandingParts{}, refused("evidence mapping", err)
	}
	instances := make(tenantInterpreters, len(recorder.stores))
	for tenant, store := range recorder.stores {
		pool, ok := store.Pool.(*pgxpool.Pool) // the Recorder's pools are the tenants' own logins; the intent store needs the pool itself
		if !ok || pool == nil || store.Appender == nil {
			return UnderstandingParts{}, refused("Interpreter", ErrNoInterpreterForTenant)
		}
		in, err := interpreter.New(interpreter.Config{
			Compiler: boundedCompiler{inner: client, timeout: compileTimeout},
			Gate:     gate,
			Recorder: &interpreter.RecordRecorder{Pool: pool, Appender: store.Appender, Timeout: storeTimeout},
			Saver:    &intentstore.Store{Pool: pool, Appender: store.Appender, Timeout: storeTimeout},
		})
		if err != nil {
			return UnderstandingParts{}, refused("Interpreter", err)
		}
		instances[tenant] = in
	}
	id, version := view.Ref()
	packRef := id + "@" + version
	port, err := interpreterport.New(interpreterport.Config{Sessions: edgeSessions{}, Interpreter: instances, Clock: clk, PackRef: packRef, Required: required, Admissions: admissions})
	if err != nil {
		return UnderstandingParts{}, refused("Interpreter port", err)
	}
	return UnderstandingParts{Interpreter: port, RuleSet: ruleSet, Mapping: evidenceMapping, PackRef: packRef, SealTimeout: sealTimeout}, nil
}

// evidenceSources reads the ids of the pack's evidence sources, which the evidence mapping may name. A pack with none, an empty id or a repeated one cannot give the mapping its sources.
func evidenceSources(pack []byte) ([]string, error) {
	var f struct {
		EvidenceSources []struct {
			SourceID string `json:"source_id"`
		} `json:"evidence_sources"`
	}
	if err := json.Unmarshal(pack, &f); err != nil || len(f.EvidenceSources) == 0 {
		return nil, errors.New("the pack names no evidence source")
	}
	seen := make(map[string]bool, len(f.EvidenceSources))
	out := make([]string, 0, len(f.EvidenceSources))
	for _, s := range f.EvidenceSources {
		if s.SourceID == "" || seen[s.SourceID] {
			return nil, errors.New("an evidence source has no id or repeats one")
		}
		seen[s.SourceID] = true
		out = append(out, s.SourceID)
	}
	return out, nil
}

// boundedCompiler gives every compile call a deadline of NOEMA_COMPILE_TIMEOUT_MS: the earlier of that and the caller's own (the compile client sends nothing without one).
type boundedCompiler struct {
	inner   interpreter.Compiler
	timeout time.Duration
}

func (c boundedCompiler) Compile(ctx context.Context, req understanding.CompileRequest) (understanding.Compiled, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.inner.Compile(ctx, req)
}

// tenantInterpreters is one Interpreter per tenant, each over that tenant's own orchestrator login. A tenant that has none is refused, never served by another's (A320, INV-12).
type tenantInterpreters map[string]*interpreter.Interpreter

func (t tenantInterpreters) Interpret(ctx context.Context, in interpreter.Input) (interpreter.Result, error) {
	i, ok := t[in.Session.TenantID]
	if !ok {
		return interpreter.Result{}, ErrNoInterpreterForTenant
	}
	return i.Interpret(ctx, in)
}

// edgeSessions resolves the orchestrator's Session to the Interpreter's (INV-12, AGENT-10, A149, A333, A514). Since noema#882 every caller fills the Session from a verified source: the
// gateway start from the verified token (its tenant, principal, session reference and principal type), a recovery from the stored investigation row. This adapter therefore builds the
// Interpreter's session from the orchestrator Session alone and never reads the request's context (a recovery has no request, and nothing a client sent may reach the answer). It refuses,
// with one fixed error that names none of the values, a Session that is not whole: no tenant or principal, a role outside the four of ruling A1, an authentication reference that is not a
// session reference (`ses_` and 32 lower-case hex digits, the SessionId of contracts common.v1; never a token), or a principal type that is not the one the edge derives from the role. No
// role or type is ever defaulted. The issuer (AssertedBy) is passed through as given: empty when it was not stored (A514). The actor the Record writes is of the session's principal type.
type edgeSessions struct{}

var (
	sessionPlainRe    = regexp.MustCompile(`\A[^\s\x00-\x1f\x7f]+\z`)
	sessionAuthnRefRe = regexp.MustCompile(`\Ases_[0-9a-f]{32}\z`)
)

func (edgeSessions) Resolve(_ context.Context, s orchestrator.Session) (interpreterport.Resolved, error) {
	pt, known := session.PrincipalTypeOf(s.Role)
	if !known || s.PrincipalType != pt || !sessionPlainRe.MatchString(s.TenantID) || !sessionPlainRe.MatchString(s.PrincipalID) || !sessionAuthnRefRe.MatchString(s.AuthnRef) {
		return interpreterport.Resolved{}, ErrSessionUnavailable
	}
	return interpreterport.Resolved{
		Session:   understanding.Session{PrincipalID: s.PrincipalID, TenantID: s.TenantID, AuthnRef: s.AuthnRef, PrincipalType: s.PrincipalType, AssertedBy: s.AssertedBy},
		ActorKind: s.PrincipalType,
	}, nil
}
