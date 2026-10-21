// Package config reads mock-case's settings from the environment. Secrets are read from files named by *_FILE
// variables and never from a value in the environment (AGENTS.md §10). Every default queue and priority below
// is SYNTHETIC (REQ-ENV-006): mock-case emulates a bank's case system, it does not define one.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config is the validated configuration.
type Config struct {
	CaseListen   string // the case API
	BenchListen  string // the bench API (truth and control), bound literally, or ...
	BenchBindVia string // ... bound to the local address that shares a subnet with this host (see listen.BindVia)
	BenchPort    string // used with BenchBindVia

	DBHost, DBPort, DBName       string
	DBAppUser, DBAdminUser       string
	dbAppPassword, dbAdminSecret string

	RuntimeToken string // the core's case adapter
	BenchToken   string // the benchmark runner

	Queues          []string
	DefaultQueue    string
	Priorities      []string
	DefaultPriority string
}

// MinTokenBytes is the shortest credential accepted. It only guards against an empty or placeholder file.
const MinTokenBytes = 32

// Load builds a Config. getenv and readFile are injected so tests need no real environment.
func Load(getenv func(string) string, readFile func(string) ([]byte, error)) (*Config, error) {
	def := func(name, fallback string) string {
		if v := getenv(name); v != "" {
			return v
		}
		return fallback
	}
	c := &Config{
		CaseListen:      def("MOCK_CASE_LISTEN", ":8090"),
		BenchListen:     getenv("MOCK_CASE_BENCH_LISTEN"),
		BenchBindVia:    getenv("MOCK_CASE_BENCH_BIND_VIA"),
		BenchPort:       def("MOCK_CASE_BENCH_PORT", "8091"),
		DBHost:          def("MOCK_CASE_DB_HOST", "mock-case-db"),
		DBPort:          def("MOCK_CASE_DB_PORT", "5432"),
		DBName:          def("MOCK_CASE_DB_NAME", "mock_case"),
		DBAppUser:       def("MOCK_CASE_DB_APP_USER", "mock_case_app"),
		DBAdminUser:     def("MOCK_CASE_DB_ADMIN_USER", "mock_case_admin"),
		Queues:          splitList(def("MOCK_CASE_QUEUES", "PAYMENTS_OPERATIONS,DISPUTES,FRAUD_REVIEW,SETTLEMENT_OPS")),
		DefaultQueue:    def("MOCK_CASE_DEFAULT_QUEUE", "PAYMENTS_OPERATIONS"),
		Priorities:      splitList(def("MOCK_CASE_PRIORITIES", "NORMAL,HIGH")),
		DefaultPriority: def("MOCK_CASE_DEFAULT_PRIORITY", "NORMAL"),
	}
	if (c.BenchListen == "") == (c.BenchBindVia == "") {
		return nil, errors.New("set exactly one of MOCK_CASE_BENCH_LISTEN and MOCK_CASE_BENCH_BIND_VIA: the bench listener must never default to every interface")
	}
	if !contains(c.Queues, c.DefaultQueue) {
		return nil, fmt.Errorf("MOCK_CASE_DEFAULT_QUEUE %q is not in MOCK_CASE_QUEUES", c.DefaultQueue)
	}
	if !contains(c.Priorities, c.DefaultPriority) {
		return nil, fmt.Errorf("MOCK_CASE_DEFAULT_PRIORITY %q is not in MOCK_CASE_PRIORITIES", c.DefaultPriority)
	}
	if _, err := strconv.Atoi(c.DBPort); err != nil {
		return nil, errors.New("MOCK_CASE_DB_PORT is not a number")
	}
	var err error
	secrets := []struct {
		env string
		dst *string
	}{
		{"MOCK_CASE_DB_APP_PASSWORD_FILE", &c.dbAppPassword},
		{"MOCK_CASE_DB_ADMIN_PASSWORD_FILE", &c.dbAdminSecret},
		{"MOCK_CASE_RUNTIME_TOKEN_FILE", &c.RuntimeToken},
		{"MOCK_CASE_BENCH_TOKEN_FILE", &c.BenchToken},
	}
	for _, s := range secrets {
		if *s.dst, err = readSecret(getenv, readFile, s.env); err != nil {
			return nil, err
		}
	}
	for _, t := range []struct{ name, v string }{{"runtime token", c.RuntimeToken}, {"bench token", c.BenchToken}} {
		if len(t.v) < MinTokenBytes {
			return nil, fmt.Errorf("the %s must be at least %d bytes", t.name, MinTokenBytes)
		}
	}
	if c.RuntimeToken == c.BenchToken {
		return nil, errors.New("the runtime token and the bench token must differ: the runtime must never hold the oracle credential")
	}
	return c, nil
}

func readSecret(getenv func(string) string, readFile func(string) ([]byte, error), env string) (string, error) {
	path := getenv(env)
	if path == "" {
		return "", fmt.Errorf("%s is required (a path to a file; the value is never read from the environment)", env)
	}
	b, err := readFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", env, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("%s: the file is empty", env)
	}
	return v, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// AppDSN is the connection string of the serving role.
func (c *Config) AppDSN() string { return c.dsn(c.DBAppUser, c.dbAppPassword) }

// AdminDSN is the connection string of the role that migrates and resets.
func (c *Config) AdminDSN() string { return c.dsn(c.DBAdminUser, c.dbAdminSecret) }

func (c *Config) dsn(user, password string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: c.DBHost + ":" + c.DBPort, Path: "/" + c.DBName, RawQuery: "sslmode=disable"}
	return u.String()
}

// OSEnv and OSReadFile bind Load to the process.
func OSEnv(name string) string { return os.Getenv(name) }

// OSReadFile reads a file from disk.
func OSReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
