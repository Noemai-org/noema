package standin

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// Config is the service's configuration: two listen addresses and two credential files.
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

func listenAddr(getenv func(string) string, name string) (string, error) {
	v := getenv(name)
	host, port, err := net.SplitHostPort(v)
	if err != nil || port == "" || host == "" || host == "0.0.0.0" || host == "::" {
		return "", fmt.Errorf("%s must be a host:port naming one interface, never every interface (got %q)", name, v)
	}
	return v, nil
}

func secret(getenv func(string) string, readFile func(string) ([]byte, error), name string) (string, error) {
	path := getenv(name)
	if path == "" {
		return "", fmt.Errorf("%s is required (a path to a file; the value is never read from the environment)", name)
	}
	b, err := readFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("%s: the file is empty", name)
	}
	return v, nil
}

// LoadConfig reads the configuration; any problem is an error naming the variable.
func LoadConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (*Config, error) {
	c := &Config{}
	var err error
	if c.DataListen, err = listenAddr(getenv, "STANDIN_DATA_LISTEN"); err != nil {
		return nil, err
	}
	if c.ControlListen, err = listenAddr(getenv, "STANDIN_CONTROL_LISTEN"); err != nil {
		return nil, err
	}
	if c.DataListen == c.ControlListen {
		return nil, errors.New("STANDIN_DATA_LISTEN and STANDIN_CONTROL_LISTEN must differ: the two credentials are served on separate listeners")
	}
	if c.Options.BenchToken, err = secret(getenv, readFile, "STANDIN_BENCH_TOKEN_FILE"); err != nil {
		return nil, err
	}
	if c.Options.RuntimeToken, err = secret(getenv, readFile, "STANDIN_RUNTIME_TOKEN_FILE"); err != nil {
		return nil, err
	}
	return c, nil
}
