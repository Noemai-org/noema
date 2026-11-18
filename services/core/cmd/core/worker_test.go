package main

// STM-04 (the core's Temporal worker; card status/STM-04.md; REQ-EXE-006, REQ-RCV-002, REQ-EXE-005, REQ-OPS-003; INV-30, INV-03, INV-07, AGENT-16). The worker polls one task queue and runs
// the EscalateCase workflow and its activities. Nothing about it has a default: the address, namespace, task queue and activity timeout are the deployment's. With no Temporal setting
// there is no worker and no error (as for every optional component); with some, or any bad one, the process does not start and the error names the setting. The registration is exactly
// what `workflows` declares, so an activity with no retry declaration cannot be registered. Every value here is SYNTHETIC.

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/workflows"
)

func workerEnv(set map[string]string) Env {
	return Env{Getenv: func(k string) string { return set[k] }}
}

func goodWorker() map[string]string {
	return map[string]string{
		"NOEMA_TEMPORAL_ADDRESS":             "temporal:7233",
		"NOEMA_TEMPORAL_NAMESPACE":           "noema",
		"NOEMA_TEMPORAL_TASK_QUEUE":          "noema-core",
		"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS": "30000",
		"NOEMA_TEMPORAL_TLS_CERT_FILE":       "/run/tls/worker.crt",
		"NOEMA_TEMPORAL_TLS_KEY_FILE":        "/run/tls/worker.key",
		"NOEMA_TEMPORAL_TLS_CA_FILE":         "/run/tls/ca.crt",
	}
}

func TestNoTemporalSettingsBuildNoWorker(t *testing.T) {
	cfg, err := LoadWorkerConfig(workerEnv(nil))
	if cfg != nil || err != nil {
		t.Fatalf("got %+v, %v; want no worker and no error", cfg, err)
	}
}

func TestTheWorkerConfigIsReadWithNoDefaults(t *testing.T) {
	cfg, err := LoadWorkerConfig(workerEnv(goodWorker()))
	if err != nil || cfg == nil {
		t.Fatalf("%+v, %v", cfg, err)
	}
	if cfg.Address != "temporal:7233" || cfg.Namespace != "noema" || cfg.TaskQueue != "noema-core" || cfg.ActivityTimeout != 30*time.Second || cfg.TLS.CertFile != "/run/tls/worker.crt" || cfg.TLS.KeyFile != "/run/tls/worker.key" || cfg.TLS.CAFile != "/run/tls/ca.crt" {
		t.Errorf("%+v", cfg)
	}
}

// Every required setting is named when it is missing, and a bad value is refused naming the setting; the error never carries a path's content or another setting's value.
func TestEveryMissingOrBadTemporalSettingIsRefusedByName(t *testing.T) {
	cases := map[string]map[string]string{
		"NOEMA_TEMPORAL_ADDRESS":             {"NOEMA_TEMPORAL_ADDRESS": ""},
		"NOEMA_TEMPORAL_NAMESPACE":           {"NOEMA_TEMPORAL_NAMESPACE": ""},
		"NOEMA_TEMPORAL_TASK_QUEUE":          {"NOEMA_TEMPORAL_TASK_QUEUE": ""},
		"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS": {"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS": ""},
	}
	for name, mut := range cases {
		set := goodWorker()
		for k, v := range mut {
			set[k] = v
		}
		if cfg, err := LoadWorkerConfig(workerEnv(set)); cfg != nil || err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s missing: %+v, %v", name, cfg, err)
		}
	}
	bad := []struct{ key, value string }{
		{"NOEMA_TEMPORAL_ADDRESS", "temporal"},       // no port
		{"NOEMA_TEMPORAL_ADDRESS", "temporal:0"},     // port 0
		{"NOEMA_TEMPORAL_ADDRESS", "temporal:99999"}, // port out of range
		{"NOEMA_TEMPORAL_ADDRESS", "http://temporal:7233"},
		{"NOEMA_TEMPORAL_ADDRESS", "temporal:7233/x"},
		{"NOEMA_TEMPORAL_NAMESPACE", "has space"},
		{"NOEMA_TEMPORAL_TASK_QUEUE", "has space"},
		{"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS", "0"},
		{"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS", "-5"},
		{"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS", "abc"},
		{"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS", "1.5"},
		{"NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS", "99999999999999999999"},
	}
	for _, b := range bad {
		set := goodWorker()
		set[b.key] = b.value
		cfg, err := LoadWorkerConfig(workerEnv(set))
		if cfg != nil || err == nil || !strings.Contains(err.Error(), b.key) {
			t.Errorf("%s=%q: %+v, %v; want a refusal naming the setting", b.key, b.value, cfg, err)
		}
	}
}

