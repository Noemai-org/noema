package recovery

import (
	"fmt"
	"regexp"
	"strconv"
)

// The settings of a pass (A330: NOEMA_<AREA>_<WHAT>[_MS|_BYTES], no default in code; AGENT-16). Deployment sets them.
const (
	// EnvIdleMS is the age, in whole milliseconds, of an investigation's last move before a pass takes it up.
	EnvIdleMS = "NOEMA_STM_RECOVERY_IDLE_MS"
	// EnvBatchSize is the most investigations one pass of one tenant takes.
	EnvBatchSize = "NOEMA_STM_RECOVERY_BATCH_SIZE"
	// EnvRunTimeoutMS is the longest a live investigation run may take, in whole milliseconds (A477, interim, relayed, not yet committed). A pass reads it only to refuse an idle age that does not exceed it.
	EnvRunTimeoutMS = "NOEMA_INVESTIGATION_RUN_TIMEOUT_MS"
)

// Config is the pass's settings.
type Config struct {
	IdleMS       int64
	Batch        int
	RunTimeoutMS int64
}

var wholeRe = regexp.MustCompile(`\A[1-9][0-9]*\z`)

func whole(getenv func(string) string, name string, max int64) (int64, error) {
	raw := getenv(name)
	if !wholeRe.MatchString(raw) {
		return 0, fmt.Errorf("recovery: %s must be a whole positive number", name)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("recovery: %s must be a whole positive number", name)
	}
	return n, nil
}

// check refuses an idle age that does not exceed the run timeout: a live run would be taken up a second time (A477, interim, relayed, not yet committed). There is no default for the run timeout.
func (c Config) check() error {
	if c.RunTimeoutMS < 1 || c.IdleMS <= c.RunTimeoutMS {
		return fmt.Errorf("recovery: %s must be greater than %s", EnvIdleMS, EnvRunTimeoutMS)
	}
	return nil
}

// LoadConfig reads the three settings strictly. A missing or malformed value is an error that names the setting and never echoes the value.
func LoadConfig(getenv func(string) string) (Config, error) {
	idle, err := whole(getenv, EnvIdleMS, 1<<62)
	if err != nil {
		return Config{}, err
	}
	batch, err := whole(getenv, EnvBatchSize, 1<<31-1)
	if err != nil {
		return Config{}, err
	}
	run, err := whole(getenv, EnvRunTimeoutMS, 1<<62)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{IdleMS: idle, Batch: int(batch), RunTimeoutMS: run}
	if err := cfg.check(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
