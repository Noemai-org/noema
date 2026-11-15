package facade

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config is the service's configuration: where to listen and the files it reads. The control listener is placed at an explicit host:port that names one
// interface, never every interface, so the arm (which reaches only the data side) has no route to it. The credential and the pack come from files, never
// from an environment value (AGENTS section 10).
type Config struct {
	Options       Options
	DataListen    string
	ControlListen string
}

// OSEnv and OSReadFile are the real environment and file system.
var (
	OSEnv      = os.Getenv
	OSReadFile = os.ReadFile
)

func validPort(p string) bool {
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

func dataAddr(v string) error {
	_, port, err := net.SplitHostPort(v)
	if err != nil || !validPort(port) {
		return fmt.Errorf("FACADE_DATA_LISTEN must be host:port or :port (got %q)", v)
	}
	return nil
}

// controlAddr requires a host that names one interface.
func controlAddr(v string) error {
	host, port, err := net.SplitHostPort(v)
	if err != nil || !validPort(port) {
		return fmt.Errorf("FACADE_CONTROL_LISTEN must be host:port naming one interface (got %q)", v)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		return fmt.Errorf("FACADE_CONTROL_LISTEN %q could be every interface: the control side must never default to it", v)
	}
	return nil
}

func fileValue(getenv func(string) string, readFile func(string) ([]byte, error), name string) ([]byte, error) {
	path := getenv(name)
	if path == "" {
		return nil, fmt.Errorf("%s is required (a path to a file; the value is never read from the environment)", name)
	}
	b, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return b, nil
}

// LoadConfig reads the configuration; any problem is an error naming the variable.
func LoadConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (*Config, error) {
	c := &Config{DataListen: getenv("FACADE_DATA_LISTEN"), ControlListen: getenv("FACADE_CONTROL_LISTEN")}
	if err := dataAddr(c.DataListen); err != nil {
		return nil, err
	}
	if err := controlAddr(c.ControlListen); err != nil {
		return nil, err
	}
	if c.DataListen == c.ControlListen {
		return nil, errors.New("FACADE_DATA_LISTEN and FACADE_CONTROL_LISTEN must differ: the arm must not reach the control side")
	}
	tok, err := fileValue(getenv, readFile, "FACADE_BENCH_TOKEN_FILE")
	if err != nil {
		return nil, err
	}
	c.Options.BenchToken = strings.TrimSpace(string(tok))
	if len(c.Options.BenchToken) < minTokenLen {
		return nil, fmt.Errorf("FACADE_BENCH_TOKEN_FILE: the credential must be at least %d characters", minTokenLen)
	}
	if c.Options.Pack, err = fileValue(getenv, readFile, "FACADE_PACK_FILE"); err != nil {
		return nil, err
	}
	return c, nil
}