// A310 (relayed; DEBT-46): a configured worker always uses TLS and verifies the server with the CA, so the CA file is required and a plain connection is refused (Codex P2 on #676). The
// bench Temporal does not authenticate its clients for now, so the client certificate and key are an optional PAIR: both or neither, never one without the other.
func TestAConfiguredWorkerRequiresTheCAAndAClientCertificateOnlyAsAPair(t *testing.T) {
	const cert, key, ca = "NOEMA_TEMPORAL_TLS_CERT_FILE", "NOEMA_TEMPORAL_TLS_KEY_FILE", "NOEMA_TEMPORAL_TLS_CA_FILE"
	// no CA: a plain connection (or one that would trust nothing) is refused, with or without a client pair
	for name, drop := range map[string][]string{"no TLS at all": {cert, key, ca}, "a pair and no CA": {ca}} {
		set := goodWorker()
		for _, f := range drop {
			delete(set, f)
		}
		if cfg, err := LoadWorkerConfig(workerEnv(set)); cfg != nil || err == nil || !strings.Contains(err.Error(), ca) {
			t.Errorf("%s: %+v, %v; want a refusal naming the CA file", name, cfg, err)
		}
	}
	// the CA alone is allowed: server-verified TLS with no client authentication (the bench Temporal, A310)
	set := goodWorker()
	delete(set, cert)
	delete(set, key)
	cfg, err := LoadWorkerConfig(workerEnv(set))
	if err != nil || cfg == nil || cfg.TLS.CAFile == "" || cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
		t.Fatalf("the CA alone: %+v, %v", cfg, err)
	}
	// a certificate without its key, or a key without its certificate, is refused naming the missing one
	for _, missing := range []string{cert, key} {
		half := goodWorker()
		delete(half, missing)
		if cfg, err := LoadWorkerConfig(workerEnv(half)); cfg != nil || err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("without %s: %+v, %v", missing, cfg, err)
		}
	}
}

// recorder is a worker registry that remembers what it was given.
type recorder struct {
	workflows  []string
	activities []any
}

func (r *recorder) RegisterWorkflowWithOptions(_ any, o workflow.RegisterOptions) {
	r.workflows = append(r.workflows, o.Name)
}
func (r *recorder) RegisterActivity(a any) { r.activities = append(r.activities, a) }

// The worker registers exactly one workflow, EscalateCase, and the activities object whose methods the `workflows` table declares: each method has a row, and the row's kind decides its
// retry (a write is one attempt, D8). A method with no row, or a row with no method that is registered, fails here.
func TestTheWorkerRegistersExactlyWhatTheWorkflowsPackageDeclares(t *testing.T) {
	r := &recorder{}
	acts := &workflows.EscalateActivities{}
	registerWorker(r, acts)
	if !reflect.DeepEqual(r.workflows, []string{workflows.EscalateWorkflowName}) {
		t.Fatalf("workflows %v, want only %s", r.workflows, workflows.EscalateWorkflowName)
	}
	if len(r.activities) != 1 || r.activities[0] != acts {
		t.Fatalf("activities %v, want the one EscalateActivities value", r.activities)
	}
	var methods []string
	ty := reflect.TypeOf(acts)
	for i := 0; i < ty.NumMethod(); i++ {
		methods = append(methods, ty.Method(i).Name)
	}
	sort.Strings(methods)
	for _, m := range methods {
		a, ok := workflows.PolicyFor(m)
		if !ok {
			t.Errorf("activity %s is registered with no retry declaration in workflows.Activities", m)
			continue
		}
		if a.Kind == workflows.Write && a.Policy.MaximumAttempts != 1 {
			t.Errorf("write activity %s is declared with %d attempts, D8 allows 1", m, a.Policy.MaximumAttempts)
		}
	}
	if len(methods) == 0 {
		t.Fatal("EscalateActivities has no methods: nothing would run")
	}
}
