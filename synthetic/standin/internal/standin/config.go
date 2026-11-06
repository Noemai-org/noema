package standin

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config is the service's configuration: where to listen and the credential files. The control listener is placed either at an explicit
// host:port (ControlListen) or on the local address that shares a subnet with a host known to be on the benchmark network only
// (ControlBindVia and ControlPort); exactly one of the two is set.
type Config struct {
	Options        Options
	DataListen     string
	ControlListen  string
	ControlBindVia string
	ControlPort    string
}

// OSEnv and OSReadFile are the real environment and file system.
var (
	OSEnv      = os.Getenv
	OSReadFile = os.ReadFile
)

// dataAddr accepts host:port or :port. The networks the container joins decide who reaches the data side, as with mock-case's case API.
func dataAddr(getenv func(string) string) (string, error) {
	v := getenv("STANDIN_DATA_LISTEN")
	_, port, err := net.SplitHostPort(v)
	if err != nil || !validPort(port) {
		return "", fmt.Errorf("STANDIN_DATA_LISTEN must be host:port or :port (got %q)", v)
	}
	return v, nil
}

func validPort(p string) bool {
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == p
}

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
	if c.DataListen, err = dataAddr(getenv); err != nil {
		return nil, err
	}
	explicit, via := getenv("STANDIN_CONTROL_LISTEN"), getenv("STANDIN_CONTROL_BIND_VIA")
	if (explicit == "") == (via == "") {
		return nil, errors.New("set exactly one of STANDIN_CONTROL_LISTEN and STANDIN_CONTROL_BIND_VIA: the control listener must never default to every interface")
	}
	if explicit != "" {
		if c.ControlListen, err = listenAddr(getenv, "STANDIN_CONTROL_LISTEN"); err != nil {
			return nil, err
		}
		if c.DataListen == c.ControlListen {
			return nil, errors.New("STANDIN_DATA_LISTEN and STANDIN_CONTROL_LISTEN must differ: the two credentials are served on separate listeners")
		}
	} else {
		c.ControlBindVia, c.ControlPort = via, getenv("STANDIN_CONTROL_PORT")
		if !validPort(c.ControlPort) {
			return nil, fmt.Errorf("STANDIN_CONTROL_PORT must be a port number (got %q)", c.ControlPort)
		}
	}
	if c.Options.BenchToken, err = secret(getenv, readFile, "STANDIN_BENCH_TOKEN_FILE"); err != nil {
		return nil, err
	}
	if c.Options.RuntimeToken, err = secret(getenv, readFile, "STANDIN_RUNTIME_TOKEN_FILE"); err != nil {
		return nil, err
	}
	if c.Options.PrincipalKey, err = secret(getenv, readFile, "STANDIN_PRINCIPAL_KEY_FILE"); err != nil {
		return nil, err
	}
	return c, nil
}
