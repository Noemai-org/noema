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
)

// Config is the pass's settings.
type Config struct {
	IdleMS int64
	Batch  int
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

// LoadConfig reads both settings strictly. A missing or malformed value is an error that names the setting and never echoes the value.
func LoadConfig(getenv func(string) string) (Config, error) {
	idle, err := whole(getenv, EnvIdleMS, 1<<62)
	if err != nil {
		return Config{}, err
	}
	batch, err := whole(getenv, EnvBatchSize, 1<<31-1)
	if err != nil {
		return Config{}, err
	}
	return Config{IdleMS: idle, Batch: int(batch)}, nil
}
